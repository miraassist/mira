package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Config flows Home Assistant discovered and waits for a yes for
// (docs/spec/found-devices.md, the state table: checked on the test HA
// 2026-11-10).

// CmdFlowSubscribe subscribes to the open config flows: one event with every
// open flow at once (type empty), then each added and removed.
const CmdFlowSubscribe = "config_entries/flow/subscribe"

// FlowEvent is one change of the open config flows.
type FlowEvent struct {
	// Type is "" for the flows open when subscribing, "added" or "removed".
	Type   string `json:"type"`
	FlowID string `json:"flow_id"`
	Flow   *Flow  `json:"flow"`
}

// Flow is an open config flow.
type Flow struct {
	FlowID  string      `json:"flow_id"`
	Handler string      `json:"handler"`
	StepID  string      `json:"step_id"`
	Context FlowContext `json:"context"`
}

// FlowContext is how a flow started.
type FlowContext struct {
	Source            string          `json:"source"`
	UniqueID          string          `json:"unique_id"`
	TitlePlaceholders map[string]any  `json:"title_placeholders"`
	DiscoveryKey      json.RawMessage `json:"discovery_key"`
}

// Name is the flow's own name for what it found, "" without one.
func (f Flow) Name() string {
	if v, ok := f.Context.TitlePlaceholders["name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SubscribeFlows subscribes to the open config flows. The handler runs on the
// connection's read loop: it only passes the change on. After a reconnect the
// subscription is issued again and starts with every open flow once more.
func (c *Client) SubscribeFlows(ctx context.Context, handler func([]FlowEvent)) (func(), error) {
	return c.subscribeCommand(ctx, map[string]any{"type": CmdFlowSubscribe}, func(ev Event) {
		var list []FlowEvent
		if err := json.Unmarshal(ev.Raw, &list); err != nil {
			c.log.Warn("undecodable config flow event", "error", err)
			return
		}
		handler(list)
	})
}

// FlowField is one field of a flow's form.
type FlowField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// FlowStep is a flow's current step, as Home Assistant's frontend gets it.
type FlowStep struct {
	// Type is "form", "create_entry", "abort", or another the frontend knows
	// (menu, external, progress).
	Type                    string            `json:"type"`
	FlowID                  string            `json:"flow_id"`
	Handler                 string            `json:"handler"`
	StepID                  string            `json:"step_id"`
	DataSchema              []FlowField       `json:"data_schema"`
	Errors                  map[string]string `json:"errors"`
	DescriptionPlaceholders map[string]any    `json:"description_placeholders"`
	Title                   string            `json:"title"`
	Reason                  string            `json:"reason"`
	Result                  struct {
		EntryID string `json:"entry_id"`
	} `json:"result"`
}

// GetFlow reads a flow's current step (GET /api/config/config_entries/flow/<id>).
func (c *Client) GetFlow(ctx context.Context, flowID string) (FlowStep, error) {
	return c.flowRequest(ctx, http.MethodGet, flowID, nil)
}

// SubmitFlow submits a flow's current step with the form's data ({} for a
// step without fields) and returns the next one.
func (c *Client) SubmitFlow(ctx context.Context, flowID string, data map[string]any) (FlowStep, error) {
	if data == nil {
		data = map[string]any{}
	}
	body, err := json.Marshal(data)
	if err != nil {
		return FlowStep{}, err
	}
	return c.flowRequest(ctx, http.MethodPost, flowID, body)
}

func (c *Client) flowRequest(ctx context.Context, method, flowID string, body []byte) (FlowStep, error) {
	answer, err := c.rest(ctx, method, "/api/config/config_entries/flow/"+url.PathEscape(flowID), body)
	if err != nil {
		return FlowStep{}, err
	}
	var step FlowStep
	if err := json.Unmarshal(answer, &step); err != nil {
		return FlowStep{}, fmt.Errorf("home assistant's answer for flow %s is not understood: %w", flowID, err)
	}
	return step, nil
}

// rest makes a request to Home Assistant's REST API with Mira's token and
// returns the answer's body; an answer of 300 or more is an error with it.
func (c *Client) rest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	base := strings.TrimSuffix(c.url, "/api/websocket")
	base = "http" + strings.TrimPrefix(base, "ws")
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("home assistant %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s %s answered %s", ErrNotAdmin, method, path, resp.Status)
	case resp.StatusCode >= 300:
		return nil, fmt.Errorf("home assistant refused %s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(answer)))
	}
	return answer, nil
}
