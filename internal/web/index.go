package web

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

func indexHandler(c *gin.Context) {
	exs, err := dataStore.SelectEx()
	if err != nil {
		slog.Error("indexHandler: SelectEx failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		slog.Error("indexHandler: SelectSet failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	weights, err := dataStore.SelectW()
	if err != nil {
		slog.Error("indexHandler: SelectW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	// Backfill colors for any exercise that lacks one (one-time operation).
	if needsColorBackfill(exs) {
		backfillColors(exs)
		// Reload so the template sees the updated colors.
		if refreshed, err := dataStore.SelectEx(); err == nil {
			exs = refreshed
		}
	}

	sortExsByFrequency(exs, sets, appConfig.FrequencyDays)

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

	sort.Slice(weights, func(i, j int) bool {
		return weights[i].Date < weights[j].Date
	})

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = displaySets
	guiData.ExData.Weight = weights
	guiData.GroupMap = buildGroupList(exs)

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "index.html", guiData)
}

// buildGroupList returns unique group names in first-seen order from exs.
func buildGroupList(exs []models.Exercise) []string {
	seen := make(map[string]bool)
	var groups []string
	for _, ex := range exs {
		if !seen[ex.Group] {
			seen[ex.Group] = true
			groups = append(groups, ex.Group)
		}
	}
	return groups
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
