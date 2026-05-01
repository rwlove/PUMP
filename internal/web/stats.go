package web

import (
	"log"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/rwlove/PUMP/internal/models"
)

func statsHandler(c *gin.Context) {
	exs, err := dataStore.SelectEx()
	if err != nil {
		log.Println("ERROR statsHandler SelectEx:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		log.Println("ERROR statsHandler SelectSet:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	weights, err := dataStore.SelectW()
	if err != nil {
		log.Println("ERROR statsHandler SelectW:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	sort.Slice(sets, func(i, j int) bool { return sets[i].Date < sets[j].Date })
	sort.Slice(weights, func(i, j int) bool { return weights[i].Date < weights[j].Date })

	// Sort exercises by usage frequency for the Activity legend
	sortExsByFrequency(exs, sets, appConfig.FrequencyDays)

	// Build exercise name list from sets for the history dropdown
	seen := make(map[string]bool)
	groupMap := make(map[string]string)
	for _, s := range sets {
		if !seen[s.Name] {
			groupMap[s.Name] = s.Name
			seen[s.Name] = true
		}
	}

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = sets
	guiData.ExData.Weight = weights
	guiData.GroupMap = groupMap
	guiData.ColorHeatMap = generateHeatMap(sets)

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "stats.html", guiData)
}
