package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/tools"
)

const (
	ToolStart   = "thread_start"
	ToolFill    = "thread_fill"
	ToolConfirm = "thread_confirm"
	ToolCancel  = "thread_cancel"
	ToolUndo    = "thread_undo"
)

// Tools are the thread tools; none without a target. The list of targets
// sits in thread_start's description, fixed for the run, so the cached
// prompt prefix does not change.
func (m *Manager) Tools() []tools.InternalTool {
	if !m.HasTargets() {
		return nil
	}
	t := m.texts()
	var list strings.Builder
	names := make([]string, 0, len(m.targets))
	for name := range m.targets {
		names = append(names, name)
	}
	for _, name := range sortedNames(names) {
		fmt.Fprintf(&list, "\n- %s: %s", name, m.targets[name].Description)
	}
	empty := `{"type":"object","properties":{}}`
	out := []tools.InternalTool{
		{
			Name: ToolStart, Description: t.startDesc + list.String(),
			Schema: fmt.Sprintf(`{"type":"object","properties":{"target":{"type":"string","enum":%s},"request":{"type":"string","description":%q}},"required":["target","request"]}`,
				mustJSON(sortedNames(names)), t.requestDesc),
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				var a struct {
					Target  string `json:"target"`
					Request string `json:"request"`
				}
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", err
				}
				return m.Start(ctx, inv, strings.TrimSpace(a.Target), a.Request)
			},
		},
		{
			Name: ToolFill, Description: t.fillDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"fields":{"type":"object","description":%q,"additionalProperties":{"type":"string"}}},"required":["fields"]}`, t.fieldsDesc),
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				var a struct {
					Fields map[string]any `json:"fields"`
				}
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", err
				}
				values := map[string]string{}
				for k, v := range a.Fields {
					switch x := v.(type) {
					case string:
						values[k] = x
					default:
						raw, _ := json.Marshal(x)
						values[k] = string(raw)
					}
				}
				return m.Fill(ctx, inv, values)
			},
		},
		{
			Name: ToolConfirm, Description: t.confirmDesc, Schema: empty, AskAfterWeb: true,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) { return m.Confirm(ctx, inv) },
		},
		{
			Name: ToolCancel, Description: t.cancelDesc, Schema: empty,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) { return m.Cancel(ctx, inv) },
		},
	}
	// Taking back is there only when a target can take back what it did.
	for _, sk := range m.targets {
		if sk.Target.Undo != nil {
			out = append(out, tools.InternalTool{
				Name: ToolUndo, Description: t.undoDesc, Schema: empty,
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					place, _ := m.place(ctx, inv)
					return m.Undo(ctx, place)
				},
			})
			break
		}
	}
	return out
}

func sortedNames(names []string) []string {
	out := append([]string(nil), names...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
