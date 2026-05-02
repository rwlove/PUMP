package conf

import (
	"github.com/spf13/viper"

	"github.com/rwlove/PUMP/internal/models"
)

// GetFromEnv reads all configuration from environment variables only.
// No config file is required.
func GetFromEnv() models.Conf {
	v := viper.New()

	v.SetDefault("HOST", "0.0.0.0")
	v.SetDefault("PORT", "8080")
	v.SetDefault("THEME", "cosmo")
	v.SetDefault("COLOR", "dark")
	v.SetDefault("HEATCOLOR", "#2780e3")
	v.SetDefault("PAGESTEP", 10)
	v.SetDefault("FREQUENCY_DAYS", 30)
	v.SetDefault("DISPLAY_DAYS", 30)
	v.SetDefault("AUTOFILL", true)

	v.AutomaticEnv()

	var config models.Conf
	config.Host = v.GetString("HOST")
	config.Port = v.GetString("PORT")
	config.Theme = v.GetString("THEME")
	config.Color = v.GetString("COLOR")
	config.HeatColor = v.GetString("HEATCOLOR")
	config.PageStep = v.GetInt("PAGESTEP")
	config.FrequencyDays = v.GetInt("FREQUENCY_DAYS")
	config.DisplayDays = v.GetInt("DISPLAY_DAYS")
	config.AutoFill = v.GetBool("AUTOFILL")

	return config
}

// Write persists config to the YAML file at config.ConfPath.
// It is a no-op when ConfPath is empty (env-var-only mode).
func Write(config models.Conf) {
	if config.ConfPath == "" {
		return
	}

	v := viper.New()
	v.SetConfigFile(config.ConfPath)
	v.SetConfigType("yaml")

	v.Set("host", config.Host)
	v.Set("port", config.Port)
	v.Set("theme", config.Theme)
	v.Set("color", config.Color)
	v.Set("heatcolor", config.HeatColor)
	v.Set("pagestep", config.PageStep)
	v.Set("frequency_days", config.FrequencyDays)
	v.Set("display_days", config.DisplayDays)
	v.Set("autofill", config.AutoFill)

	if err := v.WriteConfig(); err != nil {
		// Best-effort — don't crash when running in env-var-only mode
		// and the config file path was never created.
		return
	}
}
