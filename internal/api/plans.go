package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// ─── prescription layer (programs / prescribed sessions / feedback) ─────────────
//
// Reads are open (the UI presents the plan). Writes are authored by the coach
// agent, not the browser, so they sit behind PLAN_INGEST_KEY — the same ingest
// posture as weight/health (empty key = open, for the in-cluster path).

// planStore is the prescription-layer backend. nil when RegisterRoutes was
// given a store that does not implement PlanStore (handlers 503 rather than
// panic), mirroring healthStore.
var planStore store.PlanStore

// planIngestKey, when non-empty, requires plan write endpoints to present a
// matching X-Api-Key header.
var planIngestKey string

// planReady guards every plan handler against a nil planStore.
func planReady(c *gin.Context) bool {
	if planStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "prescription layer unavailable"})
		return false
	}
	return true
}

func getPrograms(c *gin.Context) {
	if !planReady(c) {
		return
	}
	programs, err := planStore.SelectPrograms(c.Request.Context())
	if err != nil {
		slog.Error("getPrograms: SelectPrograms failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, programs)
}

func postProgram(c *gin.Context) {
	if !planReady(c) {
		return
	}
	var p models.Program
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(p.StartDate) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "StartDate is required (YYYY-MM-DD)"})
		return
	}
	if p.LengthWeeks <= 0 {
		p.LengthWeeks = 4
	}
	id, err := planStore.InsertProgram(c.Request.Context(), p)
	if err != nil {
		slog.Error("postProgram: InsertProgram failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("program created", slog.Int("id", id), slog.String("name", p.Name), slog.String("by", p.CreatedBy))
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func deleteProgram(c *gin.Context) {
	if !planReady(c) {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := planStore.DeleteProgram(c.Request.Context(), id); err != nil {
		slog.Error("deleteProgram: DeleteProgram failed", slog.Int("id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("program deleted", slog.Int("id", id))
	c.Status(http.StatusNoContent)
}

func postPrescribedSession(c *gin.Context) {
	if !planReady(c) {
		return
	}
	programID, ok := idParam(c)
	if !ok {
		return
	}
	var ps models.PrescribedSession
	if err := c.ShouldBindJSON(&ps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ps.ProgramID = programID
	if strings.TrimSpace(ps.Date) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required (YYYY-MM-DD)"})
		return
	}
	if ps.Status != "" && !validSessionStatus(ps.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status must be 'provisional' or 'committed'"})
		return
	}
	id, err := planStore.InsertPrescribedSession(c.Request.Context(), ps)
	if err != nil {
		slog.Error("postPrescribedSession: InsertPrescribedSession failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("prescribed session created", slog.Int("id", id), slog.Int("program_id", programID))
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func putPrescribedSessionStatus(c *gin.Context) {
	if !planReady(c) {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"Status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validSessionStatus(body.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status must be 'provisional' or 'committed'"})
		return
	}
	found, err := planStore.UpdatePrescribedSessionStatus(c.Request.Context(), id, body.Status)
	if err != nil {
		slog.Error("putPrescribedSessionStatus: update failed", slog.Int("id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such prescribed session"})
		return
	}
	slog.Info("prescribed session status set", slog.Int("id", id), slog.String("status", body.Status))
	c.Status(http.StatusOK)
}

func putPrescribedItems(c *gin.Context) {
	if !planReady(c) {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var items []models.PrescribedItem
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := planStore.ReplacePrescribedItems(c.Request.Context(), id, items); err != nil {
		slog.Error("putPrescribedItems: ReplacePrescribedItems failed", slog.Int("session_id", id), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("prescribed items replaced", slog.Int("session_id", id), slog.Int("count", len(items)))
	c.Status(http.StatusOK)
}

func getSessionFeedback(c *gin.Context) {
	if !planReady(c) {
		return
	}
	fb, err := planStore.SelectSessionFeedback(c.Request.Context())
	if err != nil {
		slog.Error("getSessionFeedback: SelectSessionFeedback failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Optional ?since=YYYY-MM-DD scopes the response for the weekly check-in
	// reader, which only wants the current window. Dates are stored as
	// YYYY-MM-DD so a lexical >= is a correct date comparison.
	if since := c.Query("since"); since != "" {
		if _, err := time.Parse("2006-01-02", since); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "since must be YYYY-MM-DD"})
			return
		}
		out := make([]models.SessionFeedback, 0, len(fb))
		for _, f := range fb {
			if f.Date >= since {
				out = append(out, f)
			}
		}
		fb = out
	}
	c.JSON(http.StatusOK, fb)
}

// getPrescription returns the committed prescribed session for a date
// (?date=YYYY-MM-DD, default today) with its items, or 204 when that day is
// freestyle. Read is open — the workout UI and the weekly check-in both read
// it; authoring stays behind PLAN_INGEST_KEY.
func getPrescription(c *gin.Context) {
	if !planReady(c) {
		return
	}
	date := c.Query("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be YYYY-MM-DD"})
		return
	}
	ps, ok, err := planStore.SelectCommittedSessionOn(c.Request.Context(), date)
	if err != nil {
		slog.Error("getPrescription: SelectCommittedSessionOn failed",
			slog.String("date", date), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, ps)
}

func postSessionFeedback(c *gin.Context) {
	if !planReady(c) {
		return
	}
	var f models.SessionFeedback
	if err := c.ShouldBindJSON(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(f.Date) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required (YYYY-MM-DD)"})
		return
	}
	if f.RPE != nil && (*f.RPE < 6 || *f.RPE > 10) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "RPE must be between 6 and 10"})
		return
	}
	if f.ElbowFlag != nil && (*f.ElbowFlag < 0 || *f.ElbowFlag > 3) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ElbowFlag must be between 0 and 3"})
		return
	}
	if err := planStore.InsertSessionFeedback(c.Request.Context(), f); err != nil {
		slog.Error("postSessionFeedback: InsertSessionFeedback failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("session feedback logged", slog.String("date", f.Date))
	c.Status(http.StatusCreated)
}

// validSessionStatus mirrors the prescribed_sessions.status CHECK constraint so
// the API rejects a bad value with 400 rather than surfacing a DB error.
func validSessionStatus(s string) bool {
	return s == "provisional" || s == "committed"
}
