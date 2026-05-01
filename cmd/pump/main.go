package main

import (
	"log"
	"os"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/api"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/db"
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

func main() {
	cfg := conf.GetFromEnv()
	port := envOr("PORT", "8080")
	host := envOr("HOST", "0.0.0.0")
	apiKey := os.Getenv("API_KEY")
	nodePath := os.Getenv("NODE_PATH")

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("ERROR: POSTGRES_DSN is required")
	}

	pgStore, err := store.NewPostgres(dsn)
	if err != nil {
		log.Fatalf("ERROR: connect postgres: %v", err)
	}
	if err := db.MigratePostgres(pgStore.Pool()); err != nil {
		log.Fatalf("ERROR: migrate: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	if apiKey != "" {
		r.Use(api.APIKeyMiddleware(apiKey))
	}

	cfg.NodePath = nodePath
	api.RegisterRoutes(r, pgStore, cfg)
	web.RegisterRoutes(r, pgStore, cfg, func(newCfg models.Conf) {
		api.SetConfig(newCfg)
	})

	addr := host + ":" + port
	log.Println("=================================== ")
	log.Printf("PUMP monolith at http://%s", addr)
	log.Println("=================================== ")

	if err := r.Run(addr); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
}
