package web

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// healthHandler renders the Overall Health dashboard — a one-page view that
// pulls from every source PUMP tracks: body weight, strength training, and
// wearable metrics (steps / heart rate / sleep / cardio from Health Connect).
// Each tile deep-links into the matching Stats tab.
func healthHandler(c *gin.Context) {
	sets, err := dataStore.SelectSet()
	if err != nil {
		slog.Error("healthHandler: SelectSet failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	weights, err := dataStore.SelectW()
	if err != nil {
		slog.Error("healthHandler: SelectW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	sort.Slice(sets, func(i, j int) bool { return sets[i].Date < sets[j].Date })
	sort.Slice(weights, func(i, j int) bool { return weights[i].Date < weights[j].Date })

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Sets = sets
	guiData.ExData.Weight = weights
	guiData.ServerDate = time.Now().Format("2006-01-02")
	guiData.Health = loadHealthStats()

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "health.html", guiData)
}
