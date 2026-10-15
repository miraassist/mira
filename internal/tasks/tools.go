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
	ToolTaskEnable  = "task_enable"
	ToolTaskTry     = "task_try"
)

// Tools are the task tools. Creating drafts and asks; only the user's yes,
// through task_confirm, creates the automation.
func (m *Manager) Tools() []tools.InternalTool {
	t := m.texts()
	idSchema := fmt.Sprintf(`{"type":"object","properties":{"id":{"type":"string","description":%q}},"required":["id"]}`, t.idDesc)
	var idArg struct {
		ID string `json:"id"`
	}
	readID := func(raw json.RawMessage) (string, error) {
		idArg.ID = ""
		if err := json.Unmarshal(raw, &idArg); err != nil {
			return "", err
		}
		return strings.TrimSpace(idArg.ID), nil
	}
	notFound := func(err error) error {
		if errors.Is(err, store.ErrNotFound) {
			return errors.New(t.notFound)
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
				origin, _ := m.origin(ctx, inv)
				task, err := m.Create(ctx, a.Goal, inv.SessionKey, origin)
				if err != nil {
					return "", err
				}
				return m.proposalText(task), nil
			},
		},
		{
			// No id: the model invents one. A yes confirms the newest
			// proposal made where it is said.
			Name: ToolTaskConfirm, Description: t.confirmDesc, Schema: `{"type":"object","properties":{}}`,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				origin, _ := m.origin(ctx, inv)
				task, err := m.ConfirmLatest(ctx, origin)
				if errors.Is(err, store.ErrNotFound) {
					return "", errors.New(t.noProposal)
				}
				if err != nil {
					return "", fmt.Errorf(t.saveFailed, err)
				}
				return fmt.Sprintf(t.confirmed, task.Title), nil
			},
		},
		{
			Name: ToolTaskList, Description: t.listDesc, Schema: `{"type":"object","properties":{}}`, ReadOnly: true,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				items, err := m.List(ctx)
				if err != nil && len(items) == 0 {
					return "", err
				}
				if len(items) == 0 {
					return t.none, nil
				}
				var b strings.Builder
				for _, it := range items {
					fmt.Fprintf(&b, "- id %s: %s (%s) – %s\n", it.ID, it.Title, it.State, it.Goal)
				}
				return strings.TrimSpace(b.String()), nil
			},
		},
		{
			Name: ToolTaskDelete, Description: t.deleteDesc, Schema: idSchema,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				id, err := readID(inv.Args)
				if err != nil {
					return "", err
				}
				if err := m.Delete(ctx, id); err != nil {
					return "", notFound(err)
				}
				return t.deleted, nil
			},
		},
		{
			Name: ToolTaskEnable, Description: t.enableDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"id":{"type":"string","description":%q},"on":{"type":"boolean","description":%q}},"required":["id","on"]}`, t.idDesc, t.enableOn),
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
					On bool   `json:"on"`
				}
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", err
				}
				if err := m.SetEnabled(ctx, strings.TrimSpace(a.ID), a.On); err != nil {
					return "", notFound(err)
				}
				if a.On {
					return t.enabledOn, nil
				}
				return t.enabledOff, nil
			},
		},
		{
			Name: ToolTaskTry, Description: t.tryDesc, Schema: idSchema, ReadOnly: true,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				id, err := readID(inv.Args)
				if err != nil {
					return "", err
				}
				res, err := m.Try(ctx, id)
				if err != nil {
					return "", notFound(err)
				}
				return m.DescribeTry(res), nil
			},
		},
	}
}

// DescribeTry says what a dry run found.
func (m *Manager) DescribeTry(res TryResult) string {
	t := m.texts()
	var parts []string
	for _, v := range res.Verdicts {
		said := v.Spoken
		if said == "" {
			said = v.Details // a quiet verdict says why
		}
		parts = append(parts, fmt.Sprintf(t.triedVerdict, v.Importance, said))
	}
	for _, d := range res.Decisions {
		parts = append(parts, fmt.Sprintf(t.triedDecided, d.Answer, d.Reason))
	}
	if len(parts) == 0 {
		return t.triedNothing
	}
	return strings.Join(parts, "\n")
}
