package clip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

const preview = "https://cdnt-preview.example/a.mp3?hdnea=exp=1~hmac=secret"

// show plays the satellite and Home Assistant: what it was told.
type show struct {
	mu      sync.Mutex
	untitle bool  // the satellite cannot title its player
	playErr error // what playing on the speaker returns
	shown   []string
	played  []string
	stopped []string
	notes   []string
	// started closes as a clip starts to play; it plays until stopped or
	// its run ends.
	started chan struct{}
	stops   chan struct{}
	log     bytes.Buffer
}

func (s *show) player() *Player {
	s.started, s.stops = make(chan struct{}, 8), make(chan struct{}, 8)
	return New(Options{
		Lang: i18n.DE, Logger: slog.New(slog.NewTextHandler(&s.log, nil)),
		Satellite: func(context.Context, string) Satellite {
			sat := Satellite{Speaker: "media_player.show_speaker", Entity: "assist_satellite.show", TitleAction: "esphome.show_media_title"}
			if s.untitle {
				sat.TitleAction = ""
			}
			return sat
		},
		AfterAnswer: func(_ string, fn func(ctx context.Context)) { go fn(context.Background()) },
		Title: func(_ context.Context, action, title, subtitle, source string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.shown = append(s.shown, action+": "+title+" | "+subtitle+" | "+source)
			return nil
		},
		Play: func(_ context.Context, speaker, url string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.playErr != nil {
				return s.playErr
			}
			s.played = append(s.played, speaker+": "+url)
			return nil
		},
		Watch: func(ctx context.Context, _ string, _ time.Duration) (End, error) {
			s.started <- struct{}{}
			select {
			case <-ctx.Done():
				return RanOut, nil
			case <-s.stops:
				return Stopped, nil
			}
		},
		Stop: func(_ context.Context, speaker string) error {
			s.mu.Lock()
			s.stopped = append(s.stopped, speaker)
			s.mu.Unlock()
			s.stops <- struct{}{}
			return nil
		},
		Thread: func(context.Context, tools.Invocation) string { return "th1" },
		Note: func(_ context.Context, thread, text string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.notes = append(s.notes, thread+": "+text)
			return nil
		},
	})
}

func (s *show) settle(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		ok := cond()
		s.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("not settled")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func call(p *Player, channel config.Channel, args string) (string, error) {
	return p.Tools()[0].Run(context.Background(), tools.Invocation{Channel: channel, DeviceID: "dev-show", Args: json.RawMessage(args)})
}

const quizArgs = `{"url":"` + preview + `","title":"Musik-Quiz","subtitle":"Titel 1 von 10"}`

// MQ-01: the tool returns at once; after the answer the player is titled and
// the clip plays as music; the tool is marked as playing after the answer,
// so the satellite does not listen after it.
func TestMQ01_AClipPlaysAfterTheAnswerTitledAsMusic(t *testing.T) {
	s := &show{}
	p := s.player()
	if !p.Tools()[0].PlaysAfter {
		t.Error("clip_play is not marked as playing after the answer")
	}
	out, err := call(p, config.ChannelVoice, quizArgs)
	if err != nil || !strings.Contains(out, "nach deiner Antwort") {
		t.Fatalf("out %q, %v", out, err)
	}
	<-s.started
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.shown) != 1 || s.shown[0] != "esphome.show_media_title: Musik-Quiz | Titel 1 von 10 | Mira" {
		t.Errorf("shown %v", s.shown)
	}
	if len(s.played) != 1 || s.played[0] != "media_player.show_speaker: "+preview {
		t.Errorf("played %v", s.played)
	}
}

// MQ-02: a satellite without the title action plays the clip untitled.
func TestMQ02_WithoutTheTitleActionTheClipPlaysUntitled(t *testing.T) {
	s := &show{untitle: true}
	p := s.player()
	if _, err := call(p, config.ChannelVoice, quizArgs); err != nil {
		t.Fatal(err)
	}
	<-s.started
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.shown) != 0 || len(s.played) != 1 {
		t.Errorf("shown %v, played %v", s.shown, s.played)
	}
}

// MQ-03: a newer clip stops the one playing; only the new one plays on.
func TestMQ03_ANewerClipTakesOverTheSatellite(t *testing.T) {
	s := &show{}
	p := s.player()
	call(p, config.ChannelVoice, quizArgs)
	<-s.started
	call(p, config.ChannelVoice, `{"url":"https://cdnt-preview.example/b.mp3","title":"Musik-Quiz","subtitle":"Titel 2 von 10"}`)
	<-s.started
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stopped) != 1 || len(s.played) != 2 || !strings.HasSuffix(s.played[1], "/b.mp3") {
		t.Errorf("played %v, stopped %v", s.played, s.stopped)
	}
}

// MQ-04: when the clip's thread ends, the clip stops.
func TestMQ04_TheClipStopsWithItsThread(t *testing.T) {
	s := &show{}
	p := s.player()
	call(p, config.ChannelVoice, quizArgs)
	<-s.started
	p.StopThread(context.Background(), "other")
	p.StopThread(context.Background(), "th1")
	s.settle(t, func() bool { return len(s.stopped) == 1 })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped[0] != "media_player.show_speaker" {
		t.Errorf("stopped %v", s.stopped)
	}
}

// MQ-05: outside a voice conversation at a satellite nothing plays.
func TestMQ05_OutsideASatelliteNothingPlays(t *testing.T) {
	s := &show{}
	p := s.player()
	out, err := call(p, config.ChannelTelegram, quizArgs)
	if err != nil || !strings.Contains(out, "nur an einem Satelliten") {
		t.Errorf("out %q, %v", out, err)
	}
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.played)+len(s.shown) != 0 {
		t.Errorf("played %v, shown %v", s.played, s.shown)
	}
}

// MQ-06: a failed play writes a log line, without the address's token, and
// a note into the thread, which the next turn sees.
func TestMQ06_AFailedPlayLeavesATraceAndReachesTheNextTurn(t *testing.T) {
	s := &show{playErr: errors.New("speaker unavailable")}
	p := s.player()
	if _, err := call(p, config.ChannelVoice, quizArgs); err != nil {
		t.Fatal(err)
	}
	s.settle(t, func() bool { return len(s.notes) == 1 })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notes[0] != "th1: Der letzte Clip ließ sich am Satelliten nicht abspielen." {
		t.Errorf("notes %v", s.notes)
	}
	log := s.log.String()
	if !strings.Contains(log, "could not play a clip") || !strings.Contains(log, "media_player.show_speaker") ||
		!strings.Contains(log, "speaker unavailable") || strings.Contains(log, "hmac") {
		t.Errorf("log %q", log)
	}
}

// MQ-07: an address that is not http or https is the tool's error.
func TestMQ07_AnAddressThatIsNotHTTPIsRefused(t *testing.T) {
	s := &show{}
	p := s.player()
	for _, u := range []string{"file:///etc/passwd", "media-source://x", "https://", ""} {
		if _, err := call(p, config.ChannelVoice, `{"url":"`+u+`","title":"Musik-Quiz"}`); err == nil {
			t.Errorf("%q: no error", u)
		}
	}
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.played) != 0 {
		t.Errorf("played %v", s.played)
	}
}

// Playing is true from the clip's start until it stopped (MQ-09).
func TestPlayingWhileTheClipSounds(t *testing.T) {
	s := &show{}
	p := s.player()
	if p.Playing("dev-show") {
		t.Fatal("playing before any clip")
	}
	call(p, config.ChannelVoice, quizArgs)
	<-s.started
	if !p.Playing("dev-show") || p.Playing("other") {
		t.Error("not playing at its device, or playing elsewhere")
	}
	s.stops <- struct{}{} // "Stopp": the speaker goes paused
	deadline := time.Now().Add(2 * time.Second)
	for p.Playing("dev-show") {
		if time.Now().After(deadline) {
			t.Fatal("still playing after it stopped")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
