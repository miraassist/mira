// Package todo is Mira's way with Home Assistant's to-do lists beside HA's
// own Assist tools: the building block todo.create, which checks an item
// and the list before Home Assistant's todo.add_item, and the tool todo_due,
// which names the to-dos due by a day (docs/spec/todo-due.md).
package todo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miraassist/mira/internal/i18n"
)

// ActionCreate is the building block's name where a skill's finish names it.
const ActionCreate = "todo.create"

// The limits of the building block (docs/spec/todo-due.md).
const (
	maxItem        = 80
	maxDescription = 1000
)

// Home Assistant's TodoListEntityFeature bits the building block needs.
const (
	FeatureDueDate     = 16
	FeatureDescription = 64
)

// List is a to-do list Mira may see: exposed to Assist.
type List struct {
	Entity, Name string
	// Features is the list's supported_features.
	Features int
}

// Options is what the to-do lists need of Home Assistant and the service.
type Options struct {
	// Lists lists the exposed to-do lists.
	Lists func(ctx context.Context) ([]List, error)
	// Owner is the person whose list a list's name is by convention
	// (docs/spec/calendar.md, Personal calendars); "" for none.
	Owner func(ctx context.Context, name string) string
	// Act calls a Home Assistant action (todo.add_item).
	Act func(ctx context.Context, domain, service string, data map[string]any) error
	// Items calls todo.get_items with its response, for todo_due.
	Items    func(ctx context.Context, entity string) ([]Item, error)
	Lang     i18n.Lang
	Location *time.Location
	Clock    func() time.Time
	Logger   *slog.Logger
}

// Todo is the building block and the tool on Home Assistant's lists.
type Todo struct {
	opts Options
	log  *slog.Logger
}

func New(opts Options) *Todo {
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Todo{opts: opts, log: opts.Logger}
}

func (t *Todo) lang(ctx context.Context) i18n.Lang {
	if l, ok := i18n.FromContext(ctx); ok {
		return l
	}
	return t.opts.Lang
}

func (t *Todo) texts(ctx context.Context) texts { return i18n.Pick(catalog, t.lang(ctx)) }

// NewItem is what the building block puts on a list.
type NewItem struct {
	Item, Description string
	// Due is the day it must be done by; zero for none.
	Due time.Time
	// List is a list's name or entity, "standard", or empty.
	List string
	// Person is whose turn or thread it is.
	Person string
}

// Refusal is the building block's no: nothing was entered, and Text says
// why in words the caller passes on.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return r.Text }

// Create is the building block todo.create (docs/spec/todo-due.md): it
// checks the item and the list, then calls Home Assistant's todo.add_item.
// A *Refusal is a no before Home Assistant was asked; any other error is
// Home Assistant's or the list of lists'.
func (t *Todo) Create(ctx context.Context, n NewItem) (List, error) {
	tx := t.texts(ctx)
	if err := checkItem(tx, &n); err != nil {
		t.log.Info("todo.create refused", "item", n.Item, "person", n.Person, "why", err.Error())
		return List{}, err
	}
	list, err := t.listFor(ctx, n)
	if err != nil {
		var r *Refusal
		if errors.As(err, &r) {
			t.log.Info("todo.create refused", "item", n.Item, "list", n.List, "person", n.Person, "why", r.Text)
		} else {
			t.log.Warn("could not read the to-do lists for todo.create", "item", n.Item, "error", err)
		}
		return List{}, err
	}
	data := map[string]any{"entity_id": list.Entity, "item": n.Item}
	if !n.Due.IsZero() {
		data["due_date"] = n.Due.Format(time.DateOnly)
	}
	if n.Description != "" {
		data["description"] = n.Description
	}
	if err := t.opts.Act(ctx, "todo", "add_item", data); err != nil {
		t.log.Warn("Home Assistant did not add the to-do", "list", list.Entity, "item", n.Item, "error", err)
		return List{}, fmt.Errorf("todo.add_item in %s for %q: %w", list.Entity, n.Item, err)
	}
	t.log.Info("to-do added", "list", list.Entity, "item", n.Item, "person", n.Person)
	return list, nil
}

func checkItem(tx texts, n *NewItem) error {
	n.Item = strings.TrimSpace(n.Item)
	n.Description = strings.TrimSpace(n.Description)
	switch {
	case n.Item == "":
		return &Refusal{tx.noItem}
	case utf8.RuneCountInString(n.Item) > maxItem:
		return &Refusal{fmt.Sprintf(tx.tooLong, "item", maxItem, n.Item)}
	case utf8.RuneCountInString(n.Description) > maxDescription:
		return &Refusal{fmt.Sprintf(tx.tooLong, "description", maxDescription, Cut(n.Description, 60))}
	}
	return nil
}

// IsDefault: no list named, the person's or the only one.
func IsDefault(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || strings.EqualFold(name, "standard") || strings.EqualFold(name, "default")
}

// listFor is the list by the one rule (docs/spec/todo-due.md): the one
// named, if exposed and no other person's; else the person's own; else the
// only one that is no other person's; else a refusal that asks which. With
// a due date or a description, only lists that can hold it count.
func (t *Todo) listFor(ctx context.Context, n NewItem) (List, error) {
	tx := t.texts(ctx)
	all, err := t.opts.Lists(ctx)
	if err != nil {
		return List{}, err
	}
	owner := func(l List) string {
		if t.opts.Owner == nil {
			return ""
		}
		return t.opts.Owner(ctx, l.Name)
	}
	need := 0
	if !n.Due.IsZero() {
		need |= FeatureDueDate
	}
	if n.Description != "" {
		need |= FeatureDescription
	}
	// What this person may put on, and of it what can hold the item.
	var may, can []List
	for _, l := range all {
		if o := owner(l); n.Person != "" && o != "" && o != n.Person {
			continue
		}
		may = append(may, l)
		if l.Features&need == need {
			can = append(can, l)
		}
	}
	if len(may) == 0 {
		return List{}, &Refusal{tx.noList}
	}
	if len(can) == 0 {
		return List{}, &Refusal{fmt.Sprintf(tx.noneCan, names(may))}
	}
	if name := strings.TrimSpace(n.List); !IsDefault(name) {
		for _, l := range may {
			if strings.EqualFold(l.Name, name) || strings.EqualFold(l.Entity, name) {
				if l.Features&need != need {
					return List{}, &Refusal{fmt.Sprintf(tx.cannotHold, l.Name, names(can))}
				}
				return l, nil
			}
		}
		return List{}, &Refusal{fmt.Sprintf(tx.notThat, name, names(can))}
	}
	if n.Person != "" {
		for _, l := range can {
			if owner(l) == n.Person {
				return l, nil
			}
		}
	}
	var free []List
	for _, l := range can {
		if owner(l) == "" {
			free = append(free, l)
		}
	}
	if len(free) == 1 {
		return free[0], nil
	}
	return List{}, &Refusal{fmt.Sprintf(tx.which, names(can))}
}

func names(ls []List) string {
	n := make([]string, len(ls))
	for i, l := range ls {
		n[i] = l.Name
	}
	return strings.Join(n, ", ")
}

// CreateAction is the building block as a skill's finish names it
// (action: todo.create): the fields of the skill todo (entity_id, item,
// due_date, description, person) as strings.
func (t *Todo) CreateAction(ctx context.Context, data map[string]any) error {
	str := func(k string) string { s, _ := data[k].(string); return strings.TrimSpace(s) }
	n := NewItem{Item: str("item"), Description: str("description"), List: str("entity_id"), Person: str("person")}
	if due := str("due_date"); due != "" {
		d, err := time.ParseInLocation(time.DateOnly, due, t.opts.Location)
		if err != nil {
			r := &Refusal{fmt.Sprintf(t.texts(ctx).badDue, due)}
			t.log.Info("todo.create refused", "item", n.Item, "person", n.Person, "why", r.Text)
			return r
		}
		n.Due = d
	}
	_, err := t.Create(ctx, n)
	return err
}

// Cut shortens s to n characters, for an error that names its input.
func Cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

type texts struct {
	noItem, tooLong, badDue, noList, noneCan, cannotHold, notThat, which string
	// todo_due
	dueDesc, dueListDesc, dueUntilDesc, overdue, today, dueSince, more, nothingDue, unknownList, unreadable string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		noItem:     "Der Eintrag fehlt.",
		tooLong:    "%s ist länger als %d Zeichen: %q",
		badDue:     "Die Fälligkeit %q passt nicht; erwartet ist ein Tag wie 2026-11-30.",
		noList:     "Es gibt keine To-do-Liste, auf die ich für diese Person etwas setzen darf. In Home Assistant müssen To-do-Listen für Assist freigegeben sein.",
		noneCan:    "Keine dieser Listen kann eine Fälligkeit oder Beschreibung speichern: %s",
		cannotHold: "Die Liste %s kann keine Fälligkeit oder Beschreibung speichern. Das können: %s",
		notThat:    "Die Liste %q gibt es nicht, oder sie gehört einer anderen Person. Möglich: %s",
		which:      "Welche Liste? %s",

		dueDesc:      "Die To-dos einer Liste, die bis zu einem Tag fällig sind, überfällige der letzten 30 Tage zuerst. Nur offene; ohne Beschreibung.",
		dueListDesc:  "Die Liste (Name oder Entität)",
		dueUntilDesc: "Letzter Fälligkeitstag als YYYY-MM-DD; ohne Angabe heute",
		overdue:      "Überfällig",
		today:        "Heute",
		dueSince:     "fällig seit %s",
		more:         "und %d weitere",
		nothingDue:   "Auf der Liste %s ist nichts fällig.",
		unknownList:  "Die Liste %q gibt es nicht. Freigegeben sind: %s",
		unreadable:   "Die Liste %s konnte ich nicht lesen.",
	},
	i18n.EN: {
		noItem:     "The item is missing.",
		tooLong:    "%s is longer than %d characters: %q",
		badDue:     "The due date %q does not fit; a day like 2026-11-30 is expected.",
		noList:     "There is no to-do list I may put something on for this person. In Home Assistant, to-do lists must be exposed to Assist.",
		noneCan:    "None of these lists can hold a due date or a description: %s",
		cannotHold: "The list %s cannot hold a due date or a description. These can: %s",
		notThat:    "There is no list %q, or it is another person's. Possible: %s",
		which:      "Which list? %s",

		dueDesc:      "The to-dos of a list due by a day, overdue ones of the last 30 days first. Open ones only; without descriptions.",
		dueListDesc:  "The list (name or entity)",
		dueUntilDesc: "Last due day as YYYY-MM-DD; today when left out",
		overdue:      "Overdue",
		today:        "Today",
		dueSince:     "due since %s",
		more:         "and %d more",
		nothingDue:   "Nothing is due on the list %s.",
		unknownList:  "There is no list %q. Exposed are: %s",
		unreadable:   "I could not read the list %s.",
	},
}
