package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/auth"
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
		log.Fatalf("ERROR: cannot fetch config from API: %v", err)
	}
	appConfig = cfg
	appConfig.NodePath = nodePath
	appConfig.Icon = icon

	// Auth config is also sourced from the API so the frontend enforces the
	// same auth settings as the backend.
	authConf.Auth = cfg.Auth

	apiClient = ac
	startRouter(s, ac, "0.0.0.0:"+port)
}

// startRouter wires up the Gin router with the given store and starts serving.
// authMW selects the right auth strategy: local sessions (monolith) or
// pass-through (frontend, where the API already enforces auth via API key).
func startRouter(s store.Store, ac *store.APIClient, address string) {
	dataStore = s
	apiClient = ac

	log.Println("=================================== ")
	log.Printf("Web GUI at http://%s", address)
	log.Println("=================================== ")

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

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

	router.GET("/login/", loginHandler)
	router.POST("/login/", loginHandler)

	router.GET("/", auth.Auth(&authConf), indexHandler)
	router.GET("/config/", auth.Auth(&authConf), configHandler)
	router.GET("/exercise/", auth.Auth(&authConf), exerciseHandler)
	router.GET("/stats/", auth.Auth(&authConf), statsHandler)
	router.GET("/weight/", auth.Auth(&authConf), weightHandler)

	router.POST("/config/", auth.Auth(&authConf), saveConfigHandler)
	router.POST("/config/auth", auth.Auth(&authConf), saveConfigAuth)
	router.POST("/exercise/", auth.Auth(&authConf), saveExerciseHandler)
	router.POST("/exdel/", auth.Auth(&authConf), deleteExerciseHandler)
	router.POST("/set/", auth.Auth(&authConf), setHandler)
	router.POST("/weight/", auth.Auth(&authConf), addWeightHandler)
	if err := router.Run(address); err != nil {
		log.Fatalf("ERROR: router failed: %v", err)
	}
}
