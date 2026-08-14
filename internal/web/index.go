package web

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func indexHandler(c *gin.Context) {
	cfg := conf.Get()

	// The main page only needs sets covering the display window — the picker's
	// recency order comes from LastPerformed (an all-history aggregate), not
	// from this windowed slice, so there is no frequency-lookback to widen for.
	days := cfg.DisplayDays
	if days <= 0 {
		days = 30
	}
	fetchCutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

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

	sortExsByRecency(exs, lastPerformed(c, "indexHandler"))

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

	muscles := selectMusclesSoft(c, "indexHandler")

	var guiData models.GuiData
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	guiData.ExData.Sets = displaySets
	guiData.ExData.Weight = weights
	guiData.GroupMap = mergeGroupList(exs, muscles)
	guiData.Muscles = muscles
	guiData.ServerDate = time.Now().Format("2006-01-02")

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

// mergeGroupList returns the union of exercise groups and catalog groups, in
// first-seen order (exercise groups first). A group defined only in the muscle
// catalog — created in Settings before any exercise uses it — is still offered
// in the picker, which is what lets "select a group, then add an exercise"
// work for a brand-new group.
func mergeGroupList(exs []models.Exercise, muscles []models.Muscle) []string {
	seen := make(map[string]bool)
	var groups []string
	for _, ex := range exs {
		if !seen[ex.Group] {
			seen[ex.Group] = true
			groups = append(groups, ex.Group)
		}
	}
	for _, m := range muscles {
		if !seen[m.Group] {
			seen[m.Group] = true
			groups = append(groups, m.Group)
		}
	}
	return groups
}

// selectMusclesSoft loads the muscle catalog, degrading to nil on error rather
// than failing the page — the catalog only enriches the create form.
func selectMusclesSoft(c *gin.Context, who string) []models.Muscle {
	muscles, err := dataStore.SelectMuscles(c.Request.Context())
	if err != nil {
		slog.Warn(who+": SelectMuscles failed", slog.Any("error", err))
		return nil
	}
	return muscles
}

// lastPerformed fetches the recency map used to order the picker, degrading to
// an empty map (unsorted-by-recency, i.e. id order) rather than 500ing the page
// if the aggregate query fails — ordering is a nicety, not correctness.
func lastPerformed(c *gin.Context, who string) map[string]models.ExerciseRecency {
	rec, err := dataStore.LastPerformed(c.Request.Context())
	if err != nil {
		slog.Warn(who+": LastPerformed failed; picker order degraded", slog.Any("error", err))
		return nil
	}
	return rec
}

// sortExsByRecency orders exs in-place for the picker (Feature 2): exercises
// performed more recently rise to the top, and within a group's most recent
// session they keep the order they were done that day. Exercises never
// performed sink to the bottom, alphabetically. recency is keyed by exercise
// name (see Store.LastPerformed). This replaces the old frequency-count sort
// and the removed per-exercise Place field.
func sortExsByRecency(exs []models.Exercise, recency map[string]models.ExerciseRecency) {
	sort.SliceStable(exs, func(i, j int) bool {
		ri, iok := recency[exs[i].Name]
		rj, jok := recency[exs[j].Name]
		// Performed exercises come before never-performed ones.
		if iok != jok {
			return iok
		}
		if !iok {
			// Neither performed: stable, alphabetical.
			return exs[i].Name < exs[j].Name
		}
		// More recent session first.
		if ri.LastDate != rj.LastDate {
			return ri.LastDate > rj.LastDate
		}
		// Same last session: preserve the order they were performed.
		if ri.Pos != rj.Pos {
			return ri.Pos < rj.Pos
		}
		return exs[i].Name < exs[j].Name
	})
}
