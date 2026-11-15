package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolDue names the to-dos due by a day (docs/spec/todo-due.md, Reading).
const ToolDue = "todo_due"

// The limits of todo_due (docs/spec/todo-due.md, decision 9).
const (
	maxDue      = 20
	overdueDays = 30
)

// Item is an open to-do as Home Assistant's todo.get_items returns it.
type Item struct {
	Summary     string `json:"summary"`
	Status      string `json:"status"`
	Due         string `json:"due"`
	Description string `json:"description"`
}

type dueItem struct {
	summary string
	day     time.Time
}

// Due names the open to-dos of a list due from 30 days before today up to
// until (zero: today), overdue ones first. An error is Home Assistant's or
// the list's; the text is for the model.
func (t *Todo) Due(ctx context.Context, name string, until time.Time) (string, error) {
	tx := t.texts(ctx)
	lists, err := t.opts.Lists(ctx)
	if err != nil {
		t.log.Warn("could not read the to-do lists for todo_due", "list", name, "error", err)
		return fmt.Sprintf(tx.unreadable, name), nil
	}
	var list *List
	for i, l := range lists {
		if strings.EqualFold(l.Name, strings.TrimSpace(name)) || strings.EqualFold(l.Entity, strings.TrimSpace(name)) {
			list = &lists[i]
			break
		}
	}
	if list == nil {
		t.log.Info("todo_due: unknown list", "list", name)
		return fmt.Sprintf(tx.unknownList, name, names(lists)), nil
	}
	items, err := t.opts.Items(ctx, list.Entity)
	if err != nil {
		t.log.Warn("could not read the to-dos for todo_due", "list", list.Entity, "error", err)
		return fmt.Sprintf(tx.unreadable, list.Name), nil
	}
	now := t.opts.Clock().In(t.opts.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, t.opts.Location)
	if until.IsZero() {
		until = today
	}
	from := today.AddDate(0, 0, -overdueDays)
	var due []dueItem
	for _, it := range items {
		if it.Due == "" || (it.Status != "" && it.Status != "needs_action") {
			continue
		}
		day, err := t.dueDay(it.Due)
		if err != nil {
			t.log.Warn("todo_due: a due date that does not parse is left out", "list", list.Entity, "item", it.Summary, "due", it.Due)
			continue
		}
		if day.Before(from) || day.After(until) {
			continue
		}
		due = append(due, dueItem{strings.TrimSpace(it.Summary), day})
	}
	if len(due) == 0 {
		return fmt.Sprintf(tx.nothingDue, list.Name), nil
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].day.Before(due[j].day) })
	l := t.lang(ctx)
	var b strings.Builder
	head := ""
	for i, d := range due {
		if i == maxDue {
			fmt.Fprintf(&b, tx.more+"\n", len(due)-maxDue)
			break
		}
		h := dayLabel(tx, l, d.day, today)
		if d.day.Before(today) {
			h = tx.overdue
		}
		if h != head {
			b.WriteString(h + "\n")
			head = h
		}
		if d.day.Before(today) {
			fmt.Fprintf(&b, "- %s · %s (%s)\n", d.summary, fmt.Sprintf(tx.dueSince, shortDate(l, d.day)), list.Name)
		} else {
			fmt.Fprintf(&b, "- %s (%s)\n", d.summary, list.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// dueDay is the day of a due date: "YYYY-MM-DD", or a date and time (its
// day where Mira is).
func (t *Todo) dueDay(s string) (time.Time, error) {
	if d, err := time.ParseInLocation(time.DateOnly, s, t.opts.Location); err == nil {
		return d, nil
	}
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	v = v.In(t.opts.Location)
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, t.opts.Location), nil
}

func dayLabel(tx texts, l i18n.Lang, day, today time.Time) string {
	if day.Equal(today) {
		return tx.today
	}
	return i18n.FormatDate(l, day)
}

// shortDate: "Mittwoch 12.11." or "Wednesday 12 Nov".
func shortDate(l i18n.Lang, d time.Time) string {
	full := i18n.FormatDate(l, d)
	wd, _, _ := strings.Cut(full, ",")
	if l == i18n.EN {
		return fmt.Sprintf("%s %d %s", wd, d.Day(), d.Month().String()[:3])
	}
	return fmt.Sprintf("%s %d.%d.", wd, d.Day(), int(d.Month()))
}

// DueTool is todo_due, the tool on Due for the skill briefing and its
// schedules (docs/spec/todo-due.md, Reading).
func (t *Todo) DueTool(lang i18n.Lang, skill string) tools.InternalTool {
	tx := i18n.Pick(catalog, lang)
	return tools.InternalTool{
		Name: ToolDue, Description: tx.dueDesc, ReadOnly: true, Skill: skill, Background: true,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"list":{"type":"string","description":%q},"until":{"type":"string","description":%q}},"required":["list"]}`,
			tx.dueListDesc, tx.dueUntilDesc),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct{ List, Until string }
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("arguments of %s: %w", ToolDue, err)
			}
			var until time.Time
			if s := strings.TrimSpace(a.Until); s != "" {
				d, err := time.ParseInLocation(time.DateOnly, s, t.opts.Location)
				if err != nil {
					return "", fmt.Errorf("%s: until %q is no day like 2026-11-30", ToolDue, s)
				}
				until = d
			}
			return t.Due(ctx, a.List, until)
		},
	}
}
