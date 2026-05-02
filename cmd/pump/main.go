package main

import (
	"log/slog"
	"os"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/api"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/db"
	"github.com/rwlove/PUMP/internal/logger"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
	"github.com/rwlove/PUMP/internal/web"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// All configuration is read from environment variables:
//
//	LOG_LEVEL    log verbosity: debug/info/warn/error  (default: info)
//	PORT         listen port                           (default: 8080)
//	HOST         listen address                        (default: 0.0.0.0)
//	POSTGRES_DSN PostgreSQL connection string (required)
//	API_KEY      optional X-Api-Key for API routes
//	NODE_PATH    path to local node_modules            (default: "", use CDN)
func main() {
	logger.Init(os.Getenv("LOG_LEVEL"))

	cfg := conf.GetFromEnv()
	port := envOr("PORT", "8080")
	host := envOr("HOST", "0.0.0.0")
	apiKey := os.Getenv("API_KEY")
	nodePath := os.Getenv("NODE_PATH")

	slog.Info("PUMP monolith starting",
		slog.String("host", host),
		slog.String("port", port),
		slog.String("color", cfg.Color),
		slog.Int("pagestep", cfg.PageStep),
		slog.Int("frequency_days", cfg.FrequencyDays),
		slog.Int("display_days", cfg.DisplayDays),
		slog.Bool("api_key_set", apiKey != ""),
	)

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		slog.Error("POSTGRES_DSN environment variable is required")
		os.Exit(1)
	}

	slog.Info("connecting to PostgreSQL")
	pgStore, err := store.NewPostgres(dsn)
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

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())

	if apiKey != "" {
		r.Use(api.APIKeyMiddleware(apiKey))
		slog.Info("API key authentication enabled")
	}

	cfg.NodePath = nodePath
	api.RegisterRoutes(r, pgStore, cfg)
	web.RegisterRoutes(r, pgStore, cfg, func(newCfg models.Conf) {
		api.SetConfig(newCfg)
		slog.Info("config updated via web UI",
			slog.String("color", newCfg.Color),
		)
	})

	addr := host + ":" + port
	slog.Info("PUMP ready", slog.String("addr", "http://"+addr))

	if err := r.Run(addr); err != nil {
		slog.Error("server failed", slog.Any("error", err))
		os.Exit(1)
	}
}
