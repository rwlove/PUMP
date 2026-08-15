package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ─── groups (managed training groups) ──────────────────────────────────────────

func getGroups(c *gin.Context) {
	groups, err := dataStore.SelectGroups(c.Request.Context())
	if err != nil {
		slog.Error("getGroups: SelectGroups failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, groups)
}

func postGroup(c *gin.Context) {
	var body struct {
		Name string `json:"Name"`
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
	if err := dataStore.InsertGroup(c.Request.Context(), name); err != nil {
		slog.Error("postGroup: InsertGroup failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("group added", slog.String("name", name))
	c.Status(http.StatusCreated)
}

func putGroup(c *gin.Context) {
	var body struct {
		OldName string `json:"OldName"`
		NewName string `json:"NewName"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	old := strings.TrimSpace(body.OldName)
	nw := strings.TrimSpace(body.NewName)
	if old == "" || nw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OldName and NewName are required"})
		return
	}
	if err := dataStore.RenameGroup(c.Request.Context(), old, nw); err != nil {
		// A rename onto an existing group name violates the primary key; report
		// it as a conflict rather than a 500 so the UI can explain.
		slog.Warn("putGroup: RenameGroup failed", slog.String("old", old), slog.String("new", nw), slog.Any("error", err))
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	slog.Info("group renamed", slog.String("old", old), slog.String("new", nw))
	c.Status(http.StatusOK)
}

func deleteGroup(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	inUse, err := dataStore.DeleteGroup(c.Request.Context(), name)
	if err != nil {
		slog.Error("deleteGroup: DeleteGroup failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inUse > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "group is still in use", "inUse": inUse})
		return
	}
	slog.Info("group deleted", slog.String("name", name))
	c.Status(http.StatusNoContent)
}

func postGroupsReorder(c *gin.Context) {
	var body struct {
		Names []string `json:"Names"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := dataStore.ReorderGroups(c.Request.Context(), body.Names); err != nil {
		slog.Error("postGroupsReorder: ReorderGroups failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("groups reordered", slog.Int("count", len(body.Names)))
	c.Status(http.StatusOK)
}
