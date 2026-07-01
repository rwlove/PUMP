package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func statsHandler(c *gin.Context) {
	exs, ok := selectExOr500(c, "statsHandler")
	if !ok {
		return
	}
	sets, ok := selectSetsOr500(c, "statsHandler")
	if !ok {
		return
	}
	weights, ok := selectWeightsOr500(c, "statsHandler")
	if !ok {
		return
	}

	sort.Slice(sets, func(i, j int) bool { return sets[i].Date < sets[j].Date })
	sort.Slice(weights, func(i, j int) bool { return weights[i].Date < weights[j].Date })
	cfg := conf.Get()
	sortExsByFrequency(exs, sets, cfg.FrequencyDays)

	var guiData models.GuiData
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = sets
	guiData.ExData.Weight = weights
	guiData.ServerDate = time.Now().Format("2006-01-02")
	guiData.Health = loadHealthStats()

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "stats.html", guiData)
}
