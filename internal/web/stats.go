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
	// Unconfirmed (pending) sets — a CV detection still awaiting the athlete's
	// ✓ — are not yet real performed data, so they must not inflate volume, PRs,
	// muscle balance, or any other stat. They reappear the moment they're
	// confirmed. Manual prefill rows never reach here: they aren't written until
	// the user commits them, so filtering on pending is the only case left.
	confirmed := make([]models.Set, 0, len(sets))
	for _, s := range sets {
		if !s.Pending {
			confirmed = append(confirmed, s)
		}
	}
	sets = confirmed
	weights, ok := selectWeightsOr500(c, "statsHandler")
	if !ok {
		return
	}

	sort.Slice(sets, func(i, j int) bool { return sets[i].Date < sets[j].Date })
	sort.Slice(weights, func(i, j int) bool { return weights[i].Date < weights[j].Date })
	cfg := conf.Get()
	sortExsByRecency(exs, lastPerformed(c, "statsHandler"))

	var guiData models.GuiData
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = sets
	guiData.ExData.Weight = weights
	guiData.ServerDate = time.Now().Format("2006-01-02")
	guiData.Health = loadHealthStats(c.Request.Context())
	// Focus muscles per exercise, for the muscle-level Muscle Balance view. Soft
	// failure (nil map) — the tab falls back to the by-group breakdown.
	guiData.ExerciseMuscles, _ = dataStore.SelectAllExerciseMuscles(c.Request.Context())
	// Grip / dead-hang readings for the Grip & Hang tab (soft failure → nil).
	guiData.Measurements, _ = dataStore.SelectMeasurements(c.Request.Context())

	c.HTML(http.StatusOK, "stats.html", guiData)
}
