package conf

import (
	"github.com/spf13/viper"

	"github.com/rwlove/PUMP/internal/auth"
	"github.com/rwlove/PUMP/internal/models"
)

// GetFromEnv reads all configuration from environment variables only.
// No config file is required. This is the primary configuration path for
// the split-service deployment.
func GetFromEnv() (config models.Conf, authConf auth.Conf) {
	v := viper.New()

	v.SetDefault("HOST", "0.0.0.0")
	v.SetDefault("PORT", "8851")
	v.SetDefault("THEME", "cosmo")
	v.SetDefault("COLOR", "dark")
	v.SetDefault("HEATCOLOR", "#03a70c")
	v.SetDefault("PAGESTEP", 10)
	v.SetDefault("FREQUENCY_DAYS", 30)
	v.SetDefault("AUTH_EXPIRE", "7d")

	v.AutomaticEnv()

	config.Host = v.GetString("HOST")
	config.Port = v.GetString("PORT")
	config.Theme = v.GetString("THEME")
	config.Color = v.GetString("COLOR")
	config.HeatColor = v.GetString("HEATCOLOR")
	config.PageStep = v.GetInt("PAGESTEP")
	config.FrequencyDays = v.GetInt("FREQUENCY_DAYS")

	authConf.Auth = v.GetBool("AUTH")
	authConf.User = v.GetString("AUTH_USER")
	authConf.Password = v.GetString("AUTH_PASSWORD")
	authConf.ExpStr = v.GetString("AUTH_EXPIRE")
	authConf.Expire = auth.ToTime(authConf.ExpStr)
	config.Auth = authConf.Auth

	return config, authConf
}

// Write persists config to the YAML file at config.ConfPath.
// It is a no-op when ConfPath is empty (env-var-only mode).
func Write(config models.Conf, authConf auth.Conf) {
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

	v.Set("auth", authConf.Auth)
	v.Set("auth_user", authConf.User)
	v.Set("auth_password", authConf.Password)
	v.Set("auth_expire", authConf.ExpStr)

	if err := v.WriteConfig(); err != nil {
		// Best-effort — don't crash when running in env-var-only mode
		// and the config file path was never created.
		return
	}
}
