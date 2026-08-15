package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/store"
)

// RegisterRoutes mounts all web UI routes on r using the provided store.
// Config is read from the shared conf holder.
// Used by cmd/pump (monolith). Does not call r.Run().
func RegisterRoutes(r *gin.Engine, s *store.PostgresStore) {
	dataStore = s
	healthStore = s

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

	// Serve the app service worker from the root so its default scope is "/".
	// A worker under /fs/ could only control /fs/, so it must live at the root
	// to make PUMP installable as a PWA over the whole app.
	r.GET("/sw.js", serviceWorkerHandler)

	r.GET("/", indexHandler)
	r.GET("/admin/", adminHandler)
	r.GET("/config/", configHandler)
	r.GET("/exercise/", exerciseHandler)
	r.GET("/stats/", statsHandler)
	r.GET("/library/", libraryHandler)
	r.GET("/health/", healthHandler)
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

// serviceWorkerHandler serves the app service worker at the root path so its
// default scope is "/". no-cache lets worker updates propagate promptly.
func serviceWorkerHandler(c *gin.Context) {
	data, err := pubFS.ReadFile("public/sw.js")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
}
