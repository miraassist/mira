package voicemail

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// box plays Mira FRITZ!Box: what it answers and what it was told.
type box struct {
	mu       sync.Mutex
	messages []Message
	calls    []Call
	missing  bool
	done     []string // actions without a response, with their data
	played   []string // speaker: media id
	shown    []string // title action: title | subtitle | source
	said     []string // what the satellite said between recordings
	// stopAt is the recording someone speaks into (1-based); 0: none.
	stopAt int
	waited int
	// The listening thread: its plan, ticked steps, announcements, holds.
	plan      string
	planText  string // the plan as the thread has it
	ticked    []string
	announced []string
	held      []bool
	stopped   []string // speakers told to stop
}

func (b *box) machine() *Machine {
	loc := time.FixedZone("CET", 3600)
	return New(Options{
		Lang: i18n.DE, Location: loc,
		Clock: func() time.Time { return time.Date(2026, 10, 29, 18, 0, 0, 0, loc) },
		Ask: func(_ context.Context, service string, data map[string]any) (json.RawMessage, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.missing {
				return nil, ErrNoIntegration
			}
			switch service {
			case "get_messages":
				out := b.messages
				if data["new_only"] == true {
					out = nil
					for _, m := range b.messages {
						if m.New {
							out = append(out, m)
						}
					}
				}
				return json.Marshal(map[string]any{"messages": out})
			case "get_calls":
				return json.Marshal(map[string]any{"calls": b.calls})
			}
			return nil, nil
		},
		Do: func(_ context.Context, service string, data map[string]any) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			raw, _ := json.Marshal(data)
			b.done = append(b.done, service+" "+string(raw))
			return nil
		},
		Satellite: func(_ context.Context, device string) Satellite {
			return Satellite{Speaker: "media_player.show_speaker", TitleAction: "esphome.show_media_title", Entity: "assist_satellite.show"}
		},
		AfterAnswer: func(_ string, fn func(ctx context.Context)) { fn(context.Background()) },
		Play: func(_ context.Context, speaker, id string) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.played = append(b.played, speaker+": "+id)
			return nil
		},
		Announce: func(_ context.Context, _, text string) bool {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.said = append(b.said, text)
			return true
		},
		Plan: func(_ context.Context, _ tools.Invocation, text string) (string, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.plan = text
			return "th1", nil
		},
		PlanThread: func(context.Context, tools.Invocation) string { return "th1" },
		PlanText: func(context.Context, tools.Invocation) string {
			b.mu.Lock()
			defer b.mu.Unlock()
			return b.planText
		},
		Tick: func(_ context.Context, thread, match string) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.ticked = append(b.ticked, thread+" "+match)
			return nil
		},
		Announced: func(_ context.Context, device, thread string) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.announced = append(b.announced, device+" "+thread)
		},
		Stop: func(_ context.Context, speaker string) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.stopped = append(b.stopped, speaker)
			return nil
		},
		Hold: func(_ context.Context, _ string, hold bool) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.held = append(b.held, hold)
		},
		Played: func(context.Context, string, time.Duration) bool {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.waited++
			return b.waited != b.stopAt
		},
		Title: func(_ context.Context, action, title, subtitle, source string) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.shown = append(b.shown, action+": "+title+" | "+subtitle+" | "+source)
			return nil
		},
	})
}

func oma() Message {
	return Message{ID: "0:13", Name: "Oma", Date: "2026-10-29T14:12:00+01:00", Minutes: 1, New: true,
		MediaContentID: "media-source://mira_fritz/e/0/13"}
}

func run(t *testing.T, m *Machine, name string, channel config.Channel, args string) string {
	t.Helper()
	for _, tool := range m.Tools() {
		if tool.Name == name {
			out, err := tool.Run(context.Background(), tools.Invocation{Channel: channel, DeviceID: "dev-show", Args: json.RawMessage(args)})
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

func TestAB01_TheListNamesMessagesAndMissedCallsApart(t *testing.T) {
	b := &box{messages: []Message{oma(), {ID: "0:14", Name: "Ben", Date: "2026-10-28T09:05:00+01:00", Minutes: 2, New: true}},
		calls: []Call{{Kind: "missed", Name: "Ben", Date: "2026-10-29T16:40:00+01:00"}, {Kind: "missed", Name: "Ben", Date: "2026-10-29T15:00:00+01:00"}}}
	out := run(t, b.machine(), ToolList, config.ChannelVoice, `{}`)
	for _, want := range []string{"Nachrichten (2):", "id 0:13: Oma, heute 14:12, bis 1 Min., neu", "Ben, gestern 09:05, bis 2 Min.", "Ben, 2-mal, zuletzt heute 16:40"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if len(b.done) != 1 || !strings.HasPrefix(b.done[0], "mark_calls_seen") {
		t.Errorf("seen: %v", b.done)
	}
}

func TestAB02_AMessagePlaysAtTheSatelliteOfTheTurnAndCountsAsHeard(t *testing.T) {
	b := &box{messages: []Message{oma()}}
	out := run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13"]}`)
	if !strings.Contains(out, "von Oma (heute 14:12)") || !strings.Contains(out, "keine Frage") {
		t.Errorf("out %q", out)
	}
	if len(b.played) != 1 || b.played[0] != "media_player.show_speaker: media-source://mira_fritz/e/0/13" {
		t.Errorf("played %v", b.played)
	}
	if len(b.done) != 1 || b.done[0] != `mark_heard {"message_ids":["0:13"]}` {
		t.Errorf("heard %v", b.done)
	}
}

func TestAB03_TheScreenShowsWhatPlays(t *testing.T) {
	b := &box{messages: []Message{oma()}}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13"]}`)
	// Its player screen names it (the device takes it away when it ends).
	if len(b.shown) != 1 || b.shown[0] != "esphome.show_media_title: Nachricht von Oma | heute 14:12 | Anrufbeantworter" {
		t.Errorf("shown %v", b.shown)
	}
	// Without the action (a Dot, older firmware) nothing is titled, and
	// it plays all the same.
	b = &box{messages: []Message{oma()}}
	m := b.machine()
	sat := m.o.Satellite
	m.o.Satellite = func(ctx context.Context, d string) Satellite { s := sat(ctx, d); s.TitleAction = ""; return s }
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13"]}`)
	if len(b.shown) != 0 || len(b.played) != 1 {
		t.Errorf("shown %v, played %v", b.shown, b.played)
	}
}

func TestAB04_PlayingOnlyAtASatellite(t *testing.T) {
	b := &box{messages: []Message{oma()}}
	out := run(t, b.machine(), ToolPlay, config.ChannelTelegram, `{"ids":["0:13"]}`)
	if len(b.played) != 0 || !strings.Contains(out, "nur an einem Satelliten") {
		t.Errorf("out %q, played %v", out, b.played)
	}
}

func TestAB05_WithoutMiraFritzBoxTheSkillIsAProblem(t *testing.T) {
	b := &box{missing: true}
	m := b.machine()
	if m.Probe(context.Background()) == nil || !m.Missing() {
		t.Fatal("not missing")
	}
	if !strings.Contains(m.MissingText(), "Mira FRITZ!Box fehlt") {
		t.Error(m.MissingText())
	}
	b.missing = false
	if m.Probe(context.Background()) != nil || m.Missing() {
		t.Error("still missing once it is there")
	}
}

func TestAB06_OfManyMessagesMiraNamesTheNewestThree(t *testing.T) {
	b := &box{}
	for i := 1; i <= 9; i++ {
		b.messages = append(b.messages, Message{ID: fmt.Sprintf("1:%d", i), Name: "Ben", Date: "2026-10-29T14:12:00+01:00", Minutes: 1, New: true})
	}
	out := run(t, b.machine(), ToolList, config.ChannelVoice, `{}`)
	if !strings.Contains(out, "nur die neuesten 3") || !strings.Contains(out, "dass es 6 weitere gibt") || !strings.Contains(out, "id 1:9:") {
		t.Errorf("out:\n%s", out)
	}
}

func TestAB07_WithSeveralAnsweringMachinesEachMessageNamesItsOwn(t *testing.T) {
	privat, business := oma(), oma()
	privat.Machine, business.Machine, business.ID = "Anrufbeantworter privat", "Anrufbeantworter business", "1:2"
	b := &box{messages: []Message{privat, business}}
	out := run(t, b.machine(), ToolList, config.ChannelVoice, `{}`)
	if !strings.Contains(out, "neu, Anrufbeantworter privat") || !strings.Contains(out, "neu, Anrufbeantworter business") {
		t.Errorf("out:\n%s", out)
	}
	b = &box{messages: []Message{privat}}
	if out := run(t, b.machine(), ToolList, config.ChannelVoice, `{}`); strings.Contains(out, "privat") {
		t.Errorf("one machine named:\n%s", out)
	}
}

func ben() Message {
	return Message{ID: "0:12", Name: "Ben", Date: "2026-10-28T09:05:00+01:00", Minutes: 1, New: true,
		MediaContentID: "media-source://mira_fritz/e/0/12"}
}

func TestAB08_SeveralMessagesPlayOneAfterTheOther(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	out := run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	if !strings.Contains(out, "2 Nachrichten") || !strings.Contains(out, "von Oma (heute 14:12)") {
		t.Errorf("out %q", out)
	}
	if len(b.played) != 2 || !strings.HasSuffix(b.played[1], "e/0/12") {
		t.Errorf("played %v", b.played)
	}
	if len(b.said) != 1 || b.said[0] != "Nächste: von Ben, gestern 09:05." {
		t.Errorf("said %v", b.said)
	}
	if len(b.done) != 2 {
		t.Errorf("heard %v", b.done)
	}
}

func TestAB09_SpeakingStopsTheRest(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}, stopAt: 1}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	if len(b.played) != 1 || len(b.said) != 0 || len(b.done) != 0 {
		t.Errorf("played %v, said %v, heard %v", b.played, b.said, b.done)
	}
}

func papa() Message {
	return Message{ID: "0:4", Name: "Papa", Date: "2026-01-19T16:33:00+01:00", Minutes: 1, New: false,
		MediaContentID: "media-source://mira_fritz/e/0/4"}
}

func TestAB30_AskingForMessagesStartsTheThreadWithThemAsItsPlan(t *testing.T) {
	b := &box{messages: []Message{oma(), ben(), papa()}}
	run(t, b.machine(), ToolList, config.ChannelVoice, `{}`)
	want := "# Anrufbeantworter\n- [ ] Oma, heute 14:12 (id 0:13)\n- [ ] Ben, gestern 09:05 (id 0:12)"
	if b.plan != want {
		t.Errorf("plan:\n%s", b.plan)
	}
	// Asked again after Oma's was heard: hers stays, ticked; Papa's old one
	// does not come in.
	b.messages[0].New = false
	b.planText = b.plan
	run(t, b.machine(), ToolList, config.ChannelVoice, `{"new_only":true}`)
	want = "# Anrufbeantworter\n- [x] Oma, heute 14:12 (id 0:13)\n- [ ] Ben, gestern 09:05 (id 0:12)"
	if b.plan != want {
		t.Errorf("plan asked again:\n%s", b.plan)
	}
}

func TestAB32_WhatPlayedToItsEndIsTicked(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	if len(b.ticked) != 2 || b.ticked[0] != "th1 (id 0:13)" || b.ticked[1] != "th1 (id 0:12)" {
		t.Errorf("ticked %v", b.ticked)
	}
	if len(b.held) != 2 || !b.held[0] || b.held[1] {
		t.Errorf("held %v", b.held)
	}
	// Speaking into the first: it is not ticked.
	b = &box{messages: []Message{oma(), ben()}, stopAt: 1}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	if len(b.ticked) != 0 {
		t.Errorf("ticked what was not heard to its end: %v", b.ticked)
	}
}

func TestAB33_TheNextIsTheThreadsAnnouncement(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	if len(b.announced) != 1 || b.announced[0] != "dev-show th1" {
		t.Errorf("announced %v", b.announced)
	}
}

func TestDeletingTicksTheStep(t *testing.T) {
	b := &box{messages: []Message{oma()}}
	run(t, b.machine(), ToolDelete, config.ChannelVoice, `{"id":"0:13"}`)
	if len(b.ticked) != 1 || b.ticked[0] != "th1 (id 0:13)" {
		t.Errorf("ticked %v", b.ticked)
	}
}

// A new voicemail_play at the device ends the run playing there before it
// plays anything: the replaced recording keeps the speaker "playing", and
// the old run would tick what it did not play to its end (AB-34).
func TestAB34_ANewPlayEndsTheRunningOne(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	m := b.machine()
	first := make(chan struct{})
	m.o.AfterAnswer = func(_ string, fn func(ctx context.Context)) { go fn(context.Background()) }
	calls := 0
	m.o.Played = func(ctx context.Context, _ string, _ time.Duration) bool {
		b.mu.Lock()
		calls++
		n := calls
		b.mu.Unlock()
		if n == 1 { // the first recording plays until it is replaced
			close(first)
			<-ctx.Done()
			return false
		}
		return true
	}
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	<-first
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:12"]}`) // "weiter"
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		ticked := slices.Clone(b.ticked)
		b.mu.Unlock()
		if len(ticked) > 0 || time.Now().After(deadline) {
			if len(ticked) != 1 || ticked[0] != "th1 (id 0:12)" {
				t.Errorf("ticked %v: the replaced one must not be", ticked)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// live is a machine whose recordings play until something ends or stops
// them, in the background as after an answer: playing[n] closes when the
// n-th recording starts.
func (b *box) live() (*Machine, func(n int) <-chan struct{}) {
	m := b.machine()
	m.o.AfterAnswer = func(_ string, fn func(ctx context.Context)) { go fn(context.Background()) }
	var mu sync.Mutex
	starts := map[int]chan struct{}{}
	at := func(n int) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if starts[n] == nil {
			starts[n] = make(chan struct{})
		}
		return starts[n]
	}
	stops := make(chan struct{}, 8)
	m.o.Stop = func(_ context.Context, speaker string) error {
		b.mu.Lock()
		b.stopped = append(b.stopped, speaker)
		b.mu.Unlock()
		stops <- struct{}{}
		return nil
	}
	n := 0
	m.o.Played = func(ctx context.Context, _ string, _ time.Duration) bool {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		close(at(k))
		select { // plays until its run ends or the speaker is stopped
		case <-ctx.Done():
			return false
		case <-stops:
			return false
		}
	}
	return m, func(n int) <-chan struct{} { return at(n) }
}

func (b *box) settle(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("not settled")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// "Abbrechen": the thread ends, and its run with it: the recording stops,
// nothing more plays, nothing is ticked (AB-39).
func TestAB39_EndingTheThreadStopsItsRecordings(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	m, started := b.live()
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	<-started(1)
	m.ThreadEnded(context.Background(), "th1")
	b.settle(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.stopped) == 1 })
	time.Sleep(50 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.played) != 1 || len(b.ticked) != 0 || b.stopped[0] != "media_player.show_speaker" {
		t.Errorf("played %v, ticked %v, stopped %v", b.played, b.ticked, b.stopped)
	}
}

// "Lösch die" while it plays: it stops, is deleted and ticked, and the run
// goes on with the next (AB-40).
func TestAB40_DeletingThePlayingOneGoesOnWithTheNext(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	m, started := b.live()
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	<-started(1)
	run(t, m, ToolDelete, config.ChannelVoice, `{"id":"0:13"}`)
	<-started(2) // Ben's plays
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.played) != 2 || !strings.HasSuffix(b.played[1], "e/0/12") || len(b.stopped) != 1 {
		t.Errorf("played %v, stopped %v", b.played, b.stopped)
	}
	if len(b.ticked) != 1 || b.ticked[0] != "th1 (id 0:13)" {
		t.Errorf("ticked %v", b.ticked)
	}
}

// "Nächste" or "noch einmal" while one plays: the new call stops the
// recording at once and ends the old run, which ticks nothing (AB-34).
func TestAB34_ANewPlayStopsTheRecordingAtOnce(t *testing.T) {
	b := &box{messages: []Message{oma(), ben()}}
	m, started := b.live()
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`)
	<-started(1)
	run(t, m, ToolPlay, config.ChannelVoice, `{"ids":["0:13","0:12"]}`) // "noch einmal"
	<-started(2)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.stopped) != 1 || len(b.ticked) != 0 || !strings.HasSuffix(b.played[1], "e/0/13") {
		t.Errorf("played %v, stopped %v, ticked %v", b.played, b.stopped, b.ticked)
	}
}
