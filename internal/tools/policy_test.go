package tools

import (
	"context"
	"encoding/json"
	"github.com/miraassist/mira/internal/i18n"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
)

type memConfirmations struct{ last *store.Confirmation }

func (m *memConfirmations) PutConfirmation(_ context.Context, c store.Confirmation, _ time.Duration) error {
	m.last = &c
	return nil
}

func house() structure.Snapshot {
	return structure.Build(structure.Registry{
		Areas: []haws.Area{{AreaID: "flur", Name: "Flur"}, {AreaID: "garage", Name: "Garage"}, {AreaID: "kueche", Name: "Küche"}},
		Entities: []haws.Entity{
			{EntityID: "lock.haustuer", AreaID: "flur", Name: "Haustür"},
			{EntityID: "light.flur", AreaID: "flur", Name: "Flurlicht"},
			{EntityID: "cover.garage", AreaID: "garage", Name: "Garagentor", Aliases: []string{"Tor"}},
			{EntityID: "light.kueche", AreaID: "kueche", Name: "Küchenlicht"},
			{EntityID: "switch.pumpe", AreaID: "garage", Name: "Pumpe", Labels: []string{structure.LabelConfirm}},
		},
		// What the user shared with Assist.
		Exposed: map[string]bool{"lock.haustuer": true, "light.flur": true, "cover.garage": true, "light.kueche": true, "switch.pumpe": true},
	})
}

func newGuard() (*Guard, *memConfirmations) {
	sec := config.Default().Security
	sec.ConfirmEntities = []string{"cover.garage"}
	mem := &memConfirmations{}
	return NewGuard(i18n.DE, sec, mem, house), mem
}

var (
	turnOn  = Spec{ToolDef: prompt.ToolDef{Server: "ha", Name: "HassTurnOn"}}
	turnOff = Spec{ToolDef: prompt.ToolDef{Server: "ha", Name: "HassTurnOff"}}
	live    = Spec{ToolDef: prompt.ToolDef{Server: "ha", Name: "GetLiveContext"}, ReadOnly: true}
	search  = Spec{ToolDef: prompt.ToolDef{Server: "search", Name: "web_search"}, ReadOnly: true, WebContent: true}
)

func args(s string) json.RawMessage { return json.RawMessage(s) }

func TestCriticalDetection(t *testing.T) {
	g, _ := newGuard()
	cases := []struct {
		spec     Spec
		args     string
		critical bool
	}{
		{turnOn, `{"name":"Küchenlicht"}`, false},
		{turnOn, `{"domain":["light"],"area":"Flur"}`, false},
		{turnOn, `{"domain":["lock"]}`, true},
		{turnOn, `{"name":"Haustür"}`, true},
		{turnOff, `{"name":"haustür","area":"flur"}`, true},
		{turnOn, `{"name":"Haustürschloss"}`, true}, // unresolved but resembles a lock
		{turnOn, `{"area":"Flur"}`, true},           // whole area includes the lock
		{turnOn, `{"area":"Küche"}`, false},
		{turnOff, `{"name":"Tor"}`, true}, // alias of a confirm entity
		{turnOn, `{"entity_id":"lock.keller"}`, true},
		{turnOn, `{"name":"Pumpe"}`, true}, // labelled "Mira: nachfragen" in Home Assistant
		{live, `{}`, false},
		{turnOn, `not json`, false},
	}
	for _, c := range cases {
		got, targets := g.critical(c.spec, args(c.args))
		if got != c.critical {
			t.Errorf("%s %s: critical = %v (targets %v), want %v", c.spec.QualifiedName(), c.args, got, targets, c.critical)
		}
	}
}

func TestConfirmTools(t *testing.T) {
	sec := config.Default().Security
	sec.ConfirmTools = []string{"ha__HassTurnOff"}
	g := NewGuard(i18n.DE, sec, &memConfirmations{}, house)
	if v := g.Check(context.Background(), &TurnState{SessionKey: "s"}, turnOff, args(`{"name":"Küchenlicht"}`)); v.Outcome != NeedsConfirmation {
		t.Fatalf("configured tool not gated: %+v", v)
	}
}

func TestLockNeedsConfirmationFromTheUser(t *testing.T) {
	g, mem := newGuard()
	ctx := context.Background()
	call := args(`{"name":"Haustür"}`)

	// Turn 1: the model tries to lock. Nothing runs; a confirmation is parked.
	t1 := &TurnState{SessionKey: "telegram:1", Channel: config.ChannelTelegram, UserText: "Schließ die Haustür ab"}
	v := g.Check(ctx, t1, turnOn, call)
	if v.Outcome != NeedsConfirmation || mem.last == nil {
		t.Fatalf("turn 1 verdict = %+v, stored = %v", v, mem.last)
	}
	if mem.last.ToolName != "ha__HassTurnOn" || mem.last.Arguments != `{"name":"Haustür"}` {
		t.Fatalf("stored confirmation = %+v", mem.last)
	}

	// Turn 2: the user says yes. Exactly one matching call may run.
	t2 := &TurnState{SessionKey: "telegram:1", Channel: config.ChannelTelegram, UserText: "Ja, bitte", Pending: mem.last}
	if v := g.Check(ctx, t2, turnOn, call); v.Outcome != Allow {
		t.Fatalf("confirmed call verdict = %+v", v)
	}
	if v := g.Check(ctx, t2, turnOn, call); v.Outcome != NeedsConfirmation {
		t.Fatalf("one approval allowed a second call: %+v", v)
	}
}

func TestConfirmationMatchesByTarget(t *testing.T) {
	g, mem := newGuard()
	ctx := context.Background()
	g.Check(ctx, &TurnState{SessionKey: "s"}, turnOn, args(`{"name":"Haustür"}`))
	t2 := &TurnState{SessionKey: "s", UserText: "ok", Pending: mem.last}
	if v := g.Check(ctx, t2, turnOn, args(`{"name":"Haustür","area":"Flur"}`)); v.Outcome != Allow {
		t.Fatalf("same target with extra area should match: %+v", v)
	}
}

func TestConfirmationIsNotTransferable(t *testing.T) {
	g, mem := newGuard()
	ctx := context.Background()
	g.Check(ctx, &TurnState{SessionKey: "s"}, turnOn, args(`{"name":"Haustür"}`))
	pending := mem.last

	cases := []struct {
		name string
		text string
		spec Spec
		args string
	}{
		{"user says no", "Nein", turnOn, `{"name":"Haustür"}`},
		{"user asks something else", "Ist die Garage zu?", turnOn, `{"name":"Haustür"}`},
		{"different tool", "ja", turnOff, `{"name":"Haustür"}`},
		{"different target", "ja", turnOff, `{"name":"Garagentor"}`},
	}
	for _, c := range cases {
		p := *pending
		ts := &TurnState{SessionKey: "s", UserText: c.text, Pending: &p}
		if v := g.Check(ctx, ts, c.spec, args(c.args)); v.Outcome == Allow {
			t.Errorf("%s: call was allowed", c.name)
		}
	}
}

func TestNoCriticalCallAfterWebContent(t *testing.T) {
	g, mem := newGuard()
	ctx := context.Background()
	g.Check(ctx, &TurnState{SessionKey: "s"}, turnOn, args(`{"name":"Haustür"}`))

	// Even with a valid yes from the user, a turn that read web content may
	// not run critical tools: the web page could have steered the call.
	ts := &TurnState{SessionKey: "s", UserText: "ja", Pending: mem.last}
	if v := g.Check(ctx, ts, search, args(`{"query":"x"}`)); v.Outcome != Allow {
		t.Fatalf("search itself must be allowed: %+v", v)
	}
	g.Observe(ts, search)
	if v := g.Check(ctx, ts, turnOn, args(`{"name":"Haustür"}`)); v.Outcome != Deny || v.Reason != "after_web_content" {
		t.Fatalf("critical call after web content = %+v", v)
	}
	if v := g.Check(ctx, ts, turnOn, args(`{"name":"Küchenlicht"}`)); v.Outcome != Allow {
		t.Fatalf("non-critical call after web content = %+v", v)
	}
}

func TestBackgroundJobsAreReadOnly(t *testing.T) {
	g, _ := newGuard()
	ctx := context.Background()
	ts := &TurnState{SessionKey: "background:news", Channel: config.ChannelBackground}
	if v := g.Check(ctx, ts, turnOn, args(`{"name":"Küchenlicht"}`)); v.Outcome != Deny {
		t.Fatalf("background write = %+v", v)
	}
	if v := g.Check(ctx, ts, live, args(`{}`)); v.Outcome != Allow {
		t.Fatalf("background read = %+v", v)
	}
	memSave := Spec{ToolDef: prompt.ToolDef{Name: ToolMemorySave}, Internal: true}
	if v := g.Check(ctx, ts, memSave, args(`{"text":"x"}`)); v.Outcome != Deny {
		t.Fatalf("background memory write = %+v", v)
	}
}
