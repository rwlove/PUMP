package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// fakeStore records SaveAppConfig calls and satisfies store.Store just
// enough for saveConfigHandler.
type fakeStore struct {
	mu    sync.Mutex
	saved []models.Conf
	fail  error
}

func (f *fakeStore) SaveAppConfig(ctx context.Context, cfg models.Conf) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.saved = append(f.saved, cfg)
	return nil
}
func (f *fakeStore) GetAppConfig(ctx context.Context) (models.Conf, bool, error) {
	return models.Conf{}, false, nil
}

// Unused-by-this-test Store methods — return zero values so the interface
// is satisfied without pulling in a real DB.
func (f *fakeStore) SelectEx(context.Context) ([]models.Exercise, error) { return nil, nil }
func (f *fakeStore) InsertEx(context.Context, models.Exercise) error     { return nil }
func (f *fakeStore) UpdateEx(context.Context, models.Exercise) (bool, error) {
	return false, nil
}
func (f *fakeStore) DeleteEx(context.Context, int) error                               { return nil }
func (f *fakeStore) UpdateExColor(context.Context, int, string) error                  { return nil }
func (f *fakeStore) SelectSet(context.Context) ([]models.Set, error)                   { return nil, nil }
func (f *fakeStore) SelectSetsSince(context.Context, string) ([]models.Set, error)     { return nil, nil }
func (f *fakeStore) BulkReplaceSetsByDate(context.Context, string, []models.Set) error { return nil }
func (f *fakeStore) GetSet(context.Context, int) (models.Set, error)                   { return models.Set{}, nil }
func (f *fakeStore) InsertSet(context.Context, models.Set) (models.Set, error) {
	return models.Set{}, nil
}
func (f *fakeStore) UpdateSet(context.Context, int, models.SetUpdate) (models.Set, error) {
	return models.Set{}, nil
}
func (f *fakeStore) DeleteSet(context.Context, int) (string, error) { return "", nil }
func (f *fakeStore) SelectW(context.Context) ([]models.BodyWeight, error) {
	return nil, nil
}
func (f *fakeStore) InsertW(context.Context, models.BodyWeight) error { return nil }
func (f *fakeStore) DeleteW(context.Context, int) error               { return nil }

var _ store.Store = (*fakeStore)(nil)

// swap in/out dataStore + a known conf so tests are hermetic.
func withFakes(t *testing.T, s store.Store, base models.Conf) func() {
	t.Helper()
	origDS := dataStore
	dataStore = s
	conf.Set(base)
	return func() { dataStore = origDS; conf.Set(models.Conf{}) }
}

func TestSaveConfigHandler_PersistsToStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fs := &fakeStore{}
	defer withFakes(t, fs, models.Conf{Color: "dark", PageStep: 10})()

	form := url.Values{
		"color":         {"light"},
		"pagestep":      {"25"},
		"frequencydays": {"7"},
		"displaydays":   {"90"},
		"autofill":      {"on"},
		"cvautolog":     {"on"},
	}
	req := httptest.NewRequest("POST", "/config/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r := gin.New()
	r.POST("/config/", saveConfigHandler)
	r.ServeHTTP(w, req)

	if w.Code != 302 {
		t.Fatalf("status=%d, want 302 (redirect)", w.Code)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.saved) != 1 {
		t.Fatalf("expected 1 SaveAppConfig call, got %d", len(fs.saved))
	}
	saved := fs.saved[0]
	if saved.Color != "light" {
		t.Errorf("Color=%q, want light", saved.Color)
	}
	if saved.PageStep != 25 {
		t.Errorf("PageStep=%d, want 25", saved.PageStep)
	}
	if saved.FrequencyDays != 7 {
		t.Errorf("FrequencyDays=%d, want 7", saved.FrequencyDays)
	}
	if saved.DisplayDays != 90 {
		t.Errorf("DisplayDays=%d, want 90", saved.DisplayDays)
	}
	if !saved.AutoFill {
		t.Errorf("AutoFill=false, want true")
	}
	if !saved.CVAutoLog {
		t.Errorf("CVAutoLog=false, want true")
	}
	// And conf.Get() must reflect the change immediately (in-memory
	// mutation is the SoT for the current process — persistence is for
	// the next boot).
	got := conf.Get()
	if got.Color != "light" || !got.CVAutoLog || got.PageStep != 25 {
		t.Errorf("conf.Get() didn't reflect the save: %+v", got)
	}
}

func TestSaveConfigHandler_StoreFailureIsSoftFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fs := &fakeStore{fail: errFakeSave}
	defer withFakes(t, fs, models.Conf{Color: "dark"})()

	form := url.Values{"color": {"light"}, "pagestep": {"10"}, "frequencydays": {"30"},
		"displaydays": {"30"}, "autofill": {"on"}, "cvautolog": {""}}
	req := httptest.NewRequest("POST", "/config/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r := gin.New()
	r.POST("/config/", saveConfigHandler)
	r.ServeHTTP(w, req)

	// User still gets a successful redirect; persistence failure is a
	// server-side log line, not a UI error.
	if w.Code != 302 {
		t.Fatalf("status=%d, want 302 (redirect) even when persistence fails", w.Code)
	}
	// In-memory config is still updated — the current process reflects
	// the change; only the "survives a restart" bit is lost.
	if conf.Get().Color != "light" {
		t.Errorf("in-memory conf should reflect save even on persist fail")
	}
}

// errFakeSave is a sentinel error the fakeStore returns; using a package
// var avoids importing errors just for a single sentinel.
var errFakeSave = &fakeSaveError{}

type fakeSaveError struct{}

func (*fakeSaveError) Error() string { return "fake save error" }

// Sanity check: shopspring/decimal isn't inadvertently unused (this test
// file imports it via models.Conf but only exercises it implicitly).
var _ = decimal.Decimal{}
var _ = time.Second
