package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rwlove/PUMP/internal/models"
)

// APIClient implements Store by calling the PUMP JSON API.
// It adds an optional API key on every request via the X-Api-Key header.
type APIClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAPIClient constructs a client pointed at baseURL (e.g. "http://localhost:8851").
// apiKey may be empty when the API server runs without key protection.
func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (a *APIClient) do(method, path string, body interface{}) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("X-Api-Key", a.apiKey)
	}
	return a.client.Do(req)
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("API %s: %s", resp.Status, string(body))
	}
	return nil
}

func decodeJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// ─── Store interface ──────────────────────────────────────────────────────────

func (a *APIClient) SelectEx() ([]models.Exercise, error) {
	resp, err := a.do("GET", "/api/exercises", nil)
	if err != nil {
		return nil, err
	}
	var out []models.Exercise
	return out, decodeJSON(resp, &out)
}

func (a *APIClient) InsertEx(ex models.Exercise) error {
	var method, path string
	if ex.ID != 0 {
		method = "PUT"
		path = fmt.Sprintf("/api/exercises/%d", ex.ID)
	} else {
		method = "POST"
		path = "/api/exercises"
	}
	resp, err := a.do(method, path, ex)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

func (a *APIClient) DeleteEx(id int) error {
	resp, err := a.do("DELETE", fmt.Sprintf("/api/exercises/%d", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

func (a *APIClient) UpdateExColor(id int, color string) error {
	resp, err := a.do("PATCH", fmt.Sprintf("/api/exercises/%d/color", id),
		map[string]string{"color": color})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

func (a *APIClient) SelectSet() ([]models.Set, error) {
	resp, err := a.do("GET", "/api/sets", nil)
	if err != nil {
		return nil, err
	}
	var out []models.Set
	return out, decodeJSON(resp, &out)
}

func (a *APIClient) BulkReplaceSetsByDate(date string, sets []models.Set) error {
	resp, err := a.do("PUT", "/api/sets/date/"+date, sets)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

func (a *APIClient) SelectW() ([]models.BodyWeight, error) {
	resp, err := a.do("GET", "/api/weight", nil)
	if err != nil {
		return nil, err
	}
	var out []models.BodyWeight
	return out, decodeJSON(resp, &out)
}

func (a *APIClient) InsertW(w models.BodyWeight) error {
	resp, err := a.do("POST", "/api/weight", w)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

func (a *APIClient) DeleteW(id int) error {
	resp, err := a.do("DELETE", fmt.Sprintf("/api/weight/%d", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

// ─── Config (not part of Store, but needed by the frontend) ──────────────────

// GetConfig fetches the current configuration from the API.
func (a *APIClient) GetConfig() (models.Conf, error) {
	resp, err := a.do("GET", "/api/config", nil)
	if err != nil {
		return models.Conf{}, err
	}
	var out models.Conf
	return out, decodeJSON(resp, &out)
}

// SaveConfig persists non-auth configuration fields via the API.
func (a *APIClient) SaveConfig(cfg models.Conf) error {
	resp, err := a.do("PUT", "/api/config", cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

