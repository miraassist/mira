package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// MS-42 A skill that takes up a kind without its Ansage is a problem
// (docs/spec/messages.md, decision 30); the shipped skills have theirs.
func TestMS42_ASkillThatTakesUpAKindWithoutItsAnsageIsAProblem(t *testing.T) {
	enabled := map[string]map[string]any{}
	for _, kind := range store.KindsWithSkill {
		enabled[store.KindSkill(kind)] = nil
	}
	loaded, problems := skills.Load([]string{"../../skills"}, enabled)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	a := &app{lang: i18n.DE, skills: &skills.Set{Skills: loaded}}
	for _, kind := range store.KindsWithSkill {
		if text, err := a.kindInstruction(context.Background(), kind); err != nil || text == "" {
			t.Errorf("the shipped skill for %s: %q, %v", kind, text, err)
		}
	}
	if got := a.allProblems(); len(got) != 0 {
		t.Errorf("problems with the shipped skills: %+v", got)
	}

	// Given the skill geraet-einbinden has no section "Ansage"
	for i, sk := range loaded {
		if sk.Name == "geraet-einbinden" {
			loaded[i].Body = strings.Replace(sk.Body, "## Ansage", "## Etwas anderes", 1)
		}
	}
	_, err := a.kindInstruction(context.Background(), store.KindDevice)
	if !errors.Is(err, errNoAnnounce) || !strings.Contains(err.Error(), "SKILL.md") {
		t.Errorf("err = %v", err)
	}
	// Then GET /v1/status names the skill's problem with the missing section
	got := a.allProblems()
	if len(got) != 1 || got[0].Source != "skill" || got[0].Name != "geraet-einbinden" || !strings.Contains(got[0].Text, "„## Ansage“") {
		t.Errorf("problems = %+v", got)
	}
}
