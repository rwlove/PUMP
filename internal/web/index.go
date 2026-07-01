package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func indexHandler(c *gin.Context) {
	cfg := conf.Get()

	// The main page only needs sets covering the display window and the
	// frequency-sort lookback — fetch just that span instead of all history.
	days := cfg.DisplayDays
	if days <= 0 {
		days = 30
	}
	window := days
	if cfg.FrequencyDays > window {
		window = cfg.FrequencyDays
	}
	fetchCutoff := time.Now().AddDate(0, 0, -window).Format("2006-01-02")

	exs, ok := selectExOr500(c, "indexHandler")
	if !ok {
		return
	}
	sets, ok := selectSetsSinceOr500(c, "indexHandler", fetchCutoff)
	if !ok {
		return
	}
	weights, ok := selectWeightsOr500(c, "indexHandler")
	if !ok {
		return
	}

	// Backfill colors for any exercise that lacks one (one-time operation).
	if needsColorBackfill(exs) {
		backfillColors(c.Request.Context(), exs)
		// Reload so the template sees the updated colors.
		if refreshed, err := dataStore.SelectEx(c.Request.Context()); err == nil {
			exs = refreshed
		}
	}

	sortExsByFrequency(exs, sets, cfg.FrequencyDays)

	// Limit sets sent to the main page to the configured display window.
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
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = displaySets
	guiData.ExData.Weight = weights
	guiData.GroupMap = buildGroupList(exs)
	guiData.ServerDate = time.Now().Format("2006-01-02")

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
