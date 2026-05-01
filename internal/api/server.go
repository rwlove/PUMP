package api

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/db"
	"github.com/rwlove/PUMP/internal/logger"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

var (
	appConfig models.Conf
	dataStore store.Store
)

// Start reads config from environment variables and begins serving the JSON API.
// POSTGRES_DSN is required.
//
// All other settings (port, API key, theme, …) are read from
// environment variables:
//
//	LOG_LEVEL    log verbosity: debug/info/warn/error  (default: info)
//	PORT         listen port                           (default: 8851)
//	API_KEY      required X-Api-Key value              (default: "", no auth)
//	HOST         listen host                           (default: 0.0.0.0)
//	THEME        UI theme                              (default: cosmo)
//	COLOR        light or dark                         (default: dark)
//	HEATCOLOR    heatmap colour                        (default: #2780e3)
//	PAGESTEP     rows per page                         (default: 10)
//	DISPLAY_DAYS main-page history days                (default: 30)
func Start() {
	appConfig = conf.GetFromEnv()

	apiKey := os.Getenv("API_KEY")
	postgresDSN := os.Getenv("POSTGRES_DSN")

	slog.Info("pump starting",
		slog.String("host", appConfig.Host),
		slog.String("port", appConfig.Port),
		slog.String("theme", appConfig.Theme),
		slog.String("color", appConfig.Color),
		slog.String("heatcolor", appConfig.HeatColor),
		slog.Int("pagestep", appConfig.PageStep),
		slog.Int("frequency_days", appConfig.FrequencyDays),
		slog.Int("display_days", appConfig.DisplayDays),
		slog.Bool("api_key_set", apiKey != ""),
	)

	if postgresDSN == "" {
		slog.Error("POSTGRES_DSN environment variable is required")
		os.Exit(1)
	}

	slog.Info("connecting to PostgreSQL")
	pgStore, err := store.NewPostgres(postgresDSN)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", slog.Any("error", err))
		os.Exit(1)
	}
	slog.Info("PostgreSQL connection established")

	slog.Info("running schema migrations")
	if err := db.MigratePostgres(pgStore.Pool()); err != nil {
		slog.Error("schema migration failed", slog.Any("error", err))
		os.Exit(1)
	}
	slog.Info("schema migrations complete")
	dataStore = pgStore

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())

	// API key middleware (optional – skip when API_KEY is unset)
	if apiKey != "" {
		r.Use(apiKeyMiddleware(apiKey))
		slog.Info("API key authentication enabled")
	}

	registerRoutes(r)

	address := appConfig.Host + ":" + appConfig.Port
	slog.Info("pump ready", slog.String("addr", "http://"+address))

	if err := r.Run(address); err != nil {
		slog.Error("server failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// RegisterRoutes mounts all API routes on r using the provided store and config.
// Used by cmd/pump (monolith). Does not call r.Run().
func RegisterRoutes(r *gin.Engine, s store.Store, cfg models.Conf) {
	appConfig = cfg
	dataStore = s
	registerRoutes(r)
	slog.Debug("api routes registered")
}

// SetConfig updates the in-memory appConfig. Called by the monolith web layer
// when config is saved through the UI.
func SetConfig(cfg models.Conf) {
	appConfig = cfg
	slog.Debug("api appConfig updated",
		slog.String("theme", cfg.Theme),
		slog.String("color", cfg.Color),
	)
}

// APIKeyMiddleware returns Gin middleware that requires the given key in the
// X-Api-Key header. Exported for use by cmd/pump (monolith).
func APIKeyMiddleware(key string) gin.HandlerFunc {
	return apiKeyMiddleware(key)
}

// registerRoutes mounts all API routes on r. Shared by Start() and RegisterRoutes().
func registerRoutes(r *gin.Engine) {
	// Exercises
	r.GET("/api/exercises", getExercises)
	r.POST("/api/exercises", postExercise)
	r.PUT("/api/exercises/:id", putExercise)
	r.PATCH("/api/exercises/:id/color", patchExerciseColor)
	r.DELETE("/api/exercises/:id", deleteExercise)

	// Sets
	r.GET("/api/sets", getSets)
	r.PUT("/api/sets/date/:date", putSetsByDate)

	// Body weight
	r.GET("/api/weight", getWeight)
	r.POST("/api/weight", postWeight)
	r.DELETE("/api/weight/:id", deleteWeight)

	// Config
	r.GET("/api/config", getConfig)
	r.PUT("/api/config", putConfig)
}

// ─── middleware ───────────────────────────────────────────────────────────────

func apiKeyMiddleware(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Api-Key") != key {
			slog.Warn("rejected request: missing or invalid API key",
				slog.String("ip", c.ClientIP()),
				slog.String("path", c.Request.URL.Path),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// ─── exercises ────────────────────────────────────────────────────────────────

func getExercises(c *gin.Context) {
	exs, err := dataStore.SelectEx()
	if err != nil {
		slog.Error("getExercises: SelectEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("getExercises", slog.Int("count", len(exs)))
	c.JSON(http.StatusOK, exs)
}

func postExercise(c *gin.Context) {
	var ex models.Exercise
	if err := c.ShouldBindJSON(&ex); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("postExercise", slog.String("name", ex.Name), slog.String("group", ex.Group))
	if err := dataStore.InsertEx(ex); err != nil {
		slog.Error("postExercise: InsertEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("exercise created", slog.String("name", ex.Name))
	c.Status(http.StatusCreated)
}

func putExercise(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var ex models.Exercise
	if err := c.ShouldBindJSON(&ex); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ex.ID = id
	slog.Debug("putExercise", slog.Int("id", id), slog.String("name", ex.Name))
	// Replace: delete old, insert new
	if err := dataStore.DeleteEx(id); err != nil {
		slog.Warn("putExercise: DeleteEx failed (continuing)", slog.Int("id", id), slog.Any("error", err))
	}
	if err := dataStore.InsertEx(ex); err != nil {
		slog.Error("putExercise: InsertEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("exercise updated", slog.Int("id", id), slog.String("name", ex.Name))
	c.Status(http.StatusOK)
}

func patchExerciseColor(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("patchExerciseColor", slog.Int("id", id), slog.String("color", body.Color))
	if err := dataStore.UpdateExColor(id, body.Color); err != nil {
		slog.Error("patchExerciseColor: UpdateExColor failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func deleteExercise(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	slog.Debug("deleteExercise", slog.Int("id", id))
	if err := dataStore.DeleteEx(id); err != nil {
		slog.Error("deleteExercise: DeleteEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("exercise deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

// ─── sets ─────────────────────────────────────────────────────────────────────

func getSets(c *gin.Context) {
	sets, err := dataStore.SelectSet()
	if err != nil {
		slog.Error("getSets: SelectSet failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("getSets", slog.Int("count", len(sets)))
	c.JSON(http.StatusOK, sets)
}

func putSetsByDate(c *gin.Context) {
	date := c.Param("date")
	var sets []models.Set
	if err := c.ShouldBindJSON(&sets); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("putSetsByDate", slog.String("date", date), slog.Int("sets", len(sets)))
	if err := dataStore.BulkReplaceSetsByDate(date, sets); err != nil {
		slog.Error("putSetsByDate: BulkReplaceSetsByDate failed",
			slog.String("date", date), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("sets saved", slog.String("date", date), slog.Int("count", len(sets)))
	c.Status(http.StatusOK)
}

// ─── weight ───────────────────────────────────────────────────────────────────

func getWeight(c *gin.Context) {
	ws, err := dataStore.SelectW()
	if err != nil {
		slog.Error("getWeight: SelectW failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("getWeight", slog.Int("count", len(ws)))
	c.JSON(http.StatusOK, ws)
}

func postWeight(c *gin.Context) {
	var w models.BodyWeight
	if err := c.ShouldBindJSON(&w); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("postWeight", slog.String("date", w.Date), slog.String("weight", w.Weight.String()))
	if err := dataStore.InsertW(w); err != nil {
		slog.Error("postWeight: InsertW failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("body weight logged", slog.String("date", w.Date))
	c.Status(http.StatusCreated)
}

func deleteWeight(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	slog.Debug("deleteWeight", slog.Int("id", id))
	if err := dataStore.DeleteW(id); err != nil {
		slog.Error("deleteWeight: DeleteW failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("body weight entry deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

// ─── config ───────────────────────────────────────────────────────────────────

func getConfig(c *gin.Context) {
	slog.Debug("getConfig")
	c.JSON(http.StatusOK, appConfig)
}

func putConfig(c *gin.Context) {
	var cfg models.Conf
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Info("config updated",
		slog.String("theme", cfg.Theme),
		slog.String("color", cfg.Color),
		slog.String("heatcolor", cfg.HeatColor),
		slog.Int("pagestep", cfg.PageStep),
	)
	appConfig.Host = cfg.Host
	appConfig.Port = cfg.Port
	appConfig.Theme = cfg.Theme
	appConfig.Color = cfg.Color
	appConfig.HeatColor = cfg.HeatColor
	appConfig.PageStep = cfg.PageStep
	appConfig.FrequencyDays = cfg.FrequencyDays
	conf.Write(appConfig)
	c.Status(http.StatusOK)
}
