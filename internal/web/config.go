package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
	appConfig.DisplayDays, _ = strconv.Atoi(c.PostForm("displaydays"))
	appConfig.AutoFill = c.PostForm("autofill") == "on"

	if apiClient != nil {
		// Split-frontend: persist config via API
		if err := apiClient.SaveConfig(appConfig); err != nil {
			log.Println("ERROR saveConfigHandler SaveConfig:", err)
			c.Status(http.StatusInternalServerError)
			return
		}
	} else if configSaveHook != nil {
		// Monolith: notify the API layer of the new config
		configSaveHook(appConfig)
	} else {
		// Standalone: write config file directly
		conf.Write(appConfig)
		log.Println("INFO: writing new config to", appConfig.ConfPath)
	}

	c.Redirect(http.StatusFound, "/config")
}
