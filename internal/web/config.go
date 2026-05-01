package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/auth"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

var themes = []string{
	"cerulean", "cosmo", "cyborg", "darkly", "emerald", "flatly", "grass",
	"grayscale", "journal", "litera", "lumen", "lux", "materia", "minty",
	"morph", "ocean", "pulse", "quartz", "sand", "sandstone", "simplex",
	"sketchy", "slate", "solar", "spacelab", "superhero", "united", "vapor",
	"wood", "yeti", "zephyr",
}

func configHandler(c *gin.Context) {
	var guiData models.GuiData

	guiData.Config = appConfig
	guiData.Auth = authConf
	guiData.Themes = themes
	guiData.Version = Version

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "config.html", guiData)
}

func saveConfigHandler(c *gin.Context) {
	appConfig.Theme = c.PostForm("theme")
	appConfig.Color = c.PostForm("color")
	appConfig.HeatColor = c.PostForm("heatcolor")
	appConfig.PageStep, _ = strconv.Atoi(c.PostForm("pagestep"))
	appConfig.FrequencyDays, _ = strconv.Atoi(c.PostForm("frequencydays"))

	if apiClient != nil {
		// Split-frontend: persist config via API
		if err := apiClient.SaveConfig(appConfig); err != nil {
			log.Println("ERROR saveConfigHandler SaveConfig:", err)
			c.Status(http.StatusInternalServerError)
			return
		}
	} else {
		// Monolith: write config file directly
		conf.Write(appConfig, authConf)
		log.Println("INFO: writing new config to", appConfig.ConfPath)
	}

	c.Redirect(http.StatusFound, "/config")
}

func saveConfigAuth(c *gin.Context) {
	authConf.User = c.PostForm("user")
	authConf.ExpStr = c.PostForm("expire")
	authEnabled := c.PostForm("auth") == "on"
	pw := c.PostForm("password")

	authConf.Auth = authEnabled
	appConfig.Auth = authEnabled

	if pw != "" {
		authConf.Password = auth.HashPassword(pw)
	}

	authConf.Expire = auth.ToTime(authConf.ExpStr)

	if authConf.Auth && (authConf.User == "" || authConf.Password == "") {
		log.Println("WARNING: Auth won't work with empty login or password.")
		authConf.Auth = false
		appConfig.Auth = false
	}

	if apiClient != nil {
		if err := apiClient.SaveConfigAuth(authConf.User, pw, authConf.ExpStr, authConf.Auth); err != nil {
			log.Println("ERROR saveConfigAuth SaveConfigAuth:", err)
			c.Status(http.StatusInternalServerError)
			return
		}
	} else {
		conf.Write(appConfig, authConf)
	}

	c.Redirect(http.StatusFound, "/config")
}

