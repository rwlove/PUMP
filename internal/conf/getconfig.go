// Package conf loads PUMP's configuration from the environment and holds
// the single runtime-mutable copy shared by the api and web layers.
package conf

import (
	"os"
	"strconv"
	"sync/atomic"

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
