// Package conf loads PUMP's configuration from the environment and holds
// the single runtime-mutable copy shared by the api and web layers.
package conf

import (
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// current is the active config. Handlers read it per-request via Get and
// the settings endpoints replace it wholesale via Set, so concurrent
// readers never observe a half-written config.
var current atomic.Pointer[models.Conf]

// Set replaces the active config.
func Set(cfg models.Conf) {
	current.Store(&cfg)
}

// Get returns a copy of the active config.
func Get() models.Conf {
	if c := current.Load(); c != nil {
		return *c
	}
	return models.Conf{}
}

// Normalize clamps the user-editable numeric settings to safe floors. Both
// config write paths (the JSON API and the web form) discard strconv errors,
// so a blank or negative field arrives as zero; without this a PageStep of 0
// would be persisted and shipped back to the client as the page size. Floors
// match the env defaults in GetFromEnv.
func Normalize(c models.Conf) models.Conf {
	if c.PageStep < 1 {
		c.PageStep = 10
	}
	if c.DisplayDays < 1 {
		c.DisplayDays = 30
	}
	return c
}

// EnvOr returns the value of environment variable key, or def when unset.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt returns the integer value of environment variable key, or def
// when unset or unparsable.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envBool returns the boolean value of environment variable key, or def
// when unset or unparsable. Accepts the strconv.ParseBool forms
// (1/t/true/0/f/false, any case).
func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// envDecimal returns the decimal value of environment variable key, or def
// when unset or unparsable.
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

// weightMinLbs/weightMaxLbs bound plausible body weights accepted by any
// ingest path (the API scale endpoint and the manual web form both defer to
// WeightOutOfRange). PUMP does not trust a single upstream: the BLE-scale
// firmware enforces a tight band, and this is the backstop that keeps a
// physically-impossible reading — from any source, including a mistyped or
// blank manual entry — out of the store. Env-overridable; defaults are a
// generous human range so normal weigh-ins never trip it. Read once at import.
var (
	weightMinLbs = envDecimal("WEIGHT_MIN_LBS", decimal.NewFromInt(50))
	weightMaxLbs = envDecimal("WEIGHT_MAX_LBS", decimal.NewFromInt(500))
)

// WeightOutOfRange reports whether w falls outside the accepted plausibility
// band [weightMinLbs, weightMaxLbs] (inclusive). A blank manual entry parses to
// zero, which is below the default floor, so this also rejects empty weigh-ins.
func WeightOutOfRange(w decimal.Decimal) bool {
	return w.LessThan(weightMinLbs) || w.GreaterThan(weightMaxLbs)
}

// GetFromEnv reads all configuration from environment variables only.
// No config file is required.
func GetFromEnv() models.Conf {
	return models.Conf{
		Color:         EnvOr("COLOR", "dark"),
		PageStep:      envInt("PAGESTEP", 10),
		DisplayDays:   envInt("DISPLAY_DAYS", 30),
		AutoFill:      envBool("AUTOFILL", true),
		CVAutoLog:     envBool("CVAUTOLOG", false),
		VoltraAutoLog: envBool("VOLTRA_AUTOLOG", false),

		// Pushover credentials never land in any persisted config and are
		// not serialised through the JSON API.
		PushoverUserKey:  os.Getenv("PUSHOVER_USER_KEY"),
		PushoverAppToken: os.Getenv("PUSHOVER_APP_TOKEN"),
	}
}
