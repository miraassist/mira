package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
)

// CmdRenderTemplate renders a template in Home Assistant: a subscription
// whose first event is the result, sent again on every change of what the
// template reads.
const CmdRenderTemplate = "render_template"

// TemplateTimeout is how long RenderTemplate waits for the first answer
// (docs/spec/group-watch.md, Errors C1).
const TemplateTimeout = 5 * time.Second

// RenderTemplate has Home Assistant render a template once, with variables:
// the subscription's first answer, then the subscription ends. The result
// comes as Home Assistant sends it (a list or an object it parsed, or a
// string). A warning while rendering is passed over. A template that does not render is Home Assistant's refusal
// (code "template_error"), with its message; no answer within
// TemplateTimeout is a timeout.
func (c *Client) RenderTemplate(ctx context.Context, template string, variables map[string]any) (json.RawMessage, error) {
	type answer struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
		Level  string          `json:"level"`
	}
	first := make(chan answer, 1)
	cmd := map[string]any{"type": CmdRenderTemplate, "template": template, "report_errors": true}
	if len(variables) > 0 {
		cmd["variables"] = variables
	}
	cancel, err := c.subscribeCommand(ctx, cmd, func(ev Event) {
		var a answer
		if err := json.Unmarshal(ev.Raw, &a); err != nil {
			a.Error = fmt.Sprintf("an answer not understood: %q", excerpt.Bytes(ev.Raw))
		}
		if a.Error != "" && a.Level == "WARNING" {
			return // a warning of the template's rendering; its result follows
		}
		select {
		case first <- a: // runs on the read loop: never blocks
		default:
		}
	})
	if err != nil {
		return nil, fmt.Errorf("render_template of %q: %w", excerpt.Of(template), err)
	}
	defer cancel()
	timer := time.NewTimer(TemplateTimeout)
	defer timer.Stop()
	select {
	case a := <-first:
		if a.Error != "" {
			return nil, &Error{Command: CmdRenderTemplate, Code: "template_error", Message: a.Error}
		}
		return a.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("render_template of %q: no answer within %s: %w", excerpt.Of(template), TemplateTimeout, context.DeadlineExceeded)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
