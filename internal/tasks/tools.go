package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
)

const (
	ToolTaskCreate  = "task_create"
	ToolTaskConfirm = "task_confirm"
	ToolTaskList    = "task_list"
	ToolTaskDelete  = "task_delete"
)

// Tools are the task tools. Creating plans and asks; only the user's yes,
// through task_confirm, starts a task.
func (m *Manager) Tools() []tools.InternalTool {
	t := m.texts()
	id := `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`
	// Confirming may come in a later conversation than the proposal, so
	// without an id it starts the newest proposed task.
	optionalID := `{"type":"object","properties":{"id":{"type":"string"}}}`
	readID := func(raw json.RawMessage) (string, error) {
		var a struct {
			ID string `json:"id"`
		}
		if len(raw) == 0 {
			return "", nil
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		return strings.TrimSpace(a.ID), nil
	}
	notFound := func(err error) error {
		if errors.Is(err, store.ErrNotFound) {
			return errors.New(t.noTask)
		}
		return err
	}
	return []tools.InternalTool{
		{
			Name: ToolTaskCreate, Description: t.createDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"goal":{"type":"string","description":%q}},"required":["goal"]}`, t.createGoal),
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				var a struct {
					Goal string `json:"goal"`
				}
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Goal) == "" {
					return "", errors.New("goal is missing")
				}
				if m.opts.Deliver != nil {
					m.Propose(ctx, a.Goal, inv)
					return t.planning, nil
				}
				task, _, err := m.Create(ctx, a.Goal, inv.SessionKey)
				if err != nil {
					return "", err
				}
				return t.proposed + m.Describe(task), nil
			},
		},
		{
			Name: ToolTaskConfirm, Description: t.confirmDesc, Schema: optionalID,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				tid, err := readID(inv.Args)
				if err != nil {
					return "", err
				}
				task, err := m.Activate(ctx, tid)
				if err != nil {
					return "", notFound(err)
				}
				return t.activated + task.Title, nil
			},
		},
		{
			Name: ToolTaskList, Description: t.listDesc, Schema: `{"type":"object","properties":{}}`, ReadOnly: true,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				list, err := m.opts.Store.Tasks(ctx)
				if err != nil {
					return "", err
				}
				if len(list) == 0 {
					return t.none, nil
				}
				parts := make([]string, len(list))
				for i, task := range list {
					parts[i] = m.Describe(task)
				}
				return strings.Join(parts, "\n\n"), nil
			},
		},
		{
			Name: ToolTaskDelete, Description: t.deleteDesc, Schema: id,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				tid, err := readID(inv.Args)
				if err != nil {
					return "", err
				}
				if err := m.Delete(ctx, tid); err != nil {
					return "", notFound(err)
				}
				return t.deleted, nil
			},
		},
	}
}
