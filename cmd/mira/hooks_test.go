package main

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
)

// HK-84 A skill's action is a call of its own for the hooks
func TestHK84_ASkillsActionIsACallOfItsOwnForTheHooks(t *testing.T) {
	snap := structure.Build(structure.Registry{
		Areas:    []haws.Area{{AreaID: "flur", Name: "Flur"}},
		Entities: []haws.Entity{{EntityID: "lock.haustuer", AreaID: "flur", Name: "Haustür"}},
		Exposed:  map[string]bool{"lock.haustuer": true},
	})
	a := &app{hooks: hooks.Load("", i18n.DE, nil)}
	a.guard = tools.NewGuard(i18n.DE, config.Default().Security, nil, func() structure.Snapshot { return snap }, a.hooks)
	ctx := context.Background()
	unlock := threads.ActionCall{Skill: "tuer", Action: "lock.unlock", Data: map[string]any{"entity_id": "lock.haustuer"}, Person: "anna"}

	// Outside a turn: the thread's person; a lock asks.
	if d := a.actionDecision(ctx, unlock); d.Outcome != tools.NeedsConfirmation || d.By != "default/critical.star" {
		t.Fatalf("unlock: %+v", d)
	}
	// Without an entity the action's domain counts.
	if d := a.actionDecision(ctx, threads.ActionCall{Skill: "tuer", Action: "lock.unlock"}); d.Outcome != tools.NeedsConfirmation {
		t.Fatalf("unlock without entity: %+v", d)
	}
	// In a turn that read web content it is refused.
	ts := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice}
	a.guard.Observe(ts, tools.Spec{ToolDef: prompt.ToolDef{Name: "web_page"}, WebContent: true})
	if d := a.actionDecision(tools.WithTurn(ctx, ts), unlock); d.Outcome != tools.Deny || d.By != "system/web.star" {
		t.Fatalf("after web content: %+v", d)
	}
	// An alarm on a satellite runs.
	alarm := threads.ActionCall{Skill: "wecker", Action: "esphome.show_alarm_set", Data: map[string]any{"time": "06:00"}}
	if d := a.actionDecision(ctx, alarm); d.Outcome != tools.Allow {
		t.Fatalf("alarm: %+v", d)
	}
}
