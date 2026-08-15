package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// ─── routines (workout templates) ──────────────────────────────────────────────

func getRoutines(c *gin.Context) {
	routines, err := dataStore.SelectRoutines(c.Request.Context())
	if err != nil {
		slog.Error("getRoutines: SelectRoutines failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, routines)
}

func postRoutine(c *gin.Context) {
	var body struct {
		Name  string `json:"Name"`
		Notes string `json:"Notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	id, err := dataStore.InsertRoutine(c.Request.Context(), name, strings.TrimSpace(body.Notes))
	if err != nil {
		slog.Error("postRoutine: InsertRoutine failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("routine created", slog.String("name", name), slog.Int("id", id))
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func putRoutine(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var body struct {
		Name  string `json:"Name"`
		Notes string `json:"Notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if err := dataStore.UpdateRoutine(c.Request.Context(), id, name, strings.TrimSpace(body.Notes)); err != nil {
		slog.Error("putRoutine: UpdateRoutine failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func deleteRoutine(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := dataStore.DeleteRoutine(c.Request.Context(), id); err != nil {
		slog.Error("deleteRoutine: DeleteRoutine failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("routine deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

func putRoutineItems(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var body struct {
		Items []models.RoutineItem `json:"Items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := dataStore.ReplaceRoutineItems(c.Request.Context(), id, body.Items); err != nil {
		slog.Error("putRoutineItems: ReplaceRoutineItems failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("routine items replaced", slog.Int("routine_id", id), slog.Int("count", len(body.Items)))
	c.Status(http.StatusOK)
}
