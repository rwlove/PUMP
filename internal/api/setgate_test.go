package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
	"github.com/rwlove/PUMP/internal/voltra"
)

// gateStore records InsertSet calls so a test can tell "refused" from
// "accepted and written" — the distinction the source gate exists to make.
type gateStore struct {
	inserted []models.Set
}

func (g *gateStore) InsertSet(_ context.Context, set models.Set) (models.Set, error) {
	g.inserted = append(g.inserted, set)
	set.ID = len(g.inserted)
	return set, nil
}

// Remaining Store methods are unused here; zero values satisfy the interface.
func (g *gateStore) SelectEx(context.Context) ([]models.Exercise, error)    { return nil, nil }
func (g *gateStore) InsertEx(context.Context, models.Exercise) (int, error) { return 0, nil }
func (g *gateStore) UpdateEx(context.Context, models.Exercise) (bool, error) {
	return false, nil
}
func (g *gateStore) DeleteEx(context.Context, int) error                               { return nil }
func (g *gateStore) UpdateExColor(context.Context, int, string) error                  { return nil }
func (g *gateStore) SelectSet(context.Context) ([]models.Set, error)                   { return nil, nil }
func (g *gateStore) SelectSetsSince(context.Context, string) ([]models.Set, error)     { return nil, nil }
func (g *gateStore) BulkReplaceSetsByDate(context.Context, string, []models.Set) error { return nil }
func (g *gateStore) GetSet(context.Context, int) (models.Set, error)                   { return models.Set{}, nil }
func (g *gateStore) UpdateSet(context.Context, int, models.SetUpdate) (models.Set, error) {
	return models.Set{}, nil
}
func (g *gateStore) DeleteSet(context.Context, int) (string, error)       { return "", nil }
func (g *gateStore) SelectW(context.Context) ([]models.BodyWeight, error) { return nil, nil }
func (g *gateStore) InsertW(context.Context, models.BodyWeight) error     { return nil }
func (g *gateStore) DeleteW(context.Context, int) error                   { return nil }
func (g *gateStore) GetAppConfig(context.Context) (models.Conf, bool, error) {
	return models.Conf{}, false, nil
}
func (g *gateStore) SaveAppConfig(context.Context, models.Conf) error { return nil }
func (g *gateStore) ReorderSets(context.Context, string, []int) error { return nil }
func (g *gateStore) LastPerformed(context.Context) (map[string]models.ExerciseRecency, error) {
	return nil, nil
}
func (g *gateStore) SelectMuscles(context.Context) ([]models.Muscle, error) { return nil, nil }
func (g *gateStore) InsertMuscle(context.Context, models.Muscle) error      { return nil }
func (g *gateStore) DeleteMuscle(context.Context, int) error                { return nil }
func (g *gateStore) SelectGroups(context.Context) ([]models.Group, error)   { return nil, nil }
func (g *gateStore) InsertGroup(context.Context, string) error              { return nil }
func (g *gateStore) RenameGroup(context.Context, string, string) error      { return nil }
func (g *gateStore) DeleteGroup(context.Context, string) (int, error)       { return 0, nil }
func (g *gateStore) ReorderGroups(context.Context, []string) error          { return nil }
func (g *gateStore) SelectExerciseMuscles(context.Context, int) ([]models.FocusMuscle, error) {
	return nil, nil
}
func (g *gateStore) ReplaceExerciseMuscles(context.Context, int, []models.FocusMuscle) error {
	return nil
}
func (g *gateStore) SelectAllExerciseMuscles(context.Context) (map[int][]models.FocusMuscle, error) {
	return nil, nil
}
func (g *gateStore) SelectMeasurements(context.Context) ([]models.Measurement, error) {
	return nil, nil
}
func (g *gateStore) InsertMeasurement(context.Context, models.Measurement) error { return nil }
func (g *gateStore) DeleteMeasurement(context.Context, int) error                { return nil }
func (g *gateStore) SelectRoutines(context.Context) ([]models.Routine, error)    { return nil, nil }
func (g *gateStore) InsertRoutine(context.Context, string, string) (int, error)  { return 0, nil }
func (g *gateStore) UpdateRoutine(context.Context, int, string, string) error    { return nil }
func (g *gateStore) DeleteRoutine(context.Context, int) error                    { return nil }
func (g *gateStore) ReplaceRoutineItems(context.Context, int, []models.RoutineItem) error {
	return nil
}

var _ store.Store = (*gateStore)(nil)

// postSetWithConf runs postSet once against a fresh store under the given
// config, returning the status code and the store.
func postSetWithConf(t *testing.T, cfg models.Conf, source string) (int, *gateStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevStore := dataStore
	prevConf := conf.Get()
	t.Cleanup(func() {
		dataStore = prevStore
		conf.Set(prevConf)
	})

	gs := &gateStore{}
	dataStore = gs
	conf.Set(cfg)

	body, err := json.Marshal(models.Set{
		Date: "2026-08-03", Name: "Cable Row", Reps: 10, Source: source,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	r := gin.New()
	r.POST("/api/sets", postSet)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sets", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w.Code, gs
}

func TestPostSet_VoltraRefusedWhenAutoLogOff(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "50")
	voltra.Report(true, "") // armed and loaded, so only the toggle is under test

	code, gs := postSetWithConf(t, models.Conf{VoltraAutoLog: false}, "voltra")
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", code, http.StatusForbidden)
	}
	if len(gs.inserted) != 0 {
		t.Fatalf("wrote %d sets; a refused write must not reach the store", len(gs.inserted))
	}
}

func TestPostSet_VoltraAcceptedWhenArmedAndLoaded(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "50")
	voltra.Report(true, "")

	code, gs := postSetWithConf(t, models.Conf{VoltraAutoLog: true}, "voltra")
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", code, http.StatusCreated)
	}
	if len(gs.inserted) != 1 {
		t.Fatalf("wrote %d sets, want 1", len(gs.inserted))
	}
}

// The whole point of the armed+loaded gate: a set must not be recorded unless
// the motor was actually engaged at PUMP's weight. Otherwise PUMP's number is
// attributed to reps performed at whatever load the trainer was holding.
func TestPostSet_VoltraRefusedWhenArmedButNotLoaded(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "50") // armed, never loaded

	code, gs := postSetWithConf(t, models.Conf{VoltraAutoLog: true}, "voltra")
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", code, http.StatusConflict)
	}
	if len(gs.inserted) != 0 {
		t.Fatalf("wrote %d sets; an unloaded set must not be recorded", len(gs.inserted))
	}
}

func TestPostSet_VoltraRefusedWhenNothingArmed(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Report(true, "") // loaded, but nothing armed

	code, gs := postSetWithConf(t, models.Conf{VoltraAutoLog: true}, "voltra")
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", code, http.StatusConflict)
	}
	if len(gs.inserted) != 0 {
		t.Fatalf("wrote %d sets with nothing armed", len(gs.inserted))
	}
}

// The gate is Voltra-only. Manual entry and CV must not be affected by whether
// a cable machine happens to be engaged.
func TestPostSet_ArmedStateDoesNotGateOtherSources(t *testing.T) {
	t.Cleanup(voltra.Reset)
	// Nothing armed, nothing loaded.
	for _, src := range []string{"", "manual"} {
		code, gs := postSetWithConf(t, models.Conf{}, src)
		if code != http.StatusCreated || len(gs.inserted) != 1 {
			t.Errorf("source %q: status=%d inserted=%d, want 201/1", src, code, len(gs.inserted))
		}
	}
	code, _ := postSetWithConf(t, models.Conf{CVAutoLog: true}, "cv")
	if code != http.StatusCreated {
		t.Errorf("cv write blocked by voltra armed state: status=%d", code)
	}
}

// The two gates are independent: turning CV on must not open the Voltra
// path, and vice versa. A single shared toggle would pass the tests above
// but fail these.
func TestPostSet_GatesAreIndependent(t *testing.T) {
	t.Cleanup(voltra.Reset)
	voltra.Arm(1, "50")
	voltra.Report(true, "")

	if code, _ := postSetWithConf(t, models.Conf{CVAutoLog: true}, "voltra"); code != http.StatusForbidden {
		t.Errorf("voltra write under CVAutoLog-only: status = %d, want %d", code, http.StatusForbidden)
	}
	if code, _ := postSetWithConf(t, models.Conf{VoltraAutoLog: true}, "cv"); code != http.StatusForbidden {
		t.Errorf("cv write under VoltraAutoLog-only: status = %d, want %d", code, http.StatusForbidden)
	}
}

// Manual entry must never be gated — the toggles govern sidecars only.
func TestPostSet_ManualAlwaysAccepted(t *testing.T) {
	for _, source := range []string{"", "manual"} {
		code, gs := postSetWithConf(t, models.Conf{}, source)
		if code != http.StatusCreated {
			t.Errorf("source %q: status = %d, want %d", source, code, http.StatusCreated)
		}
		if len(gs.inserted) != 1 {
			t.Errorf("source %q: wrote %d sets, want 1", source, len(gs.inserted))
		}
	}
}

// AutoLog drives the workout page's save mode. If it ever returns false while
// a sidecar can write, the page falls back to the bulk /set/ path and deletes
// sidecar-written sets on the next autosave.
func TestConfAutoLog(t *testing.T) {
	cases := []struct {
		cv, voltra, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, c := range cases {
		got := models.Conf{CVAutoLog: c.cv, VoltraAutoLog: c.voltra}.AutoLog()
		if got != c.want {
			t.Errorf("AutoLog(cv=%v, voltra=%v) = %v, want %v", c.cv, c.voltra, got, c.want)
		}
	}
}
