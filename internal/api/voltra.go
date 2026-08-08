package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/voltra"
)

// Voltra phase-2 coordination. The browser owns intent (which set is armed,
// whether it should be loaded); the sidecar owns fact (whether the motor is
// actually engaged at that weight). Neither can see the other directly — the
// browser cannot reach the trainer, the sidecar cannot see the DOM — so this
// is the ground they share.

func getVoltraState(c *gin.Context) {
	c.JSON(http.StatusOK, voltra.Get())
}

type armRequest struct {
	SetID    int    `json:"SetID"`
	WeightLb string `json:"WeightLb"`
}

// postVoltraArm points recording at a set. Arming never engages the motor;
// loading is always a separate, explicit action.
func postVoltraArm(c *gin.Context) {
	var req armRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "SetID is required"})
		return
	}
	// Confirm the set exists and belongs to a Voltra-flagged exercise, so a
	// stale UI can't arm a deleted row or point the motor at a barbell lift.
	set, err := dataStore.GetSet(c.Request.Context(), req.SetID)
	if err != nil || set.ID == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such set"})
		return
	}
	flagged, err := exerciseIsVoltra(c, set.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !flagged {
		c.JSON(http.StatusBadRequest,
			gin.H{"error": "exercise is not flagged as using the Voltra trainer"})
		return
	}

	weight := req.WeightLb
	if weight == "" {
		weight = set.Weight.String()
	}
	st := voltra.Arm(req.SetID, weight)
	slog.Info("voltra armed", slog.Int("set_id", req.SetID),
		slog.String("name", set.Name), slog.String("weight_lb", weight))
	c.JSON(http.StatusOK, st)
}

func postVoltraDisarm(c *gin.Context) {
	st := voltra.Disarm()
	slog.Info("voltra disarmed")
	c.JSON(http.StatusOK, st)
}

type loadRequest struct {
	Load bool `json:"Load"`
}

// postVoltraLoad records the athlete pressing LOAD or UNLOAD. It only sets
// intent — Loaded stays false until the sidecar confirms the read-back, so the
// UI cannot show engaged until the hardware says so.
func postVoltraLoad(c *gin.Context) {
	var req loadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Load && voltra.Get().ArmedSetID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing is armed"})
		return
	}
	st := voltra.SetWantLoad(req.Load)
	slog.Info("voltra load requested", slog.Bool("load", req.Load),
		slog.Int("set_id", st.ArmedSetID), slog.String("weight_lb", st.WeightLb))
	c.JSON(http.StatusOK, st)
}

type reportRequest struct {
	Loaded bool   `json:"Loaded"`
	Error  string `json:"Error"`
}

// postVoltraReport is the sidecar telling us what the hardware actually did.
func postVoltraReport(c *gin.Context) {
	var req reportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st := voltra.Report(req.Loaded, req.Error)
	c.JSON(http.StatusOK, st)
}

// getVoltraStream pushes state changes to both the sidecar (so a LOAD press
// reaches it without polling) and the browser (so it reflects hardware truth
// rather than intent).
func getVoltraStream(c *gin.Context) {
	ch, unsub := voltra.Subscribe()
	defer unsub()

	if !sseHeaders(c) {
		return
	}
	// Send current state immediately: a sidecar that reconnects mid-workout
	// must not sit idle waiting for the next change to learn it should be
	// holding the motor.
	select {
	case ch <- voltra.Get():
	default:
	}
	sseLoop(c, ch, func(voltra.State) string { return "state" })
}

// exerciseIsVoltra reports whether the named exercise carries the Voltra flag.
// Matched case-insensitively: sets.name is free text with no foreign key to
// exercises, so spellings drift.
func exerciseIsVoltra(c *gin.Context, name string) (bool, error) {
	exs, err := dataStore.SelectEx(c.Request.Context())
	if err != nil {
		return false, err
	}
	for _, ex := range exs {
		if ex.Voltra && strings.EqualFold(strings.TrimSpace(ex.Name), strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}
