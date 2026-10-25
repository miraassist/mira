package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
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
		speaker: speakerid.New(srv.URL),
	}
}

const (
	annaSaidIt = `{"person": "anna", "score": 0.71, "decision": "accepted", "count": 1}`
	nobody     = ""
)

// voiceTurn is who the satellite turn is, with alone as the "alone at home"
// rule's answer.
func voiceTurn(a *app, alone string) string {
	return atSatellite(a.persons(), a.voiceOf(context.Background(), "Erinner mich an den Müll"), func() string { return alone })
}

func TestSP01_RecognizedVoiceIsThePerson(t *testing.T) {
	a := speakerApp(t, annaSaidIt, 0)
	if got := voiceTurn(a, nobody); got != "anna" {
		t.Errorf("got %q, want anna", got)
	}
}

func TestSP02_UncertainVoiceStaysAnonymous(t *testing.T) {
	a := speakerApp(t, `{"person": null, "score": 0.55, "decision": "uncertain", "count": 1}`, 0)
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
	if got := a.identify(context.Background(), req); got != "ben" {
		t.Errorf("got %q, want ben", got)
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
	a := speakerApp(t, `{"person": "zoe", "score": 0.8, "decision": "accepted", "count": 1}`, 0)
	if got := voiceTurn(a, nobody); got != "" {
		t.Errorf("got %q, want nobody", got)
	}
}

// The service merges what several satellites heard (services/speaker-id,
// match); Mira takes its answer as it is.
func TestSP07_TwoSatellitesHeardTheSamePerson(t *testing.T) {
	a := speakerApp(t, `{"person": "anna", "score": 0.7, "decision": "accepted", "count": 2}`, 0)
	if got := voiceTurn(a, nobody); got != "anna" {
		t.Errorf("got %q, want anna", got)
	}
}

func TestSP08_TwoSatellitesDisagree(t *testing.T) {
	a := speakerApp(t, `{"person": null, "score": 0.7, "decision": "uncertain", "count": 2}`, 0)
	if got := voiceTurn(a, "ben"); got != "ben" {
		t.Errorf("got %q, want the alone-at-home answer ben", got)
	}
}
