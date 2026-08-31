package treadmill

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

const (
	defaultTopic       = "zwave/Gym/Treadmill/50/0/value/66049" // Z-Wave Meter CC (50), Electric W
	defaultThreshold   = 50.0                                   // watts; the plug's idle draw sits well below
	defaultOffDebounce = 60 * time.Second
	defaultMinSession  = 120 * time.Second
	defaultSessionType = "Treadmill"
	tickInterval       = 15 * time.Second // how often the debounce window is re-evaluated
)

// Config is the treadmill MQTT consumer configuration, read from the
// environment. The consumer is inert unless Enabled is true.
type Config struct {
	Enabled     bool
	Broker      string // e.g. tcp://emqx-headless.home.svc.cluster.local:1883
	Username    string
	Password    string
	Topic       string
	Threshold   float64
	OffDebounce time.Duration
	MinSession  time.Duration
	SessionType string
}

// recorder is the slice of the store the consumer needs: persisting a cardio
// HealthRecord. *store.PostgresStore satisfies it (store.HealthStore).
type recorder interface {
	InsertHealthRecords(ctx context.Context, recs []models.HealthRecord) (int, error)
}

// Consumer subscribes to the treadmill wattage topic and records a cardio
// session whenever the Detector reports one.
type Consumer struct {
	cfg    Config
	store  recorder
	client mqtt.Client

	mu   sync.Mutex // guards det (MQTT callback vs. tick loop)
	det  *Detector
	stop chan struct{}
}

// ConfigFromEnv builds a Config from the TREADMILL_MQTT_* environment.
func ConfigFromEnv() Config {
	return Config{
		Enabled:     os.Getenv("TREADMILL_MQTT_ENABLED") == "true",
		Broker:      os.Getenv("TREADMILL_MQTT_BROKER"),
		Username:    os.Getenv("TREADMILL_MQTT_USERNAME"),
		Password:    os.Getenv("TREADMILL_MQTT_PASSWORD"),
		Topic:       conf.EnvOr("TREADMILL_MQTT_TOPIC", defaultTopic),
		Threshold:   floatOr("TREADMILL_WATTS_THRESHOLD", defaultThreshold),
		OffDebounce: secondsOr("TREADMILL_OFF_DEBOUNCE_SECONDS", defaultOffDebounce),
		MinSession:  secondsOr("TREADMILL_MIN_SESSION_SECONDS", defaultMinSession),
		SessionType: conf.EnvOr("TREADMILL_SESSION_TYPE", defaultSessionType),
	}
}

// Start connects to the broker and begins consuming. It does not block on the
// initial connection — paho retries in the background — so a broker that is
// briefly unavailable never holds up PUMP startup. Returns an error only on
// invalid configuration.
func Start(cfg Config, store recorder) (*Consumer, error) {
	// Fail loud on misconfig. With ConnectRetry set, an empty/typo'd broker
	// otherwise produces a consumer that retries a bad address forever while
	// Start returns nil — the operator sees "consumer started" and believes
	// cardio auto-capture is live when it records nothing.
	if cfg.Broker == "" {
		return nil, fmt.Errorf("treadmill: enabled but TREADMILL_MQTT_BROKER is empty")
	}
	c := &Consumer{
		cfg:   cfg,
		store: store,
		det: &Detector{
			Threshold:   cfg.Threshold,
			OffDebounce: cfg.OffDebounce,
			MinSession:  cfg.MinSession,
		},
		stop: make(chan struct{}),
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID("pump-treadmill").
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetOnConnectHandler(c.onConnect).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			slog.Warn("treadmill: mqtt connection lost", slog.Any("error", err))
		})
	c.client = mqtt.NewClient(opts)

	// Connect without blocking: log the first attempt's outcome in the
	// background; reconnect/retry handlers keep trying thereafter.
	tok := c.client.Connect()
	go func() {
		if tok.Wait() && tok.Error() != nil {
			slog.Error("treadmill: initial mqtt connect failed", slog.Any("error", tok.Error()))
		}
	}()

	go c.tickLoop()
	return c, nil
}

// onConnect (re)subscribes on every successful connect so a reconnect restores
// the subscription.
func (c *Consumer) onConnect(client mqtt.Client) {
	slog.Info("treadmill: mqtt connected",
		slog.String("broker", c.cfg.Broker), slog.String("topic", c.cfg.Topic))
	if tok := client.Subscribe(c.cfg.Topic, 0, c.onMessage); tok.Wait() && tok.Error() != nil {
		slog.Error("treadmill: subscribe failed", slog.Any("error", tok.Error()))
	}
}

// onMessage feeds one wattage reading (received-now) into the detector.
func (c *Consumer) onMessage(_ mqtt.Client, msg mqtt.Message) {
	watts, ok := parseWatts(msg.Payload())
	if !ok {
		slog.Debug("treadmill: unparseable payload", slog.String("raw", string(msg.Payload())))
		return
	}
	c.mu.Lock()
	sess := c.det.Sample(time.Now(), watts)
	c.mu.Unlock()
	if sess != nil {
		c.record(*sess)
	}
}

// tickLoop re-evaluates the debounce window on a fixed cadence so a session
// still closes when the plug stops reporting after going idle.
func (c *Consumer) tickLoop() {
	// Recover + relaunch: a bare goroutine's panic is not covered by
	// gin.Recovery(), and a dead tickLoop silently stops closing idle sessions.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("treadmill: tickLoop panicked; restarting", slog.Any("panic", r))
			go c.tickLoop()
		}
	}()
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case now := <-t.C:
			c.mu.Lock()
			sess := c.det.Tick(now)
			c.mu.Unlock()
			if sess != nil {
				c.record(*sess)
			}
		}
	}
}

// record persists a completed session as an "exercise" HealthRecord. The store
// dedupes on (metric_type, start_time, end_time), so a re-emitted identical
// session is a harmless no-op.
func (c *Consumer) record(s Session) {
	durSec := s.Duration().Seconds()
	val := decimal.NewFromFloat(durSec)
	end := s.End
	extra, _ := json.Marshal(map[string]string{"type": c.cfg.SessionType})

	rec := models.HealthRecord{
		MetricType: "exercise",
		StartTime:  s.Start,
		EndTime:    &end,
		Value:      &val,
		Unit:       "seconds",
		Extra:      extra,
		Source:     "treadmill-mqtt",
	}
	inserted, err := c.store.InsertHealthRecords(context.Background(), []models.HealthRecord{rec})
	if err != nil {
		slog.Error("treadmill: failed to record session", slog.Any("error", err))
		return
	}
	slog.Info("treadmill: session recorded",
		slog.Time("start", s.Start),
		slog.Time("end", s.End),
		slog.Float64("minutes", durSec/60),
		slog.Int("inserted", inserted))
}

// Stop ends the tick loop and disconnects the MQTT client.
func (c *Consumer) Stop() {
	close(c.stop)
	c.client.Disconnect(250)
}

// parseWatts reads a wattage value from a zwave-js-ui meter payload. The
// gateway's default is a JSON object {"time":<ms>,"value":<number>}; if
// "publish values as raw" is enabled it is a bare number. Both are handled.
func parseWatts(payload []byte) (float64, bool) {
	var obj struct {
		Value *float64 `json:"value"`
	}
	if err := json.Unmarshal(payload, &obj); err == nil && obj.Value != nil {
		return *obj.Value, true
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(string(payload)), 64); err == nil {
		return f, true
	}
	return 0, false
}

func floatOr(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func secondsOr(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return def
}
