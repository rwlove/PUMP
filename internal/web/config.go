package web

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
	appConfig.CVAutoLog = c.PostForm("cvautolog") == "on"

	// Notify the API layer of the new config.
	if configSaveHook != nil {
		configSaveHook(appConfig)
	}

	c.Redirect(http.StatusFound, "/config")
}
