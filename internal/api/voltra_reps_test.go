package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/voltra"
)

// postReps drives the /api/voltra/reps handler with a given config and request
// body, returning the status code and the decoded state (when the handler
// returns one).
func postReps(t *testing.T, cfg models.Conf, body string) (int, voltra.State) {
	t.Helper()
	prev := conf.Get()
	t.Cleanup(func() { conf.Set(prev) })
	conf.Set(cfg)

	r := gin.New()
	r.POST("/api/voltra/reps", postVoltraReps)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/voltra/reps", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var st voltra.State
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	return w.Code, st
}

func TestPostVoltraReps_RefusedWhenAutoLogOff(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "40")
	voltra.Report(true, "") // armed and loaded, so only the toggle is under test

	code, _ := postReps(t, models.Conf{VoltraAutoLog: false}, `{"Reps":10}`)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", code, http.StatusForbidden)
	}
	if voltra.Get().CompletedSeq != 0 {
		t.Fatalf("a refused report recorded a completion")
	}
}

func TestPostVoltraReps_RefusedWhenNotRecording(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "40") // armed, never loaded

	code, _ := postReps(t, models.Conf{VoltraAutoLog: true}, `{"Reps":10}`)
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", code, http.StatusConflict)
	}
	if voltra.Get().CompletedSeq != 0 {
		t.Fatalf("reps recorded against an unloaded set")
	}
}

func TestPostVoltraReps_RecordsAgainstArmedSet(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(42, "40")
	voltra.Report(true, "")

	code, st := postReps(t, models.Conf{VoltraAutoLog: true}, `{"Reps":9}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if st.CompletedSetID != 42 || st.CompletedReps != 9 || st.CompletedSeq != 1 {
		t.Fatalf("state = {setID:%d reps:%d seq:%d}, want {42 9 1}",
			st.CompletedSetID, st.CompletedReps, st.CompletedSeq)
	}
}
