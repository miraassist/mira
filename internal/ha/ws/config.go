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
	return c.configRequest(ctx, http.MethodPost, id, body)
}

// DeleteAutomationConfig removes one automation through the same API; Home
// Assistant removes its registry entry right after (check C2).
func (c *Client) DeleteAutomationConfig(ctx context.Context, id string) error {
	return c.configRequest(ctx, http.MethodDelete, id, nil)
}

func (c *Client) configRequest(ctx context.Context, method, id string, body []byte) error {
	base := strings.TrimSuffix(c.url, "/api/websocket")
	base = "http" + strings.TrimPrefix(base, "ws")
	req, err := http.NewRequestWithContext(ctx, method, base+"/api/config/automation/config/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return err
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
		return fmt.Errorf("home assistant's automation config: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s %s answered %s", ErrNotAdmin, method, id, resp.Status)
	case resp.StatusCode >= 300:
		return fmt.Errorf("home assistant refused %s of automation %s: %s %s", method, id, resp.Status, strings.TrimSpace(string(answer)))
	}
	return nil
}
