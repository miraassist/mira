package hooks

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/tools"
)

// A change of a task passes the hooks as the call task_edit, with the
// entities its actions reach (docs/spec/aufgaben-aendern.md, Saving).

var taskEdit = tools.Spec{ToolDef: prompt.ToolDef{Name: "task_edit"}}

// AE-31 After web content a change is refused
func TestAE31_AfterWebContentAChangeIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	anna := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "anna"}
	unlock := args(`{"task":"automation.morgens","entity_id":["lock.haustuer"]}`)
	// Without web content the lock asks for a yes: the thread's yes is it.
	if d := f.g.Decide(context.Background(), anna, taskEdit, unlock); d.Outcome != tools.NeedsConfirmation {
		t.Fatalf("without web content: %+v", d)
	}
	f.g.Observe(anna, search)
	if d := f.g.Decide(context.Background(), anna, taskEdit, unlock); d.Outcome != tools.Deny || d.By != "system/web.star" {
		t.Fatalf("after web content: %+v", d)
	}
	// A lock behind an area is seen by its domain.
	if d := f.g.Decide(context.Background(), anna, taskEdit, args(`{"task":"automation.morgens","domain":["lock"]}`)); d.Outcome != tools.Deny {
		t.Fatalf("by domain: %+v", d)
	}
	// What is not critical stays allowed, also after web content.
	if d := f.g.Decide(context.Background(), anna, taskEdit, args(`{"task":"automation.abend","entity_id":["light.flur"]}`)); d.Outcome != tools.Allow {
		t.Fatalf("a light: %+v", d)
	}
}

// AE-32 An unknown voice cannot change a task
func TestAE32_AnUnknownVoiceCannotChangeATask(t *testing.T) {
	f := newFixture(t, nil)
	light := args(`{"task":"automation.abend","entity_id":["light.flur"]}`)
	if d := f.g.Decide(context.Background(), voice(), taskEdit, light); d.Outcome != tools.Deny || d.By != "system/self.star" {
		t.Fatalf("unknown voice: %+v", d)
	}
	ben := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "ben"}
	if d := f.g.Decide(context.Background(), ben, taskEdit, light); d.Outcome != tools.Allow {
		t.Fatalf("ben: %+v", d)
	}
}
