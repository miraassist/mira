package main

import (
	"context"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clip"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// A recording counts as played when the speaker played and went idle, as
// its state says; paused (stopped, "Stopp") is no end, nor is the paused
// it may start from (AB-32).
func TestPlayedThroughFollowsTheSpeakersState(t *testing.T) {
	a, ha, _ := deliveryApp(t)
	speaker := "media_player.show_speaker"
	ctx := context.Background()
	run := func(from string, states ...string) bool {
		ha.SetStates([]haws.State{{EntityID: speaker, State: from}})
		done := make(chan bool, 1)
		go func() { done <- a.playedThrough(ctx, speaker, 5*time.Second) }()
		for ha.SubscriptionCount() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		for _, s := range states {
			ha.SetState(speaker, s, nil)
		}
		return <-done
	}
	if !run("paused", "playing", "idle") {
		t.Error("played to its end: not played")
	}
	if run("idle", "playing", "paused") {
		t.Error("stopped: counted as played")
	}
}

// "Nächste …" waits for a quiet satellite, then says it as a plain
// announcement: nothing listens after it.
func TestAnnounceNextIsPlainAndWaitsForQuiet(t *testing.T) {
	a, ha, _ := deliveryApp(t)
	sat := "assist_satellite.show"
	ha.SetStates([]haws.State{{EntityID: sat, State: "responding"}})
	done := make(chan bool, 1)
	go func() { done <- a.announceNext(context.Background(), sat, "Nächste: von Ben.") }()
	for ha.SubscriptionCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(ha.ServiceCalls()); n != 0 {
		t.Fatalf("announced into a speaking satellite: %d calls", n)
	}
	ha.SetState(sat, "idle", nil)
	if !<-done {
		t.Fatal("not said")
	}
	calls := ha.ServiceCalls()
	if len(calls) != 1 || calls[0].Domain+"."+calls[0].Service != "assist_satellite.announce" || calls[0].Data["message"] != "Nächste: von Ben." {
		t.Errorf("calls %+v", calls)
	}
}

// A clip's watch says how it ended, by the speaker's state
// (docs/spec/music-quiz.md, row 4 of the state table).
func TestPlaybackSaysHowItEnded(t *testing.T) {
	a, ha, _ := deliveryApp(t)
	speaker := "media_player.show_speaker"
	ctx := context.Background()
	run := func(from string, states ...string) clip.End {
		ha.SetStates([]haws.State{{EntityID: speaker, State: from}})
		type result struct {
			end clip.End
			err error
		}
		done := make(chan result, 1)
		go func() { end, err := a.playback(ctx, speaker, 5*time.Second); done <- result{end, err} }()
		for ha.SubscriptionCount() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		for _, s := range states {
			ha.SetState(speaker, s, nil)
		}
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.end
	}
	if end := run("idle", "playing", "idle"); end != clip.Played {
		t.Errorf("played to its end: %v", end)
	}
	if end := run("idle", "playing", "paused"); end != clip.Stopped {
		t.Errorf("stopped: %v", end)
	}
}
