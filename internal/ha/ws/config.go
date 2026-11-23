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
	_, err = c.configRequest(ctx, "automation", http.MethodPost, id, body)
	return err
}

// DeleteAutomationConfig removes one automation through the same API; Home
// Assistant removes its registry entry right after (check C2).
func (c *Client) DeleteAutomationConfig(ctx context.Context, id string) error {
	_, err := c.configRequest(ctx, "automation", http.MethodDelete, id, nil)
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
	return c.readConfig(ctx, "automation", id)
}

// ErrNoScript: Home Assistant has no script of that id (its script config
// API answered 404).
var ErrNoScript = errors.New("home assistant has no script of that id")

// SaveScriptConfig writes one script through Home Assistant's script config
// API, the one its script editor uses. It answers before the script is
// loaded: script.reload is the signal (docs/spec/skill-files.md, checked
// rows 5 and 6).
func (c *Client) SaveScriptConfig(ctx context.Context, id string, config map[string]any) error {
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = c.configRequest(ctx, "script", http.MethodPost, id, body)
	return err
}

// ScriptConfig reads one script as the script config API holds it
// ({alias, description, use_blueprint|sequence…}); ErrNoScript when there is
// none (docs/spec/skill-files.md, checked row 6).
func (c *Client) ScriptConfig(ctx context.Context, id string) (map[string]any, error) {
	return c.readConfig(ctx, "script", id)
}

func (c *Client) readConfig(ctx context.Context, kind, id string) (map[string]any, error) {
	raw, err := c.configRequest(ctx, kind, http.MethodGet, id, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("home assistant's %s config of %s answered %q: %w", kind, id, excerpt.Bytes(raw), err)
	}
	return out, nil
}

// ConfigError is a refusal of the automation or script config API, by its
// status: a 429 and a 5xx may pass, any other refusal is about the request.
type ConfigError struct {
	// Kind is "automation" or "script"; empty reads as automation.
	Kind, Method, ID, Status, Answer string
	Code                             int
}

func (e *ConfigError) Error() string {
	kind := e.Kind
	if kind == "" {
		kind = "automation"
	}
	return fmt.Sprintf("home assistant refused %s of %s %s: %s %s", e.Method, kind, e.ID, e.Status, e.Answer)
}

func (e *ConfigError) Retryable() bool {
	return e.Code == http.StatusTooManyRequests || e.Code >= 500
}

func (c *Client) configRequest(ctx context.Context, kind, method, id string, body []byte) ([]byte, error) {
	base := strings.TrimSuffix(c.url, "/api/websocket")
	base = "http" + strings.TrimPrefix(base, "ws")
	req, err := http.NewRequestWithContext(ctx, method, base+"/api/config/"+kind+"/config/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	// real time: a bound of a call on the socket to Home Assistant; no test waits for it
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("home assistant's %s config: %w", kind, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s %s answered %s", ErrNotAdmin, method, id, resp.Status)
	case resp.StatusCode == http.StatusNotFound && method == http.MethodGet:
		if kind == "script" {
			return nil, fmt.Errorf("%w: %s", ErrNoScript, id)
		}
		return nil, fmt.Errorf("%w: %s", ErrNoAutomation, id)
	case resp.StatusCode >= 300:
		return nil, &ConfigError{Kind: kind, Method: method, ID: id, Status: resp.Status, Answer: excerpt.Bytes(answer), Code: resp.StatusCode}
	}
	return answer, nil
}
