package web

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

func indexHandler(c *gin.Context) {
	var guiData models.GuiData

	exs, err := dataStore.SelectEx()
	if err != nil {
		log.Println("ERROR indexHandler SelectEx:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		log.Println("ERROR indexHandler SelectSet:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	weights, err := dataStore.SelectW()
	if err != nil {
		log.Println("ERROR indexHandler SelectW:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Backfill colors for any exercise that lacks one (one-time operation).
	if needsColorBackfill(exs) {
		backfillColors(exs)
		// Reload so the template sees the updated colors.
		if refreshed, err := dataStore.SelectEx(); err == nil {
			exs = refreshed
			sortExsByFrequency(exs, sets, appConfig.FrequencyDays)
		}
	}

	exData.Exs = exs
	exData.Sets = sets
	exData.Weight = weights

	guiData.Config = appConfig
	guiData.ExData = exData
	guiData.GroupMap = createGroupMap()

	// Heatmap and frequency sorting always use the full set history.
	guiData.ColorHeatMap = generateHeatMap(exData.Sets)
	sortExsByFrequency(guiData.ExData.Exs, sets, appConfig.FrequencyDays)

	// Limit sets sent to the main page to the configured display window.
	days := appConfig.DisplayDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var displaySets []models.Set
	for _, s := range sets {
		if s.Date >= cutoff {
			displaySets = append(displaySets, s)
		}
	}
	guiData.ExData.Sets = displaySets

	sort.Slice(guiData.ExData.Weight, func(i, j int) bool {
		return guiData.ExData.Weight[i].Date < guiData.ExData.Weight[j].Date
	})

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "index.html", guiData)
}

func createGroupMap() map[string]string {
	i := 0
	grMap := make(map[string]string)
	for _, ex := range exData.Exs {
		if _, ok := grMap[ex.Group]; !ok {
			grMap[ex.Group] = "grID" + fmt.Sprintf("%d", i)
			i++
		}
	}
	return grMap
}

// sortExsByFrequency sorts exs in-place by how many times each was used in
// the last `days` days (descending). Ties are broken by exercise Place field.
func sortExsByFrequency(exs []models.Exercise, sets []models.Set, days int) {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	count := make(map[string]int, len(exs))
	for _, s := range sets {
		if s.Date >= cutoff {
			count[s.Name]++
		}
	}
	sort.SliceStable(exs, func(i, j int) bool {
		ci, cj := count[exs[i].Name], count[exs[j].Name]
		if ci != cj {
			return ci > cj
		}
		return exs[i].Place < exs[j].Place
	})
}
