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
	"github.com/rwlove/PUMP/internal/notify"
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
//	LOG_LEVEL          log verbosity: debug/info/warn/error  (default: info)
//	PORT               listen port                           (default: 8080)
//	HOST               listen address                        (default: 0.0.0.0)
//	POSTGRES_DSN       PostgreSQL connection string          (required)
//	API_KEY            sent as X-Api-Key when proxying to pump-cv (server-to-server only;
//	                   no inbound auth — pump itself is gated by oauth2-proxy on the gateway)
//	WEIGHT_INGEST_KEY  when set, POST /api/weight requires header X-Api-Key matching this
//	                   value. Enables off-cluster ingest (e.g. an ESPHome BLE-scale device
//	                   reaching pump-api via an oauth2-bypassing internal Route scoped to
//	                   /api/weight only). Unset preserves the in-cluster no-auth posture.
//	NODE_PATH          path to local node_modules            (default: "", use CDN)
//	CVAUTOLOG          accept set writes from pump-cv        (default: false; toggleable in UI)
//	PUSHOVER_USER_KEY  Pushover user key for notifications   (env-only, not in UI)
//	PUSHOVER_APP_TOKEN Pushover app token for notifications  (env-only, not in UI)
//	PUSHOVER_API_URL   Pushover API endpoint override         (default: api.pushover.net)
//	PUBLIC_URL         externally-reachable PUMP base URL    (used in notification deep-links)
//	PUMP_CV_URL        where to forward reference-clip uploads (e.g. http://pump-cv:8080)
//	PUMP_CLIPS_DIR     local directory holding per-set clip mp4s, served at /clips/
func main() {
	logger.Init(os.Getenv("LOG_LEVEL"))

	cfg := conf.GetFromEnv()
	port := envOr("PORT", "8080")
	host := envOr("HOST", "0.0.0.0")
	nodePath := os.Getenv("NODE_PATH")

	slog.Info("PUMP monolith starting",
		slog.String("version", web.Version),
		slog.String("host", host),
		slog.String("port", port),
		slog.String("color", cfg.Color),
		slog.Int("pagestep", cfg.PageStep),
		slog.Int("frequency_days", cfg.FrequencyDays),
		slog.Int("display_days", cfg.DisplayDays),
		slog.Bool("cv_autolog", cfg.CVAutoLog),
		slog.Bool("pushover_configured", cfg.PushoverConfigured()),
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

	cfg.NodePath = nodePath
	pushover := &notify.Pushover{
		UserKey:  cfg.PushoverUserKey,
		AppToken: cfg.PushoverAppToken,
		APIURL:   os.Getenv("PUSHOVER_API_URL"),
	}
	publicURL := os.Getenv("PUBLIC_URL")
	api.RegisterRoutes(r, pgStore, cfg, pushover, publicURL)
	// Broadcast the running build to any connected wall kiosk so it
	// can self-reload when this Pod is replaced by a newer image.
	api.SetBuildSHA(web.Version)

	// Static serving of per-set clip mp4s written by pump-cv. Both
	// services mount the same volume; this side just exposes it at
	// /clips/ so the browser can <video src="/clips/<date>/<file>.mp4">.
	if clipsDir := os.Getenv("PUMP_CLIPS_DIR"); clipsDir != "" {
		r.Static("/clips", clipsDir)
		slog.Info("serving CV clips", slog.String("from", clipsDir))
	}
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
