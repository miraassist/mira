// Package clip is clip_play (docs/spec/music-quiz.md): a short clip from the
// web, played at the satellite of the turn after Mira's answer, as music and
// under a title the skill gives, so its player screen does not tell what
// plays. Any skill that plays something short uses it.
package clip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Tool is the tool's name.
const Tool = "clip_play"

// limit ends the watch on a clip when no state comes: a safety net, not the
// signal. A clip is meant to be short; Mira knows no length of its own.
const limit = 10 * time.Minute

// Satellite is what clip_play needs of a satellite: its speaker, the action
// that titles its player screen ("" without one) and its assist_satellite.
type Satellite struct {
	Speaker, TitleAction, Entity string
}

// End is how a watched playback ended.
type End int

const (
	// Played: the speaker played and went idle at the end.
	Played End = iota
	// Stopped: the speaker went paused (Home Assistant's pause, Mira's stop).
	Stopped
	// NotStarted: it did not start playing within a while.
	NotStarted
	// RanOut: the watch's limit passed, or its run ended.
	RanOut
)

func (e End) String() string {
	return [...]string{"played", "stopped", "not started", "ran out"}[e]
}

type Options struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	// Satellite finds the satellite of a device.
	Satellite func(ctx context.Context, deviceID string) Satellite
	// AfterAnswer runs fn once the satellite has said Mira's answer.
	AfterAnswer func(entity string, fn func(ctx context.Context))
	// Title gives the next playback at a satellite its title, subtitle and
	// source for its player screen.
	Title func(ctx context.Context, action, title, subtitle, source string) error
	// Play plays an address on a speaker, as music.
	Play func(ctx context.Context, speaker, url string) error
	// Watch follows the speaker's state until the playback ends, or limit.
	Watch func(ctx context.Context, speaker string, limit time.Duration) (End, error)
	// Stop stops what a speaker plays (its state goes "paused").
	Stop func(ctx context.Context, speaker string) error
	// Thread is the open thread the turn belongs to, "" without one; Note
	// writes a line of Mira's into its log, which the next turn sees.
	Thread func(ctx context.Context, inv tools.Invocation) string
	Note   func(ctx context.Context, threadID, text string) error
}

// Player plays the clips; one at a time per satellite.
type Player struct {
	o   Options
	t   texts
	log *slog.Logger

	mu      sync.Mutex
	playing map[string]*run // by device
}

// run is one clip at a device: its thread, its speaker, what ends its wait
// or watch, and whether it plays now.
type run struct {
	thread, speaker string
	stop            context.CancelFunc
	sounding        bool
}

func New(o Options) *Player {
	p := &Player{o: o, t: i18n.Pick(catalog, o.Lang), log: o.Logger, playing: map[string]*run{}}
	if p.log == nil {
		p.log = slog.Default()
	}
	return p
}

// Tools is clip_play.
func (p *Player) Tools() []tools.InternalTool {
	t := p.t
	return []tools.InternalTool{{
		Name: Tool, Description: t.desc, PlaysAfter: true,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"url":{"type":"string","description":%q},"title":{"type":"string","description":%q},"subtitle":{"type":"string","description":%q}},"required":["url","title"]}`,
			t.argURL, t.argTitle, t.argSubtitle),
		Run: p.play,
	}}
}

func (p *Player) play(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct{ URL, Title, Subtitle string }
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return "", errors.New("url and title are required")
	}
	if u, err := url.Parse(strings.TrimSpace(args.URL)); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("url must be an http or https address")
	}
	t := p.t
	if inv.Channel != config.ChannelVoice || inv.DeviceID == "" {
		return t.notVoice, nil
	}
	sat := p.o.Satellite(ctx, inv.DeviceID)
	if sat.Speaker == "" {
		return t.noSpeaker, nil
	}
	thread := ""
	if p.o.Thread != nil {
		thread = p.o.Thread(ctx, inv)
	}
	device, address := inv.DeviceID, strings.TrimSpace(args.URL)
	// The clip takes over the satellite: one still playing there stops now.
	this := p.takeOver(ctx, device, thread, sat.Speaker)
	p.o.AfterAnswer(sat.Entity, func(ctx context.Context) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if !p.adopt(device, this, cancel) {
			return // already taken over by a newer clip
		}
		defer p.release(device, this)
		if sat.TitleAction != "" && p.o.Title != nil {
			if err := p.o.Title(ctx, sat.TitleAction, args.Title, args.Subtitle, t.source); err != nil {
				p.log.Warn("could not title the player", "error", err) // it plays all the same, untitled
			}
		}
		p.setSounding(this, true)
		if err := p.o.Play(ctx, sat.Speaker, address); err != nil {
			p.setSounding(this, false)
			p.log.Error("could not play a clip; the next turn is told", "host", hostOf(address), "speaker", sat.Speaker, "error", err)
			p.note(ctx, thread)
			return
		}
		end, err := p.o.Watch(ctx, sat.Speaker, limit)
		p.setSounding(this, false)
		switch {
		case err != nil:
			p.log.Warn("could not watch the clip play; its end is not known", "speaker", sat.Speaker, "error", err)
		case ctx.Err() != nil:
			// Ended by a newer clip or its thread, which stopped it.
		case end == NotStarted || end == RanOut:
			p.log.Warn("clip watch ran out", "speaker", sat.Speaker, "end", end.String())
		default:
			p.log.Info("clip played", "speaker", sat.Speaker, "end", end.String())
		}
	})
	return t.queued, nil
}

// note tells the turn's thread that its last clip did not play.
func (p *Player) note(ctx context.Context, thread string) {
	if thread == "" || p.o.Note == nil {
		return
	}
	if err := p.o.Note(context.WithoutCancel(ctx), thread, p.t.failed); err != nil {
		p.log.Warn("could not note the failed clip in its thread", "thread", thread, "error", err)
	}
}

// hostOf is an address's host, for the log: the rest may carry a token.
func hostOf(address string) string {
	if u, err := url.Parse(address); err == nil {
		return u.Host
	}
	return ""
}

// takeOver ends the clip at the device, if any, stopping it, and makes a new
// one the device's.
func (p *Player) takeOver(ctx context.Context, device, thread, speaker string) *run {
	p.mu.Lock()
	old := p.playing[device]
	r := &run{thread: thread, speaker: speaker}
	p.playing[device] = r
	p.mu.Unlock()
	p.end(ctx, old)
	return r
}

// end ends a clip's run: its wait or watch ends, and what it plays stops.
func (p *Player) end(ctx context.Context, r *run) {
	if r == nil {
		return
	}
	p.mu.Lock()
	stop, sounding, speaker := r.stop, r.sounding, r.speaker
	r.sounding = false
	p.mu.Unlock()
	if stop != nil {
		stop()
	}
	if sounding && p.o.Stop != nil {
		if err := p.o.Stop(ctx, speaker); err != nil {
			p.log.Warn("could not stop the clip", "speaker", speaker, "error", err) // the run is over all the same
		}
	}
}

// Playing says whether a clip plays at a device now: from its start until
// the speaker says it ended, was stopped, or the watch ran out.
func (p *Player) Playing(device string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.playing[device]
	return r != nil && r.sounding
}

// StopThread ends the clips of a thread, which stop: when it ended, or when
// its plan is done (docs/spec/music-quiz.md, decision 9).
func (p *Player) StopThread(ctx context.Context, thread string) {
	if thread == "" {
		return
	}
	p.mu.Lock()
	var ending []*run
	for device, r := range p.playing {
		if r.thread == thread {
			ending = append(ending, r)
			delete(p.playing, device)
		}
	}
	p.mu.Unlock()
	for _, r := range ending {
		p.end(ctx, r)
	}
}

// adopt gives the run its stop, if it is still the device's.
func (p *Player) adopt(device string, r *run, stop context.CancelFunc) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing[device] != r {
		return false
	}
	r.stop = stop
	return true
}

func (p *Player) setSounding(r *run, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r.sounding = on
}

// release lets go of the device, if the run still has it.
func (p *Player) release(device string, r *run) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing[device] == r {
		delete(p.playing, device)
	}
}
