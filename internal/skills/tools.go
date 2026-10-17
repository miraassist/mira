package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const (
	ToolSkillOpen = "skill_open"
)

// Set is the enabled skills with what their tools need.
type Set struct {
	Lang     i18n.Lang
	Skills   []Skill
	Clock    func() time.Time
	Location *time.Location
}

func (s *Set) clock() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s *Set) loc() *time.Location {
	if s.Location != nil {
		return s.Location
	}
	return time.Local
}

// Get returns a skill by name.
func (s *Set) Get(name string) (Skill, bool) {
	for _, sk := range s.Skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}

// Tools returns skill_open, which only reads. With no skill enabled there
// is none, and the prompt stays as it was.
func (s *Set) Tools() []tools.InternalTool {
	if len(s.Skills) == 0 {
		return nil
	}
	t := i18n.Pick(catalog, s.Lang)
	var list strings.Builder
	for _, sk := range s.Skills {
		fmt.Fprintf(&list, "\n- %s: %s", sk.Name, sk.Description)
	}
	out := []tools.InternalTool{{
		Name:        ToolSkillOpen,
		Description: t.openDesc + list.String(),
		Schema:      fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string","description":%q}},"required":["name"]}`, t.openName),
		ReadOnly:    true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", err
			}
			sk, ok := s.Get(strings.TrimSpace(a.Name))
			if !ok {
				return "", fmt.Errorf(t.noSkill, a.Name)
			}
			return sk.Body, nil
		},
	}}
	return out
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

type texts struct {
	openDesc, openName, noSkill string
	taskIntro, taskRules        string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		openDesc:  "Öffnet einen Skill: liefert seine Anleitung, die du dann befolgst. Öffne den passenden Skill, bevor du eine Frage aus seinem Bereich beantwortest. Verfügbare Skills:",
		openName:  "Name des Skills.",
		noSkill:   "keinen Skill %q gibt es nicht",
		taskIntro: "Geplante Aufgabe des Skills %q. Anleitung des Skills:\n\n",
		taskRules: `

Antworte am Ende nur mit einem JSON-Objekt:
- importance: 0 = nichts zu melden, 1 = nebenbei erwähnen, 2 = wichtig
- spoken: ein bis zwei kurze Sätze zum Vorlesen, ohne Markdown; leer bei importance 0
- details: optional die Langfassung
- expires_in_hours: wie lange die Nachricht relevant bleibt`,
	},
	i18n.EN: {
		openDesc:  "Opens a skill: returns its instructions, which you then follow. Open the matching skill before answering a question in its area. Available skills:",
		openName:  "Name of the skill.",
		noSkill:   "there is no skill %q",
		taskIntro: "Scheduled task of the skill %q. The skill's instructions:\n\n",
		taskRules: `

At the end, answer only with a JSON object:
- importance: 0 = nothing to report, 1 = mention in passing, 2 = important
- spoken: one or two short sentences to be read aloud, no Markdown; empty when importance is 0
- details: optionally the long form
- expires_in_hours: how long the message stays relevant`,
	},
}
