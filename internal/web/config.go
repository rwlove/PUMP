package web

import (
	"log/slog"
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

	// The muscle-catalog editor lives on this page. Show the current groups
	// (from exercises + catalog) and their muscles.
	exs, err := dataStore.SelectEx(c.Request.Context())
	if err != nil {
		slog.Warn("configHandler: SelectEx failed", slog.Any("error", err))
	}
	muscles := selectMusclesSoft(c, "configHandler")
	guiData.Muscles = muscles
	guiData.GroupMap = mergeGroupList(exs, muscles)

	c.HTML(http.StatusOK, "config.html", guiData)
}

func saveConfigHandler(c *gin.Context) {
	cfg := conf.Get()
	cfg.Color = c.PostForm("color")
	cfg.PageStep, _ = strconv.Atoi(c.PostForm("pagestep"))
	cfg.DisplayDays, _ = strconv.Atoi(c.PostForm("displaydays"))
	cfg.AutoFill = c.PostForm("autofill") == "on"
	cfg.CVAutoLog = c.PostForm("cvautolog") == "on"
	cfg = conf.Normalize(cfg)
	conf.Set(cfg)

	// Persist so the change survives a pod restart. Failure here is
	// logged but not fatal — the in-memory config is already updated, so
	// the UI reflects the change until the next restart.
	if err := dataStore.SaveAppConfig(c.Request.Context(), cfg); err != nil {
		slog.Error("saveConfigHandler: SaveAppConfig failed",
			slog.Any("error", err))
	}

	c.Redirect(http.StatusFound, "/config")
}
