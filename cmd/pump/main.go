package main

import (
	"context"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/api"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/db"
	"github.com/rwlove/PUMP/internal/logger"
	"github.com/rwlove/PUMP/internal/notify"
	"github.com/rwlove/PUMP/internal/store"
	"github.com/rwlove/PUMP/internal/treadmill"
	"github.com/rwlove/PUMP/internal/voltra"
	"github.com/rwlove/PUMP/internal/web"
)

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
//
// Treadmill cardio auto-capture (subscribes to the Z-Wave smart-plug wattage
// zwave-js-ui publishes to MQTT; detects a session and logs it as cardio):
//
//	TREADMILL_MQTT_ENABLED          turn the consumer on            (default: false)
//	TREADMILL_MQTT_BROKER           broker URL (tcp://host:1883)
//	TREADMILL_MQTT_USERNAME         broker username
//	TREADMILL_MQTT_PASSWORD         broker password
//	TREADMILL_MQTT_TOPIC            wattage topic (default: zwave/Gym/Treadmill/50/0/value/66049)
//	TREADMILL_WATTS_THRESHOLD       watts at/above which it's "in use"  (default: 50)
//	TREADMILL_OFF_DEBOUNCE_SECONDS  sub-threshold time before a session closes (default: 60)
//	TREADMILL_MIN_SESSION_SECONDS   shorter sessions are ignored        (default: 120)
//	TREADMILL_SESSION_TYPE          cardio type label                   (default: Treadmill)
func main() {
	logger.Init(os.Getenv("LOG_LEVEL"))

	cfg := conf.GetFromEnv()
	port := conf.EnvOr("PORT", "8080")
	host := conf.EnvOr("HOST", "0.0.0.0")
	nodePath := os.Getenv("NODE_PATH")

	slog.Info("PUMP monolith starting",
		slog.String("version", web.Version),
		slog.String("host", host),
		slog.String("port", port),
		slog.String("color", cfg.Color),
		slog.Int("pagestep", cfg.PageStep),
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

	// Overlay any UI-persisted settings on top of the env-var defaults so
	// changes made via /config/ survive a pod restart. Env vars still win
	// for a fresh install (no app_config row yet).
	if persisted, ok, err := pgStore.GetAppConfig(context.Background()); err != nil {
		slog.Warn("failed to load persisted config; using env-var values",
			slog.Any("error", err))
	} else if ok {
		cfg.Color = persisted.Color
		cfg.PageStep = persisted.PageStep
		cfg.DisplayDays = persisted.DisplayDays
		cfg.AutoFill = persisted.AutoFill
		cfg.CVAutoLog = persisted.CVAutoLog
		slog.Info("loaded persisted config",
			slog.String("color", cfg.Color),
			slog.Bool("cv_autolog", cfg.CVAutoLog),
			slog.Int("pagestep", cfg.PageStep))
	}

	cfg.NodePath = nodePath
	conf.Set(cfg)
	pushover := &notify.Pushover{
		UserKey:  cfg.PushoverUserKey,
		AppToken: cfg.PushoverAppToken,
		APIURL:   os.Getenv("PUSHOVER_API_URL"),
	}
	publicURL := os.Getenv("PUBLIC_URL")
	api.RegisterRoutes(r, pgStore, pushover, publicURL)
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
	web.RegisterRoutes(r, pgStore)

	// Expire the Voltra "motor engaged" claim when the sidecar stops
	// reporting. Correcting on read is not enough — the workout page reads the
	// state once and then follows SSE, so without something publishing the
	// transition the UI keeps saying "loaded — recording" after the trainer has
	// physically released.
	go voltra.WatchStale(context.Background(), 5*time.Second)

	// Treadmill cardio auto-capture: subscribe to the smart-plug wattage feed
	// zwave-js-ui publishes to MQTT and log detected workouts as cardio. Inert
	// unless TREADMILL_MQTT_ENABLED=true; never blocks startup.
	if tmCfg := treadmill.ConfigFromEnv(); tmCfg.Enabled {
		tc, err := treadmill.Start(tmCfg, pgStore)
		if err != nil {
			slog.Error("treadmill: consumer failed to start", slog.Any("error", err))
		} else {
			defer tc.Stop()
			slog.Info("treadmill MQTT consumer started",
				slog.String("broker", tmCfg.Broker),
				slog.String("topic", tmCfg.Topic),
				slog.Float64("watts_threshold", tmCfg.Threshold))
		}
	}

	addr := host + ":" + port
	slog.Info("PUMP ready", slog.String("addr", "http://"+addr))

	if err := r.Run(addr); err != nil {
		slog.Error("server failed", slog.Any("error", err))
		os.Exit(1)
	}
}
