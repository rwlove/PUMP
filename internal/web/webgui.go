package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/store"
)

// RegisterRoutes mounts all web UI routes on r using the provided store.
// Config is read from the shared conf holder.
// Used by cmd/pump (monolith). Does not call r.Run().
func RegisterRoutes(r *gin.Engine, s *store.PostgresStore) {
	dataStore = s
	healthStore = s
	planStore = s

	templ := template.New("").Funcs(templateFuncs())
	templ = template.Must(templ.ParseFS(templFS, "templates/*"))
	r.SetHTMLTemplate(templ)
	r.StaticFS("/fs/", http.FS(pubFS))

	// Serve the app service worker from the root so its default scope is "/".
	// A worker under /fs/ could only control /fs/, so it must live at the root
	// to make PUMP installable as a PWA over the whole app.
	r.GET("/sw.js", serviceWorkerHandler)

	// Serve the web app manifest from the root with no-cache. The /fs static
	// server sets no cache headers, so browsers heuristically cache the manifest
	// (and its icons) and never re-read them — an install then stays pinned to a
	// stale icon set even after the icons are fixed. A root URL + no-cache makes
	// the manifest always revalidate so icon updates reach installed PWAs.
	r.GET("/manifest.json", manifestHandler)

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
	// Athlete feedback (session RPE + next-day soreness). Same-origin form,
	// unkeyed — the coach-authoring API twin (/api/session-feedback) is what
	// PLAN_INGEST_KEY gates.
	r.POST("/feedback/", feedbackHandler)
}

// templateFuncs returns the FuncMap every HTML template is parsed with. It is
// a package function (not inline in RegisterRoutes) so the template-parse test
// can validate the templates against the exact same funcs the app uses.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"json": func(v interface{}) template.JS {
			j, _ := json.Marshal(v)
			return template.JS(j)
		},
		"safeJS": func(s interface{}) template.JS {
			return template.JS(fmt.Sprint(s))
		},
		// derefInt renders a *int target (RIR) — nil prints nothing; guard the
		// surrounding block with {{ if .TargetRIR }} first.
		"derefInt": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
		// weightStr renders a *decimal target weight; "" when not prescribed
		// (a provisional pull whose load waits on feedback).
		"weightStr": func(p *decimal.Decimal) string {
			if p == nil {
				return ""
			}
			return p.String()
		},
		// splitComma turns a "grip-offload,elbow-gated" flag string into its
		// individual tags so the template can render one badge per flag.
		"splitComma": func(s string) []string {
			s = strings.TrimSpace(s)
			if s == "" {
				return nil
			}
			parts := strings.Split(s, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			return out
		},
	}
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

// manifestHandler serves the web app manifest with no-cache (see the /manifest.json
// route comment). Served with the correct application/manifest+json type.
func manifestHandler(c *gin.Context) {
	data, err := pubFS.ReadFile("public/manifest.json")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/manifest+json", data)
}
