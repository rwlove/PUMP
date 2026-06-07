package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/logger"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// GuiWithStore starts the frontend web server backed by a remote API.
//
//   - s        – store.APIClient pointing at the backend API
//   - ac       – the same *store.APIClient for config/auth operations
//   - port     – the port this frontend process should listen on (e.g. "8080")
//   - nodePath – path to local node_modules (empty = use CDN)
func GuiWithStore(s store.Store, ac *store.APIClient, port, nodePath string) {
	// Fetch display config (theme, color, etc.) from the API.
	cfg, err := ac.GetConfig()
	if err != nil {
		slog.Error("cannot fetch config from API", slog.Any("error", err))
		os.Exit(1)
	}
	appConfig = cfg
	appConfig.NodePath = nodePath
	appConfig.Icon = icon

	apiClient = ac
	startRouter(s, ac, "0.0.0.0:"+port)
}

// RegisterRoutes mounts all web UI routes on r using the provided store and config.
// onConfigSave is called whenever the user saves settings; pass nil in split mode.
// Used by cmd/pump (monolith). Does not call r.Run().
func RegisterRoutes(r *gin.Engine, s store.Store, cfg models.Conf, onConfigSave func(models.Conf)) {
	appConfig = cfg
	dataStore = s
	apiClient = nil
	configSaveHook = onConfigSave

	templ := template.New("").Funcs(template.FuncMap{
		"json": func(v interface{}) template.JS {
			j, _ := json.Marshal(v)
			return template.JS(j)
		},
		"safeJS": func(s interface{}) template.JS {
			return template.JS(fmt.Sprint(s))
		},
	})
	templ = template.Must(templ.ParseFS(templFS, "templates/*"))
	r.SetHTMLTemplate(templ)
	r.StaticFS("/fs/", http.FS(pubFS))

	r.GET("/", indexHandler)
	r.GET("/admin/", adminHandler)
	r.GET("/config/", configHandler)
	r.GET("/exercise/", exerciseHandler)
	r.GET("/stats/", statsHandler)
	r.GET("/wall/", wallHandler)

	// Browser-facing proxy to pump-cv for the admin panel's live data.
	r.Any("/api/cv/*path", pumpCVProxyHandler)

	r.POST("/config/", saveConfigHandler)
	r.POST("/exercise/", saveExerciseHandler)
	r.POST("/exercise/:id/reference", uploadReferenceClipHandler)
	r.POST("/exdel/", deleteExerciseHandler)
	r.POST("/set/", setHandler)
	r.POST("/weight/", addWeightHandler)
	r.POST("/wdel/", deleteWeightHandler)
}

// startRouter wires up the Gin router with the given store and starts serving.
func startRouter(s store.Store, ac *store.APIClient, address string) {
	dataStore = s
	apiClient = ac

	slog.Info("pump-frontend ready", slog.String("addr", "http://"+address))

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(logger.GinMiddleware())

	templ := template.New("").Funcs(template.FuncMap{
		"json": func(v interface{}) template.JS {
			j, _ := json.Marshal(v)
			return template.JS(j)
		},
		"safeJS": func(s interface{}) template.JS {
			return template.JS(fmt.Sprint(s))
		},
	})
	templ = template.Must(templ.ParseFS(templFS, "templates/*"))
	router.SetHTMLTemplate(templ)
	router.StaticFS("/fs/", http.FS(pubFS))

	router.GET("/", indexHandler)
	router.GET("/admin/", adminHandler)
	router.GET("/config/", configHandler)
	router.GET("/exercise/", exerciseHandler)
	router.GET("/stats/", statsHandler)
	router.GET("/wall/", wallHandler)

	router.Any("/api/cv/*path", pumpCVProxyHandler)

	router.POST("/config/", saveConfigHandler)
	router.POST("/exercise/", saveExerciseHandler)
	router.POST("/exercise/:id/reference", uploadReferenceClipHandler)
	router.POST("/exdel/", deleteExerciseHandler)
	router.POST("/set/", setHandler)
	router.POST("/weight/", addWeightHandler)
	router.POST("/wdel/", deleteWeightHandler)
	if err := router.Run(address); err != nil {
		slog.Error("router failed", slog.Any("error", err))
		os.Exit(1)
	}
}
