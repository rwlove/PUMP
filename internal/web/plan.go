package web

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// planStore backs the prescription-layer reads the workout page needs
// server-side (today's committed session) and the athlete's same-origin
// feedback form. nil when the wired store does not implement PlanStore, in
// which case the page degrades to freestyle (no prescription header) and the
// feedback handler 503s — mirroring the api package's planStore guard.
var planStore store.PlanStore

// committedSessionToday loads the committed prescribed session for date, or nil
// when the day is freestyle or anything goes wrong — the prescription header is
// an enrichment, never a reason to fail the workout page (same soft-degrade
// posture as routines/groups).
func committedSessionToday(c *gin.Context, who, date string) *models.PrescribedSession {
	if planStore == nil {
		return nil
	}
	ps, ok, err := planStore.SelectCommittedSessionOn(c.Request.Context(), date)
	if err != nil {
		slog.Warn(who+": SelectCommittedSessionOn failed; freestyle fallback",
			slog.String("date", date), slog.Any("error", err))
		return nil
	}
	if !ok {
		return nil
	}
	return &ps
}

// currentQueue builds the training-queue view the workout page presents: the
// cursor session the "Arnold's Routine" button loads, the rotation for the
// picker, and the next-up label. It reads all programs and derives the queue
// from the active one (models.BuildQueuePlan). Returns nil — a freestyle state
// — when there is no queue or anything fails; the queue is an enrichment, never
// a reason to fail the workout page.
func currentQueue(c *gin.Context, who string) *models.QueuePlan {
	if planStore == nil {
		return nil
	}
	programs, err := planStore.SelectPrograms(c.Request.Context())
	if err != nil {
		slog.Warn(who+": SelectPrograms failed; freestyle fallback", slog.Any("error", err))
		return nil
	}
	qp, ok := models.BuildQueuePlan(programs)
	if !ok {
		return nil
	}
	return qp
}

// currentCompletions loads the day→completed-session bindings the workout page
// uses to freeze a completed day on what was performed. Soft-degrades to nil on
// any error (or no plan store) — freezing is an enrichment, never a reason to
// fail the page; without it a completed day just falls back to the next-up
// header, the pre-freeze behavior.
func currentCompletions(c *gin.Context, who string) []models.SessionCompletion {
	if planStore == nil {
		return nil
	}
	cs, err := planStore.SelectSessionCompletions(c.Request.Context())
	if err != nil {
		slog.Warn(who+": SelectSessionCompletions failed; freeze disabled", slog.Any("error", err))
		return nil
	}
	return cs
}

// feedbackHandler persists the athlete's post-session feedback from the
// same-origin workout-page form. Two independent values, each keyed to its own
// date so they land as separate rows (readers merge by date, latest wins):
//   - the session RPE (6-10) for the training day, and
//   - the next-day 0-3 soreness/irritation rating, which — because the flare is
//     delayed ~24h — is rated the day AFTER and keyed to the prior session.
//
// This is the browser/athlete path and is deliberately NOT behind
// PLAN_INGEST_KEY (that gates the coach-authoring API, /api/session-feedback),
// exactly like /set/ and /weight/ are same-origin and unkeyed while their
// /api/* twins are ingest-gated.
func feedbackHandler(c *gin.Context) {
	if planStore == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	wrote := false

	if raw := strings.TrimSpace(c.PostForm("rpe")); raw != "" {
		rpe, err := strconv.ParseFloat(raw, 64)
		if err != nil || rpe < 6 || rpe > 10 {
			c.Status(http.StatusBadRequest)
			return
		}
		date := strings.TrimSpace(c.PostForm("rpe_date"))
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		if err := planStore.InsertSessionFeedback(c.Request.Context(),
			models.SessionFeedback{Date: date, RPE: &rpe, Note: c.PostForm("note")}); err != nil {
			slog.Error("feedbackHandler: InsertSessionFeedback (rpe) failed", slog.Any("error", err))
			c.Status(http.StatusInternalServerError)
			return
		}
		wrote = true
	}

	if raw := strings.TrimSpace(c.PostForm("soreness")); raw != "" {
		flag, err := strconv.Atoi(raw)
		if err != nil || flag < 0 || flag > 3 {
			c.Status(http.StatusBadRequest)
			return
		}
		date := strings.TrimSpace(c.PostForm("soreness_date"))
		if date == "" {
			// Default to yesterday: the next-day rating is about the prior
			// training day, submitted today.
			date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		}
		if err := planStore.InsertSessionFeedback(c.Request.Context(),
			models.SessionFeedback{Date: date, ElbowFlag: &flag}); err != nil {
			slog.Error("feedbackHandler: InsertSessionFeedback (soreness) failed", slog.Any("error", err))
			c.Status(http.StatusInternalServerError)
			return
		}
		wrote = true
	}

	if !wrote {
		// Nothing submitted — treat as a no-op rather than an error so an
		// accidental empty submit just returns to the page.
		slog.Debug("feedbackHandler: empty submit, no-op")
	}

	ref := c.Request.Referer()
	if ref == "" {
		ref = "/"
	}
	c.Redirect(http.StatusFound, ref)
}

// advanceHandler marks the session the athlete just completed as done and
// advances the training queue to the next one. Same-origin and unkeyed (like
// /feedback/ and /set/): this is the browser/athlete path, not the
// coach-authoring API. It expects a session_id form field (the completed
// session) and an optional date (the calendar day being completed, YYYY-MM-DD).
//
// With a date, it records the completion for that day AND advances the cursor
// in one transaction (CompleteSession): the day freezes on what was performed
// and the next-up session surfaces only on later days. Without a date it falls
// back to a bare cursor advance (AdvanceCursor) — the pre-freeze behavior — so
// an older client still works. Returns {"next_position": N}.
func advanceHandler(c *gin.Context) {
	if planStore == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.Atoi(strings.TrimSpace(c.PostForm("session_id")))
	if err != nil || id <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	var next int
	var ok bool
	if date := strings.TrimSpace(c.PostForm("date")); date != "" {
		next, ok, err = planStore.CompleteSession(c.Request.Context(), date, id)
		if err != nil {
			slog.Error("advanceHandler: CompleteSession failed",
				slog.Int("session_id", id), slog.String("date", date), slog.Any("error", err))
			c.Status(http.StatusInternalServerError)
			return
		}
	} else {
		next, ok, err = planStore.AdvanceCursor(c.Request.Context(), id)
		if err != nil {
			slog.Error("advanceHandler: AdvanceCursor failed", slog.Int("session_id", id), slog.Any("error", err))
			c.Status(http.StatusInternalServerError)
			return
		}
	}
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"next_position": next})
}
