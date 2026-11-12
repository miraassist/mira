package main

import (
	"os"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// docs/spec/inbox.md, step 9.
func TestIN44_TheSkillPostfachEntersThroughCalendarCreate(t *testing.T) {
	a := &app{lang: i18n.DE, calendar: calendar.New(calendar.Options{})}
	r := tools.NewRegistry(nil, a.calendarTools()...)
	offered := map[string]bool{}
	for _, d := range r.ConversationSkillDefs("postfach") {
		offered[d.Name] = true
	}
	if !offered[calendar.ToolCreate] || offered["inbox_enter"] {
		t.Fatalf("postfach gets %v", offered)
	}
	for _, s := range r.Specs(false) {
		if s.Name == calendar.ToolCreate {
			t.Fatal("calendar_create is offered without a skill")
		}
	}
	raw, err := os.ReadFile("../../skills/postfach/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if body := string(raw); !strings.Contains(body, calendar.ToolCreate) || strings.Contains(body, "inbox_enter") {
		t.Fatal("the skill postfach does not name calendar_create, or still names inbox_enter")
	}
}
