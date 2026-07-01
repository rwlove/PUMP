package web

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func configHandler(c *gin.Context) {
	var guiData models.GuiData

	guiData.Config = conf.Get()
	guiData.Version = Version

	c.HTML(http.StatusOK, "config.html", guiData)
}

func saveConfigHandler(c *gin.Context) {
	cfg := conf.Get()
	cfg.Color = c.PostForm("color")
	cfg.PageStep, _ = strconv.Atoi(c.PostForm("pagestep"))
	cfg.FrequencyDays, _ = strconv.Atoi(c.PostForm("frequencydays"))
	cfg.DisplayDays, _ = strconv.Atoi(c.PostForm("displaydays"))
	cfg.AutoFill = c.PostForm("autofill") == "on"
	cfg.CVAutoLog = c.PostForm("cvautolog") == "on"
	conf.Set(cfg)

	c.Redirect(http.StatusFound, "/config")
}
