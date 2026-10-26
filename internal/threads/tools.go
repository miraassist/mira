package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/miraassist/mira/internal/i18n"
	"strings"

	"github.com/miraassist/mira/internal/tools"
)

const (
	ToolStart   = "thread_start"
	ToolFill    = "thread_fill"
	ToolConfirm = "thread_confirm"
	ToolCancel  = "thread_cancel"
	ToolUndo    = "thread_undo"
	ToolPlan    = "thread_plan"
)

// Tools are the thread tools; none without a target. The list of targets
// sits in thread_start's description, the same for every turn of a language,
// so the cached prompt prefix does not change.
func (m *Manager) Tools() []tools.InternalTool {
	if !m.HasTargets() {
		return nil
	}
	t := m.texts()
	names := make([]string, 0, len(m.targets))
	for name := range m.targets {
		names = append(names, name)
	}
	empty := `{"type":"object","properties":{}}`
	out := []tools.InternalTool{
		{
			Name: ToolStart, Description: m.describeTargets(m.opts.Lang), DescribeIn: m.describeTargets,
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
	// The plan tool is there only when a target has a plan.
	for _, sk := range m.targets {
		if sk.Target.Plan {
			out = append(out, tools.InternalTool{
				Name: ToolPlan, Description: t.planDesc,
				Schema: fmt.Sprintf(`{"type":"object","properties":{"action":{"type":"string","enum":["write","check","uncheck","read"]},"text":{"type":"string","description":%q},"step":{"type":"integer","description":%q}},"required":["action"]}`,
					t.planTextDesc, t.planStepDesc),
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					var a struct {
						Action string `json:"action"`
						Text   string `json:"text"`
						Step   int    `json:"step"`
					}
					if err := json.Unmarshal(inv.Args, &a); err != nil {
						return "", err
					}
					return m.Plan(ctx, inv, strings.TrimSpace(a.Action), a.Text, a.Step)
				},
			})
			break
		}
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

// describeTargets is thread_start's description in l: what it does and the
// targets with their skills' descriptions in that language.
func (m *Manager) describeTargets(l i18n.Lang) string {
	names := make([]string, 0, len(m.targets))
	for name := range m.targets {
		names = append(names, name)
	}
	var list strings.Builder
	for _, name := range sortedNames(names) {
		fmt.Fprintf(&list, "\n- %s: %s", name, m.targets[name].DescriptionIn(l))
	}
	return i18n.Pick(catalog, l).startDesc + list.String()
}
