package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func configHandler(c *gin.Context) {
	var guiData models.GuiData

	guiData.Config = appConfig
	guiData.Version = Version

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "config.html", guiData)
}

func saveConfigHandler(c *gin.Context) {
	appConfig.Color = c.PostForm("color")
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
