// Package notify sends user-facing notifications. Currently Pushover only.
//
// All sends are best-effort: failures log a warning and never propagate to
// the request that triggered them. Network and credential problems are
// the user's signal that something needs fixing on the Pushover side, not
// errors PUMP needs to halt for.
package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAPIURL = "https://api.pushover.net/1/messages.json"

// Pushover is a minimal client for the Pushover messages API.
//
// A nil receiver is valid and silently no-ops on every method, so callers
// can hold a single field that is either configured or not without nil-
// checking at every call site.
type Pushover struct {
	UserKey  string
	AppToken string
	APIURL   string // override for testing; empty = the public Pushover endpoint
	Client   *http.Client
}

// Configured reports whether both credentials are present.
func (p *Pushover) Configured() bool {
	return p != nil && p.UserKey != "" && p.AppToken != ""
}

// Message is the payload accepted by Send / SendAsync.
type Message struct {
	Title    string
	Body     string
	URL      string
	URLTitle string
	Priority int // Pushover priority: -2..2 (0 = default)
}

// Send pushes a single message synchronously.
func (p *Pushover) Send(ctx context.Context, m Message) error {
	if !p.Configured() {
		return nil
	}

	form := url.Values{}
	form.Set("token", p.AppToken)
	form.Set("user", p.UserKey)
	form.Set("message", m.Body)
	if m.Title != "" {
		form.Set("title", m.Title)
	}
	if m.URL != "" {
		form.Set("url", m.URL)
	}
	if m.URLTitle != "" {
		form.Set("url_title", m.URLTitle)
	}
	if m.Priority != 0 {
		form.Set("priority", fmt.Sprintf("%d", m.Priority))
	}

	endpoint := p.APIURL
	if endpoint == "" {
		endpoint = defaultAPIURL
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pushover: status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// SendAsync fires Send in a goroutine and logs the outcome. Used from
// request handlers so they don't block on the Pushover round-trip.
func (p *Pushover) SendAsync(m Message) {
	if !p.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Send(ctx, m); err != nil {
			slog.Warn("pushover send failed",
				slog.String("title", m.Title),
				slog.Any("error", err))
			return
		}
		slog.Debug("pushover sent", slog.String("title", m.Title))
	}()
}
