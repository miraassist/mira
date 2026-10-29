package threads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// assignEnv has a target bound to the room (kochen) and one bound to the
// person (vokabeln), both with plans (docs/spec/vorgaenge.md, Assignment).
func assignEnv(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	kochen := skills.Skill{Name: "kochen", Description: "Kochen", Body: "Koch.", Target: &skills.Target{Plan: true, Bound: skills.BoundRoom}}
	vokabeln := skills.Skill{Name: "vokabeln", Description: "Vokabeln", Body: "Frag ab.", Target: &skills.Target{Plan: true}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{kochen, vokabeln}, Clock: func() time.Time { return e.now }})
	return e
}

// at is a voice turn in an area, at a place: "person:<key>" when the voice
// is recognised, the area's place when not (as the conversation decides).
func (e *env) at(t *testing.T, key, area, place string) (tools.Invocation, store.Session) {
	t.Helper()
	sess, err := e.st.UpsertSession(context.Background(), store.Session{Key: key, Channel: string(config.ChannelVoice),
		ConversationID: key, AreaID: area}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tools.Invocation{SessionKey: key, Channel: config.ChannelVoice, Area: area, Place: place}, sess
}

func (e *env) only(t *testing.T, target string) store.Thread {
	t.Helper()
	all, _ := e.st.OpenThreads(context.Background(), "")
	var out []store.Thread
	for _, th := range all {
		if th.Target == target {
			out = append(out, th)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%d threads of %s: %+v", len(out), target, out)
	}
	return out[0]
}

func TestAG110_ARoomsThreadIsTheRoomsWhoeverSpeaks(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	anna, _ := e.at(t, "voice:anna", "wohnzimmer", "person:anna")
	if _, err := e.m.Start(ctx, anna, "kochen", "Pfannkuchen"); err != nil {
		t.Fatal(err)
	}
	if th := e.only(t, "kochen"); th.Place != "area:wohnzimmer" {
		t.Fatalf("place %q", th.Place)
	}
	_, nobody := e.at(t, "voice:x", "wohnzimmer", "area:wohnzimmer")
	if text, _ := e.m.Context(ctx, nobody, "area:wohnzimmer", nil, ""); !strings.Contains(text, "kochen") {
		t.Errorf("unrecognised: %q", text)
	}
	ben, benSess := e.at(t, "voice:ben", "wohnzimmer", "person:ben")
	if text, _ := e.m.Context(ctx, benSess, "person:ben", nil, ""); !strings.Contains(text, "kochen") {
		t.Errorf("ben: %q", text)
	}
	if _, err := e.m.Start(ctx, ben, "kochen", "Pfannkuchen"); err != nil {
		t.Fatal(err)
	}
	e.only(t, "kochen")
}

func TestAG111_APersonsThreadFollowsThePerson(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	anna, _ := e.at(t, "voice:anna", "kueche", "person:anna")
	if _, err := e.m.Start(ctx, anna, "vokabeln", "Englisch"); err != nil {
		t.Fatal(err)
	}
	if th := e.only(t, "vokabeln"); th.Place != "person:anna" {
		t.Fatalf("place %q", th.Place)
	}
	_, there := e.at(t, "voice:anna2", "wohnzimmer", "person:anna")
	if text, _ := e.m.Context(ctx, there, "person:anna", nil, ""); !strings.Contains(text, "vokabeln") {
		t.Errorf("anna elsewhere: %q", text)
	}
	_, ben := e.at(t, "voice:ben", "kueche", "person:ben")
	if text, _ := e.m.Context(ctx, ben, "person:ben", nil, ""); text != "" {
		t.Errorf("ben sees anna's: %q", text)
	}
}

func TestAG112_ATurnSeesItsPersonsThreadsAndItsRooms(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	anna, _ := e.at(t, "voice:a1", "kueche", "person:anna")
	e.m.Start(ctx, anna, "vokabeln", "Englisch")
	e.now = e.now.Add(time.Minute) // the room's is the newer one
	room, _ := e.at(t, "voice:r", "wohnzimmer", "area:wohnzimmer")
	e.m.Start(ctx, room, "kochen", "Pfannkuchen")
	_, here := e.at(t, "voice:a2", "wohnzimmer", "person:anna")
	text, _ := e.m.Context(ctx, here, "person:anna", nil, "")
	if !strings.Contains(text, "vokabeln") || !strings.Contains(text, "kochen") || strings.Index(text, "kochen") > strings.Index(text, "vokabeln") {
		t.Errorf("context %q", text)
	}
}

func TestAG113_APersonsThreadStartedUnrecognisedBecomesTheirs(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	nobody, _ := e.at(t, "voice:x", "kueche", "area:kueche")
	e.m.Start(ctx, nobody, "vokabeln", "Englisch")
	if th := e.only(t, "vokabeln"); th.Place != "area:kueche" {
		t.Fatalf("place %q", th.Place)
	}
	anna, _ := e.at(t, "voice:anna", "kueche", "person:anna")
	if _, err := e.m.Plan(ctx, anna, "write", "- [ ] dog", 0); err != nil {
		t.Fatal(err)
	}
	if th := e.only(t, "vokabeln"); th.Place != "person:anna" || th.Person != "anna" {
		t.Errorf("not anna's: %q %q", th.Place, th.Person)
	}
	// A room's thread stays the room's, whoever goes on with it.
	e.m.Start(ctx, nobody, "kochen", "Pfannkuchen")
	ben, _ := e.at(t, "voice:ben", "kueche", "person:ben")
	if _, err := e.m.Plan(ctx, ben, "write", "- [ ] Teig", 0); err != nil {
		t.Fatal(err)
	}
	if th := e.only(t, "kochen"); th.Place != "area:kueche" {
		t.Errorf("room's thread moved: %q", th.Place)
	}
}

func TestAG114_TheDirectAnswerDoesNotReachAnotherPersonsThread(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	anna, _ := e.at(t, "voice:anna", "wohnzimmer", "person:anna")
	e.m.Start(ctx, anna, "vokabeln", "Englisch")
	id := e.only(t, "vokabeln").ID
	_, ben := e.at(t, "voice:ben", "wohnzimmer", "person:ben")
	if text, _ := e.m.Context(ctx, ben, "person:ben", nil, id); text != "" {
		t.Errorf("ben joined: %q", text)
	}
	_, nobody := e.at(t, "voice:x", "wohnzimmer", "area:wohnzimmer")
	if text, _ := e.m.Context(ctx, nobody, "area:wohnzimmer", nil, id); !strings.Contains(text, "Laufender Vorgang") {
		t.Errorf("unrecognised did not join: %q", text)
	}
}

func TestAG115_APrivateAreaKeepsItsThreads(t *testing.T) {
	e := assignEnv(t)
	ctx := context.Background()
	// In a private area the conversation places Anna's turn in the area.
	private, _ := e.at(t, "voice:p", "bad", "area:bad")
	e.m.Start(ctx, private, "vokabeln", "Englisch")
	if th := e.only(t, "vokabeln"); th.Place != "area:bad" {
		t.Fatalf("place %q", th.Place)
	}
	_, elsewhere := e.at(t, "voice:anna", "wohnzimmer", "person:anna")
	if text, _ := e.m.Context(ctx, elsewhere, "person:anna", nil, ""); text != "" {
		t.Errorf("seen outside: %q", text)
	}
}
