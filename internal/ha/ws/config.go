package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
)

// ErrNotAdmin: Home Assistant refused a command or a request because Mira's
// token is not an admin's: an install that still has the token of the old
// user "Mira" (docs/spec/hacs-default.md, 3). A websocket refusal with the
// code "unauthorized" is one too.
var ErrNotAdmin = errors.New("home assistant needs an admin's token for this")

// SaveAutomationConfig writes one automation through Home Assistant's
// automation config API, the one its editor uses: Home Assistant writes
// automations.yaml and answers at once; it loads the automation right after
// (docs/spec/hacs-default.md, check C1).
func (c *Client) SaveAutomationConfig(ctx context.Context, id string, config map[string]any) error {
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = c.configRequest(ctx, http.MethodPost, id, body)
	return err
}

// DeleteAutomationConfig removes one automation through the same API; Home
// Assistant removes its registry entry right after (check C2).
func (c *Client) DeleteAutomationConfig(ctx context.Context, id string) error {
	_, err := c.configRequest(ctx, http.MethodDelete, id, nil)
	return err
}

// ErrNoAutomation: Home Assistant has no automation of that id (its config
// API answered 404).
var ErrNoAutomation = errors.New("home assistant has no automation of that id")

// AutomationConfig reads one automation as Home Assistant's config API
// holds it ({id, alias, description, use_blueprint|triggers…});
// ErrNoAutomation when there is none (docs/spec/skill-files.md, checked
// row 2).
func (c *Client) AutomationConfig(ctx context.Context, id string) (map[string]any, error) {
	raw, err := c.configRequest(ctx, http.MethodGet, id, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("home assistant's automation config of %s answered %q: %w", id, excerpt.Bytes(raw), err)
	}
	return out, nil
}

// ConfigError is a refusal of the automation config API, by its status: a
// 429 and a 5xx may pass, any other refusal is about the request.
type ConfigError struct {
	Method, ID, Status, Answer string
	Code                       int
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("home assistant refused %s of automation %s: %s %s", e.Method, e.ID, e.Status, e.Answer)
}

func (e *ConfigError) Retryable() bool {
	return e.Code == http.StatusTooManyRequests || e.Code >= 500
}

func (c *Client) configRequest(ctx context.Context, method, id string, body []byte) ([]byte, error) {
	base := strings.TrimSuffix(c.url, "/api/websocket")
	base = "http" + strings.TrimPrefix(base, "ws")
	req, err := http.NewRequestWithContext(ctx, method, base+"/api/config/automation/config/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("home assistant's automation config: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s %s answered %s", ErrNotAdmin, method, id, resp.Status)
	case resp.StatusCode == http.StatusNotFound && method == http.MethodGet:
		return nil, fmt.Errorf("%w: %s", ErrNoAutomation, id)
	case resp.StatusCode >= 300:
		return nil, &ConfigError{Method: method, ID: id, Status: resp.Status, Answer: excerpt.Bytes(answer), Code: resp.StatusCode}
	}
	return answer, nil
}
