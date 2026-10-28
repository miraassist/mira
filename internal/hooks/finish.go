package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
)

// RuleField is the field of the skill "regel" the agent fills with the rule
// as JSON; WishField the user's words (docs/spec/hooks-schreiben.md).
const (
	RuleField = "regel"
	WishField = "wunsch"
)

// SaveFinisher is the building block "hook.save": it proposes the rule the
// agent wrote, in one sentence, and after the yes saves it, once the hooks
// let it (hook_save, system/self.star). guard is asked when the yes comes (it
// may be built after the finisher); name says how Mira names a person.
func (e *Engine) SaveFinisher(guard func() *tools.Guard, name func(person string) string, clock func() time.Time) threads.Finisher {
	if clock == nil {
		clock = time.Now
	}
	return saveFinisher{e: e, guard: guard, name: name, clock: clock}
}

type saveFinisher struct {
	e     *Engine
	guard func() *tools.Guard
	name  func(string) string
	clock func() time.Time
}

func (saveFinisher) Async() bool { return false }

func (f saveFinisher) Work(_ context.Context, th store.Thread) (threads.Result, error) {
	var r Rule
	if err := json.Unmarshal([]byte(th.Fields[RuleField]), &r); err != nil || r.File == "" || r.Code == "" {
		return threads.Result{Cancel: f.e.texts.noRule}, nil
	}
	content, _ := json.Marshal(r)
	return threads.Result{Proposal: fmt.Sprintf(f.e.texts.proposal, strings.TrimSpace(r.Summary)), Content: content}, nil
}

func (f saveFinisher) Commit(ctx context.Context, th store.Thread) (string, error) {
	var r Rule
	if err := json.Unmarshal(th.Content, &r); err != nil || r.File == "" {
		return "", errors.New(f.e.texts.noRule)
	}
	// The yes is said; what the hooks refuse stays refused (an unknown
	// voice, third-party text in the turn).
	if g := f.guard(); g != nil {
		ts := tools.TurnOf(ctx)
		if ts == nil {
			// Outside a turn (the page's yes): the thread's person.
			ts = &tools.TurnState{SessionKey: "thread:" + th.ID, Person: th.Person}
		}
		args, _ := json.Marshal(map[string]string{"name": r.File})
		spec := tools.Spec{ToolDef: prompt.ToolDef{Name: ToolSave}, Internal: true}
		if d := g.Decide(ctx, ts, spec, args); d.Outcome == tools.Deny {
			return "", errors.New(d.Reason)
		}
	}
	who := th.Person
	if f.name != nil && f.name(th.Person) != "" {
		who = f.name(th.Person)
	}
	if err := f.e.Save(r, who, th.Fields[WishField], f.clock()); err != nil {
		return "", err
	}
	return fmt.Sprintf(f.e.texts.saved, r.File), nil
}
