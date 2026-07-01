package conf

import (
	"os"

	"github.com/spf13/viper"

	"github.com/rwlove/PUMP/internal/models"
)

// GetFromEnv reads all configuration from environment variables only.
// No config file is required.
func GetFromEnv() models.Conf {
	v := viper.New()

	v.SetDefault("COLOR", "dark")
	v.SetDefault("PAGESTEP", 10)
	v.SetDefault("FREQUENCY_DAYS", 30)
	v.SetDefault("DISPLAY_DAYS", 30)
	v.SetDefault("AUTOFILL", true)
	v.SetDefault("CVAUTOLOG", false)

	v.AutomaticEnv()

	var config models.Conf
	config.Color = v.GetString("COLOR")
	config.PageStep = v.GetInt("PAGESTEP")
	config.FrequencyDays = v.GetInt("FREQUENCY_DAYS")
	config.DisplayDays = v.GetInt("DISPLAY_DAYS")
	config.AutoFill = v.GetBool("AUTOFILL")
	config.CVAutoLog = v.GetBool("CVAUTOLOG")

	// Pushover credentials are read from env directly so they don't pass
	// through Viper's case-folding and never land in any persisted config.
	config.PushoverUserKey = os.Getenv("PUSHOVER_USER_KEY")
	config.PushoverAppToken = os.Getenv("PUSHOVER_APP_TOKEN")

	return config
}
