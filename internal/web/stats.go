package web

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/models"
)

func statsHandler(c *gin.Context) {
	exs, err := dataStore.SelectEx()
	if err != nil {
		slog.Error("statsHandler: SelectEx failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		slog.Error("statsHandler: SelectSet failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	weights, err := dataStore.SelectW()
	if err != nil {
		slog.Error("statsHandler: SelectW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	sort.Slice(sets, func(i, j int) bool { return sets[i].Date < sets[j].Date })
	sort.Slice(weights, func(i, j int) bool { return weights[i].Date < weights[j].Date })
	sortExsByFrequency(exs, sets, appConfig.FrequencyDays)

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = sets
	guiData.ExData.Weight = weights
	guiData.ServerDate = time.Now().Format("2006-01-02")

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "stats.html", guiData)
}
