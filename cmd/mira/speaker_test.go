package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/speakerid"
)

// Scenarios of docs/spec/sprecher.md.

// speakerApp is an app whose speaker-id service answers body after delay,
// with anna and ben as persons; ben is signed in to the HA app as u-ben.
func speakerApp(t *testing.T, body string, delay time.Duration) *app {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &app{
		cfg: config.Config{Persons: map[string]config.Person{
			"anna": {Name: "Anna"},
			"ben":  {Name: "Ben", UserID: "u-ben"},
		}},
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		speaker: speakerid.New(srv.URL, ""),
	}
}

const (
	annaSaidIt = `[{"person": "anna", "score": 0.81, "second": "ben", "second_score": 0.3}]`
	nobody     = ""
)

// voiceTurn is who the satellite turn is, with alone as the "alone at home"
// rule's answer.
func voiceTurn(a *app, alone string) string {
	person, _ := atSatellite(a.persons(), a.voiceOf(context.Background(), "Erinner mich an den Müll", a.persons()), func() string { return alone })
	return person
}

func TestSP01_RecognizedVoiceIsThePerson(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 0)
	if got := voiceTurn(a, nobody); got != "anna" {
		t.Errorf("got %q, want anna", got)
	}
}

func TestSP02_UncertainVoiceStaysAnonymous(t *testing.T) {
	a := speakerApp(t, `[{"person": "anna", "score": 0.8, "second": "ben", "second_score": 0.78}]`, 0)
	if got := voiceTurn(a, nobody); got != "" {
		t.Errorf("got %q, want nobody", got)
	}
	if got := voiceTurn(a, "ben"); got != "ben" {
		t.Errorf("with ben alone at home: got %q, want ben", got)
	}
}

func TestSP03_VoiceComesBeforeAloneAtHome(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 0)
	if got := voiceTurn(a, "ben"); got != "anna" {
		t.Errorf("got %q, want anna", got)
	}
}

func TestSP04_SignedInUserComesBeforeVoice(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 0)
	req := conversation.Request{Channel: config.ChannelVoice, HAUser: "u-ben", Text: "Erinner mich an den Müll"}
	if got, by := a.identify(context.Background(), req); got != "ben" || by != "" {
		t.Errorf("got %q by %q, want ben, signed in", got, by)
	}
}

func TestSP05_SlowServiceHoldsNothingUp(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 2*time.Second)
	start := time.Now()
	got := voiceTurn(a, "ben")
	if got != "ben" {
		t.Errorf("got %q, want the alone-at-home answer ben", got)
	}
	if took := time.Since(start); took > speakerid.Timeout+200*time.Millisecond {
		t.Errorf("the turn waited %v", took)
	}
}

func TestSP06_VoiceOfNobodyHomeAssistantKnows(t *testing.T) {
	a := speakerApp(t, `[{"person": "zoe", "score": 0.9}]`, 0)
	if got := voiceTurn(a, nobody); got != "" {
		t.Errorf("got %q, want nobody", got)
	}
}

func TestSP07_TwoSatellitesHeardTheSamePerson(t *testing.T) {
	a := speakerApp(t, `[{"person": "anna", "score": 0.81}, {"person": "anna", "score": 0.74}]`, 0)
	if got := voiceTurn(a, nobody); got != "anna" {
		t.Errorf("got %q, want anna", got)
	}
}

func TestSP08_TwoSatellitesDisagree(t *testing.T) {
	a := speakerApp(t, `[{"person": "anna", "score": 0.81}, {"person": "ben", "score": 0.9}]`, 0)
	if got := voiceTurn(a, "ben"); got != "ben" {
		t.Errorf("got %q, want the alone-at-home answer ben", got)
	}
}

// The score a voice needs is Mira's to set: speaker_id.threshold for
// everyone, voice_threshold for a person whose voice scores lower.
func TestSP09_ThresholdIsMirasPerPersonToo(t *testing.T) {
	a := speakerApp(t, `[{"person": "ben", "score": 0.62}]`, 0)
	if got := voiceTurn(a, nobody); got != "" {
		t.Errorf("at the default threshold: got %q, want nobody", got)
	}
	a.cfg.SpeakerID.Threshold = 0.6
	if got := voiceTurn(a, nobody); got != "ben" {
		t.Errorf("at speaker_id.threshold 0.6: got %q, want ben", got)
	}
	a.cfg.SpeakerID.Threshold = 0.8
	ben := a.cfg.Persons["ben"]
	ben.VoiceThreshold = 0.6
	a.cfg.Persons["ben"] = ben
	if got := voiceTurn(a, nobody); got != "ben" {
		t.Errorf("with ben's own 0.6 under a general 0.8: got %q, want ben", got)
	}
}

// A person Mira recognized (by the voice, or alone at home) is named when
// she does something personal for them; one who signed in is not.
func TestSP10_RecognizedPersonIsNamedWhenPersonal(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 0)
	persons := a.persons()
	voice := a.voiceOf(context.Background(), "Merk dir, dass ich gern Tee trinke", persons)
	for _, c := range []struct {
		name, voice, alone, wantBy, wantLine string
	}{
		{"by the voice", voice, "", prompt.ByVoice, "Es spricht: anna (an der Stimme erkannt)."},
		{"alone at home", "", "ben", prompt.ByAlone, "Es spricht: ben (allein zu Hause)."},
	} {
		person, by := atSatellite(persons, c.voice, func() string { return c.alone })
		if by != c.wantBy {
			t.Errorf("%s: by %q, want %q", c.name, by, c.wantBy)
		}
		line := prompt.Turn{Lang: i18n.DE, Person: person, PersonBy: by}.Render()
		if !strings.Contains(line, c.wantLine) || !strings.Contains(line, "nenn sie kurz beim Namen") ||
			!strings.Contains(line, "nicht in jeder Antwort") {
			t.Errorf("%s: %s", c.name, line)
		}
	}
	signedIn := prompt.Turn{Lang: i18n.DE, Person: "ben"}.Render()
	if !strings.Contains(signedIn, "Es spricht: ben. ") || strings.Contains(signedIn, "beim Namen") {
		t.Errorf("signed in: %s", signedIn)
	}
	en := prompt.Turn{Lang: i18n.EN, Person: "anna", PersonBy: prompt.ByVoice}.Render()
	if !strings.Contains(en, "Speaking: anna (recognized by their voice).") || !strings.Contains(en, "say their name briefly") {
		t.Errorf("english: %s", en)
	}
}
