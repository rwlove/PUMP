package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/notify"
	"github.com/rwlove/PUMP/internal/store"
	"github.com/rwlove/PUMP/internal/voltra"
	"github.com/shopspring/decimal"
)

var (
	dataStore store.Store
	pushover  *notify.Pushover // may be nil; safe to invoke either way
	publicURL string           // base URL for deep-links in notifications
	// weightIngestKey, when non-empty, requires POST /api/weight callers to
	// present a matching X-Api-Key header. Empty preserves the legacy
	// no-inbound-auth posture for in-cluster callers and the manual
	// web-form path (which goes through /wadd/, not /api/weight).
	weightIngestKey string
	// weightMinLbs/weightMaxLbs bound plausible body weights accepted by
	// POST /api/weight. PUMP does not trust a single upstream — the BLE-scale
	// firmware enforces a tight band, and this is the backstop that keeps a
	// physically-impossible reading (from any source) out of the store.
	// Overridable via WEIGHT_MIN_LBS / WEIGHT_MAX_LBS; defaults are a generous
	// human range so normal weigh-ins never trip it.
	weightMinLbs = decimal.NewFromInt(50)
	weightMaxLbs = decimal.NewFromInt(500)
)

// RegisterRoutes mounts all API routes on r using the provided store.
// Config is read from the shared conf holder. p may be nil (notifications
// disabled). pubURL is the externally-reachable PUMP base URL used to
// build deep-links in notifications; may be empty (notifications still
// fire, just without a clickable URL).
//
// Used by cmd/pump (monolith). Does not call r.Run().
func RegisterRoutes(r *gin.Engine, s *store.PostgresStore, p *notify.Pushover, pubURL string) {
	dataStore = s
	healthStore = s
	pushover = p
	publicURL = pubURL
	weightIngestKey = os.Getenv("WEIGHT_INGEST_KEY")
	healthIngestKey = os.Getenv("HEALTH_INGEST_KEY")
	weightMinLbs = envDecimal("WEIGHT_MIN_LBS", weightMinLbs)
	weightMaxLbs = envDecimal("WEIGHT_MAX_LBS", weightMaxLbs)
	registerRoutes(r)
	slog.Debug("api routes registered",
		slog.Bool("weight_ingest_key_configured", weightIngestKey != ""),
		slog.Bool("health_ingest_key_configured", healthIngestKey != ""))
}

// envDecimal reads a decimal from env var key, falling back to def when the
// var is unset or unparseable.
func envDecimal(key string, def decimal.Decimal) decimal.Decimal {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		slog.Warn("invalid decimal env var; using default",
			slog.String("key", key), slog.String("value", v),
			slog.String("default", def.String()))
		return def
	}
	return d
}

// registerRoutes mounts all API routes on r.
func registerRoutes(r *gin.Engine) {
	// Health / connectivity probe (no auth required)
	r.GET("/api/ping", getPing)

	// Exercises
	r.GET("/api/exercises", getExercises)
	r.POST("/api/exercises", postExercise)
	r.PUT("/api/exercises/:id", putExercise)
	r.PATCH("/api/exercises/:id/color", patchExerciseColor)
	r.DELETE("/api/exercises/:id", deleteExercise)

	// Sets
	r.GET("/api/sets", getSets)
	r.GET("/api/sets/stream", getSetsStream)
	r.PUT("/api/sets/date/:date", putSetsByDate)
	r.GET("/api/sets/:id", getSet)
	r.POST("/api/sets", postSet)
	r.PATCH("/api/sets/:id", patchSet)
	r.POST("/api/sets/:id/confirm", postSetConfirm)
	r.DELETE("/api/sets/:id", deleteSet)

	// Voltra phase-2 coordination between the workout UI and the sidecar.
	r.GET("/api/voltra/state", getVoltraState)
	r.GET("/api/voltra/stream", getVoltraStream)
	r.POST("/api/voltra/arm", postVoltraArm)
	r.POST("/api/voltra/disarm", postVoltraDisarm)
	r.POST("/api/voltra/load", postVoltraLoad)
	r.POST("/api/voltra/report", postVoltraReport)

	// Body weight, gated by WEIGHT_INGEST_KEY. Off-cluster callers (the
	// BLE-scale ESPHome firmware) reach these via a path-scoped Route that
	// bypasses gateway auth, so the header check is the only auth in that
	// path.
	//
	// All three verbs are gated, not just POST. The Route matches on
	// PathPrefix /api/weight with no method restriction, so GET and DELETE
	// were reachable off-cluster unauthenticated — readable weight history and
	// a destructive delete. Gating here rather than only narrowing the Route
	// keeps it true regardless of how the ingress is configured later. No
	// browser code calls these; the UI renders body weight server-side.
	weightAuth := ingestAuth(weightIngestKey, "weight ingest")
	r.GET("/api/weight", weightAuth, getWeight)
	r.POST("/api/weight", weightAuth, postWeight)
	r.DELETE("/api/weight/:id", weightAuth, deleteWeight)

	// Wearable health (Android Health Connect via the HC Webhook bridge).
	// POST is gated by HEALTH_INGEST_KEY for off-cluster ingest, mirroring
	// the /api/weight path-scoped Route.
	r.GET("/api/health", getHealth)
	r.POST("/api/health", ingestAuth(healthIngestKey, "health ingest"), postHealth)

	// Config
	r.GET("/api/config", getConfig)
	r.PUT("/api/config", putConfig)

	// Wall display
	r.GET("/api/wall/stream", getWallStream)
	r.POST("/api/wall/wake", postWallWake)
	r.POST("/api/wall/sleep", postWallSleep)

	// Notifications
	r.POST("/api/notify/test", postNotifyTest)
}

// getPing is an unauthenticated liveness probe API clients can use
// to verify connectivity before committing settings.
func getPing(c *gin.Context) {
	slog.Debug("ping", slog.String("ip", c.ClientIP()))
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ─── exercises ────────────────────────────────────────────────────────────────

func getExercises(c *gin.Context) {
	exs, err := dataStore.SelectEx(c.Request.Context())
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
	if err := dataStore.InsertEx(c.Request.Context(), ex); err != nil {
		slog.Error("postExercise: InsertEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("exercise created", slog.String("name", ex.Name))
	c.Status(http.StatusCreated)
}

func putExercise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var ex models.Exercise
	if err := c.ShouldBindJSON(&ex); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ex.ID = id
	slog.Debug("putExercise", slog.Int("id", id), slog.String("name", ex.Name))
	found, err := dataStore.UpdateEx(c.Request.Context(), ex)
	if err != nil {
		slog.Error("putExercise: UpdateEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !found {
		// Unknown id: create the exercise, preserving the old
		// delete+insert upsert behavior (the row gets a fresh id).
		if err := dataStore.InsertEx(c.Request.Context(), ex); err != nil {
			slog.Error("putExercise: InsertEx failed", slog.Any("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	slog.Info("exercise updated", slog.Int("id", id), slog.String("name", ex.Name))
	c.Status(http.StatusOK)
}

func patchExerciseColor(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
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
	if err := dataStore.UpdateExColor(c.Request.Context(), id, body.Color); err != nil {
		slog.Error("patchExerciseColor: UpdateExColor failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func deleteExercise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	slog.Debug("deleteExercise", slog.Int("id", id))
	if err := dataStore.DeleteEx(c.Request.Context(), id); err != nil {
		slog.Error("deleteExercise: DeleteEx failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("exercise deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

// ─── sets ─────────────────────────────────────────────────────────────────────

func getSets(c *gin.Context) {
	sets, err := dataStore.SelectSet(c.Request.Context())
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
	if err := dataStore.BulkReplaceSetsByDate(c.Request.Context(), date, sets); err != nil {
		slog.Error("putSetsByDate: BulkReplaceSetsByDate failed",
			slog.String("date", date), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("sets saved", slog.String("date", date), slog.Int("count", len(sets)))
	publishSetEvent(SetEvent{Type: SetEventBulk, Date: date}, clientID(c))
	c.Status(http.StatusOK)
}

// getSetsStream is an SSE endpoint that broadcasts add/update/delete/bulk
// events for sets. Clients should use the standard SSE format:
//
//	event: add
//	data: {"type":"add","id":42,"date":"2026-05-03","set":{...}}
//
// A keepalive comment is sent every 25 s to keep idle connections open
// through proxies. Slow subscribers have events dropped silently.
func getSetsStream(c *gin.Context) {
	ch, unsub := setBroker.subscribe(c.Query("client"))
	defer unsub()

	if !sseHeaders(c) {
		return
	}
	sseLoop(c, ch, func(ev SetEvent) string { return string(ev.Type) })
}

func getSet(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	set, err := dataStore.GetSet(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, set)
}

func postSet(c *gin.Context) {
	var set models.Set
	if err := c.ShouldBindJSON(&set); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Gate sidecar writes on the operator's opt-in toggles. Manual writes are
	// always accepted. Refusing loudly beats accepting silently: a sidecar
	// pointed at a server with its toggle off should fail visibly rather than
	// look like it is working.
	cfg := conf.Get()
	switch {
	case set.Source == "cv" && !cfg.CVAutoLog:
		slog.Warn("postSet: refused CV write — CVAutoLog is off",
			slog.String("date", set.Date), slog.String("name", set.Name))
		c.JSON(http.StatusForbidden, gin.H{"error": "CV auto-log is disabled"})
		return
	case set.Source == "voltra" && !cfg.VoltraAutoLog:
		slog.Warn("postSet: refused Voltra write — VoltraAutoLog is off",
			slog.String("date", set.Date), slog.String("name", set.Name))
		c.JSON(http.StatusForbidden, gin.H{"error": "Voltra auto-log is disabled"})
		return
	case set.Source == "voltra" && !voltra.Get().Recording():
		// Belt and braces with the sidecar's own gate. The sidecar can be
		// mid-reconnect holding stale state, and a set recorded while the
		// motor was not engaged at PUMP's weight would claim a load that was
		// never applied.
		st := voltra.Get()
		slog.Warn("postSet: refused Voltra write — not armed and loaded",
			slog.Int("armed_set_id", st.ArmedSetID), slog.Bool("loaded", st.Loaded))
		c.JSON(http.StatusConflict,
			gin.H{"error": "no Voltra set is armed and loaded"})
		return
	}
	stored, err := dataStore.InsertSet(c.Request.Context(), set)
	if err != nil {
		slog.Error("postSet: InsertSet failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("set inserted",
		slog.Int("id", stored.ID),
		slog.String("date", stored.Date),
		slog.String("name", stored.Name),
		slog.String("source", stored.Source),
		slog.Bool("pending", stored.Pending),
	)
	publishSetEvent(SetEvent{Type: SetEventAdd, ID: stored.ID, Date: stored.Date, Set: &stored}, clientID(c))
	if stored.Pending {
		pushover.SendAsync(buildPendingSetMessage(stored))
	}
	c.JSON(http.StatusCreated, gin.H{"id": stored.ID})
}

// buildPendingSetMessage formats a Pushover notification for a low-
// confidence CV set that needs the athlete to confirm or correct.
func buildPendingSetMessage(s models.Set) notify.Message {
	conf := int(s.Confidence*100 + 0.5)
	body := fmt.Sprintf("%s · %s lb × %d (%d%% confidence)",
		s.Name, s.Weight.String(), s.Reps, conf)
	if s.Note != "" {
		body += "\n" + s.Note
	}
	m := notify.Message{
		Title: "PUMP — confirm set",
		Body:  body,
	}
	if publicURL != "" {
		m.URL = strings.TrimRight(publicURL, "/") + "/"
		m.URLTitle = "Open PUMP"
	}
	return m
}

func patchSet(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var upd models.SetUpdate
	if err := c.ShouldBindJSON(&upd); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stored, err := dataStore.UpdateSet(c.Request.Context(), id, upd)
	if err != nil {
		slog.Error("patchSet: UpdateSet failed", slog.Int("id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("set updated", slog.Int("id", id))
	if stored.ID != 0 {
		publishSetEvent(SetEvent{Type: SetEventUpdate, ID: id, Date: stored.Date, Set: &stored}, clientID(c))
	}
	c.Status(http.StatusOK)
}

// postSetConfirm clears the pending flag and optionally applies edits from
// the body in a single update.
func postSetConfirm(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var upd models.SetUpdate
	// Body is optional — an empty body just clears pending — but a body
	// that is present and malformed is rejected instead of ignored.
	if err := c.ShouldBindJSON(&upd); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pendingFalse := false
	upd.Pending = &pendingFalse
	stored, err := dataStore.UpdateSet(c.Request.Context(), id, upd)
	if err != nil {
		slog.Error("postSetConfirm: UpdateSet failed", slog.Int("id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("set confirmed", slog.Int("id", id))
	if stored.ID != 0 {
		publishSetEvent(SetEvent{Type: SetEventUpdate, ID: id, Date: stored.Date, Set: &stored}, clientID(c))
	}
	c.Status(http.StatusOK)
}

// ─── wall display ─────────────────────────────────────────────────────────────

// getWallStream is an SSE endpoint for wall-mounted kiosks. It carries
// wake/sleep events plus a build_changed event so kiosks self-reload on
// deploy. Set lifecycle (add/update/delete) stays on /api/sets/stream;
// the wall just subscribes to both.
func getWallStream(c *gin.Context) {
	ch, lastSHA, unsub := wallBroker.subscribe()
	defer unsub()

	if !sseHeaders(c) {
		return
	}

	// Tell the freshly-connected kiosk what build is currently live so
	// it can reload if it loaded against a stale build.
	if lastSHA != "" {
		if !sseEvent(c, string(WallEventBuildChanged),
			WallEvent{Type: WallEventBuildChanged, Data: lastSHA}) {
			return
		}
	}

	sseLoop(c, ch, func(ev WallEvent) string { return string(ev.Type) })
}

func postWallWake(c *gin.Context) {
	publishWallEvent(WallEvent{Type: WallEventWake})
	slog.Debug("wall: wake")
	c.Status(http.StatusOK)
}

func postWallSleep(c *gin.Context) {
	publishWallEvent(WallEvent{Type: WallEventSleep})
	slog.Debug("wall: sleep")
	c.Status(http.StatusOK)
}

// postNotifyTest fires a one-off Pushover notification so the operator
// can verify their credentials without waiting for a real pending set.
// Returns 412 when Pushover isn't configured, 502 when the upstream
// rejects the credentials.
func postNotifyTest(c *gin.Context) {
	if !pushover.Configured() {
		c.JSON(http.StatusPreconditionFailed,
			gin.H{"error": "Pushover not configured"})
		return
	}
	url := publicURL
	if url == "" {
		url = "https://pushover.net/"
	}
	msg := notify.Message{
		Title: "PUMP — test notification",
		Body:  "If you see this on your phone, Pushover credentials are wired correctly.",
		URL:   url,
	}
	if err := pushover.Send(c.Request.Context(), msg); err != nil {
		slog.Warn("notify test failed", slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

func deleteSet(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	date, err := dataStore.DeleteSet(c.Request.Context(), id)
	if err != nil {
		slog.Error("deleteSet: DeleteSet failed", slog.Int("id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("set deleted", slog.Int("id", id))
	publishSetEvent(SetEvent{Type: SetEventDelete, ID: id, Date: date}, clientID(c))
	c.Status(http.StatusNoContent)
}

// ─── weight ───────────────────────────────────────────────────────────────────

func getWeight(c *gin.Context) {
	ws, err := dataStore.SelectW(c.Request.Context())
	if err != nil {
		slog.Error("getWeight: SelectW failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("getWeight", slog.Int("count", len(ws)))
	c.JSON(http.StatusOK, ws)
}

// weightOutOfRange reports whether w falls outside the accepted plausibility
// band [weightMinLbs, weightMaxLbs] (inclusive).
func weightOutOfRange(w decimal.Decimal) bool {
	return w.LessThan(weightMinLbs) || w.GreaterThan(weightMaxLbs)
}

func postWeight(c *gin.Context) {
	var w models.BodyWeight
	if err := c.ShouldBindJSON(&w); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("postWeight",
		slog.String("date", w.Date),
		slog.String("recorded_at", w.RecordedAt),
		slog.String("weight", w.Weight.String()))
	// Plausibility guard: reject physically-impossible body weights before the
	// store. The scale firmware enforces a tight band; this is the backstop
	// against a bad reading from any source (see weightMinLbs/weightMaxLbs).
	if weightOutOfRange(w.Weight) {
		slog.Warn("postWeight: rejected out-of-range weight",
			slog.String("date", w.Date),
			slog.String("recorded_at", w.RecordedAt),
			slog.String("weight", w.Weight.String()),
			slog.String("min", weightMinLbs.String()),
			slog.String("max", weightMaxLbs.String()))
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "weight out of plausible range"})
		return
	}
	if err := dataStore.InsertW(c.Request.Context(), w); err != nil {
		slog.Error("postWeight: InsertW failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("body weight logged", slog.String("date", w.Date))
	c.Status(http.StatusCreated)
}

func deleteWeight(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	slog.Debug("deleteWeight", slog.Int("id", id))
	if err := dataStore.DeleteW(c.Request.Context(), id); err != nil {
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
	c.JSON(http.StatusOK, conf.Get())
}

func putConfig(c *gin.Context) {
	var cfg models.Conf
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	slog.Info("config updated",
		slog.String("color", cfg.Color),
		slog.Int("pagestep", cfg.PageStep),
		slog.Bool("cv_autolog", cfg.CVAutoLog),
	)
	cur := conf.Get()
	cur.Color = cfg.Color
	cur.PageStep = cfg.PageStep
	cur.FrequencyDays = cfg.FrequencyDays
	cur.DisplayDays = cfg.DisplayDays
	cur.AutoFill = cfg.AutoFill
	cur.CVAutoLog = cfg.CVAutoLog
	// Pushover creds are env-only — never accepted from the API body.
	conf.Set(cur)
	// Persist for restart survival. Same soft-fail semantics as the web
	// save handler: log and keep the in-memory update.
	if err := dataStore.SaveAppConfig(c.Request.Context(), cur); err != nil {
		slog.Error("putConfig: SaveAppConfig failed", slog.Any("error", err))
	}
	c.Status(http.StatusOK)
}
