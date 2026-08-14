package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// ─── muscle catalog (DB-editable focus muscles per group) ──────────────────────

func getMuscles(c *gin.Context) {
	muscles, err := dataStore.SelectMuscles(c.Request.Context())
	if err != nil {
		slog.Error("getMuscles: SelectMuscles failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, muscles)
}

func postMuscle(c *gin.Context) {
	var m models.Muscle
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m.Group = strings.TrimSpace(m.Group)
	m.Name = strings.TrimSpace(m.Name)
	if m.Group == "" || m.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group and name are required"})
		return
	}
	if err := dataStore.InsertMuscle(c.Request.Context(), m); err != nil {
		slog.Error("postMuscle: InsertMuscle failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("muscle added", slog.String("group", m.Group), slog.String("name", m.Name))
	c.Status(http.StatusCreated)
}

func deleteMuscle(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := dataStore.DeleteMuscle(c.Request.Context(), id); err != nil {
		slog.Error("deleteMuscle: DeleteMuscle failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("muscle deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

// ─── set reorder (drag-and-drop) ───────────────────────────────────────────────

// postSetsReorder rewrites the per-day position of the given set ids to match
// the submitted order (index 0 = top of the log). Used by the workout log's
// drag-and-drop in per-set save mode; the bulk save path assigns position from
// DOM order instead. A bulk SSE event tells other viewers (e.g. the wall) to
// re-render the day in the new order.
func postSetsReorder(c *gin.Context) {
	var body struct {
		Date string `json:"date"`
		IDs  []int  `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date is required"})
		return
	}
	if err := dataStore.ReorderSets(c.Request.Context(), body.Date, body.IDs); err != nil {
		slog.Error("postSetsReorder: ReorderSets failed",
			slog.String("date", body.Date), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("sets reordered", slog.String("date", body.Date), slog.Int("count", len(body.IDs)))
	publishSetEvent(SetEvent{Type: SetEventBulk, Date: body.Date}, clientID(c))
	c.Status(http.StatusOK)
}
