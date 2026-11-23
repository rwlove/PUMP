package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// A rejected manual weigh-in must explain itself. The form is a plain HTML
// POST, so a bodyless 4xx renders as a blank page on a phone and the user has
// no idea nothing was saved. These paths return before touching the store.
func TestAddWeightHandler_RejectionIsReadable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, weight string
		status       int
		want         string
	}{
		{"blank", "", http.StatusUnprocessableEntity, "The weight field was empty."},
		{"implausible", "5", http.StatusUnprocessableEntity, "5 lb is outside the accepted range."},
		{"garbage", "abc<b>", http.StatusBadRequest, "is not a number."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/weight/", addWeightHandler)
			form := url.Values{"date": {"2026-11-23"}, "weight": {tc.weight}}
			req := httptest.NewRequest(http.MethodPost, "/weight/", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Referer", "/stats/")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			body := w.Body.String()
			if !strings.Contains(body, "Weight not saved") || !strings.Contains(body, tc.want) {
				t.Errorf("body missing explanation %q:\n%s", tc.want, body)
			}
			if !strings.Contains(body, `href="/stats/"`) {
				t.Errorf("body missing back link to referer:\n%s", body)
			}
			if strings.Contains(body, "<b>") {
				t.Errorf("user input not escaped:\n%s", body)
			}
		})
	}
}
