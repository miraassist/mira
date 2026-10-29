package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/notifier"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/ui"
	"github.com/miraassist/mira/internal/voicemail"
)

// The answering machine at the satellite (docs/spec/anrufbeantworter.md):
// the FRITZ!Box's messages and missed calls from Mira FRITZ!Box, played at
// the satellite of the turn and shown on rings and screens.

// newVoicemail builds the answering machine on Home Assistant.
func (a *app) newVoicemail() *voicemail.Machine {
	call := func(err error) error {
		if errors.Is(err, haws.ErrNotFound) {
			return voicemail.ErrNoIntegration
		}
		return err
	}
	return voicemail.New(voicemail.Options{
		Lang: a.lang, Logger: a.log,
		Ask: func(ctx context.Context, service string, data map[string]any) (json.RawMessage, error) {
			raw, err := a.ha.CallServiceResponse(ctx, "mira_fritz", service, data)
			return raw, call(err)
		},
		Do: func(ctx context.Context, service string, data map[string]any) error {
			return call(a.ha.CallService(ctx, "mira_fritz", service, data, nil))
		},
		Satellite: a.voicemailSatellite,
		AfterAnswer: func(entity string, fn func(ctx context.Context)) {
			go a.afterAnswer(entity, fn)
		},
		Play: func(ctx context.Context, speaker, id string) error {
			return a.ha.CallService(ctx, "media_player", "play_media", map[string]any{
				"media_content_id": id, "media_content_type": "music",
			}, &haws.Target{EntityID: []string{speaker}})
		},
		Announce: a.announceNext,
		// Listening is a thread with a plan (step 4); the threads are built
		// after the tools, so they are looked up when used.
		Plan: func(ctx context.Context, inv tools.Invocation, text string) (string, error) {
			if a.threads == nil {
				return "", nil
			}
			return a.threads.SetPlan(ctx, inv, voicemail.Skill, voicemail.Skill, text)
		},
		PlanThread: func(ctx context.Context, inv tools.Invocation) string {
			if a.threads == nil {
				return ""
			}
			return a.threads.PlanThread(ctx, inv, voicemail.Skill)
		},
		PlanText: func(ctx context.Context, inv tools.Invocation) string {
			if a.threads == nil {
				return ""
			}
			return a.threads.PlanText(ctx, inv, voicemail.Skill)
		},
		Tick: func(ctx context.Context, threadID, match string) error {
			if a.threads == nil {
				return nil
			}
			return a.threads.TickPlan(ctx, threadID, match)
		},
		Announced: func(ctx context.Context, deviceID, threadID string) {
			if threadID == "" {
				return
			}
			err := a.store.RecordAnnouncement(ctx, store.Announcement{ID: ulid.Make().String(), DeviceID: deviceID,
				AreaID: a.structure.Snapshot().DeviceArea[deviceID], Channel: "voice", ThreadID: threadID})
			if err != nil {
				a.log.Warn("could not record the announcement of the next message", "error", err)
			}
		},
		Stop: func(ctx context.Context, speaker string) error {
			// Tried at the Show 2026-10-29: the recording stops, the speaker
			// goes "paused".
			return a.ha.CallService(ctx, "media_player", "media_stop", nil, &haws.Target{EntityID: []string{speaker}})
		},
		Hold: func(ctx context.Context, deviceID string, hold bool) {
			a.proactiveMu.Lock()
			n := a.notifier
			a.proactiveMu.Unlock()
			if n != nil {
				n.HoldPhoneScreen(ctx, a.structure.Snapshot().DeviceArea[deviceID], hold)
			}
		},
		Played: a.playedThrough,
		Counts: func(ctx context.Context) (voicemail.News, error) {
			messages, missed, _, err := a.phoneCounts(ctx)
			return voicemail.News{Messages: messages, Missed: missed}, err
		},
		Title: func(ctx context.Context, action, title, subtitle, source string) error {
			domain, service, _ := strings.Cut(action, ".")
			return a.ha.CallService(ctx, domain, service, map[string]any{
				"title": title, "subtitle": subtitle, "source": source,
			}, nil)
		},
	})
}

// voicemailOn says whether the skill anrufbeantworter is on and started:
// among the skills, or among the targets, since listening is a thread.
func (a *app) voicemailOn() bool {
	if _, ok := a.cfg.Skills.Enabled[voicemail.Skill]; !ok {
		return false
	}
	named := func(s skills.Skill) bool { return s.Name == voicemail.Skill }
	return (a.skills != nil && slices.ContainsFunc(a.skills.Skills, named)) || slices.ContainsFunc(a.targetSkills, named)
}

// phoneNews is what is new at the FRITZ!Box, for the notifier's indicators;
// nil without the skill.
func (a *app) phoneNews() func(ctx context.Context) (notifier.PhoneNews, error) {
	if !a.voicemailOn() {
		return nil
	}
	return func(ctx context.Context) (notifier.PhoneNews, error) {
		messages, missed, _, err := a.phoneCounts(ctx)
		return notifier.PhoneNews{Messages: messages, Missed: missed}, err
	}
}

// phoneSensors are Mira FRITZ!Box's two sensors, found by their unique ids
// (their entity ids follow Home Assistant's language), and their device.
func (a *app) phoneSensors() (messages, missed, device string) {
	for id, e := range a.structure.Snapshot().Entities {
		if e.Platform != "mira_fritz" {
			continue
		}
		switch {
		case strings.HasSuffix(e.UniqueID, "_new_messages"):
			messages, device = id, e.DeviceID
		case strings.HasSuffix(e.UniqueID, "_missed_calls"):
			missed, device = id, e.DeviceID
		}
	}
	return messages, missed, device
}

// phoneCounts are the two sensors' counts: from the watch on them while it
// runs (watchPhone), else read once, filtered to them. Never through Mira
// FRITZ!Box's actions, which ask the box.
func (a *app) phoneCounts(ctx context.Context) (messages, missed int, device string, err error) {
	msgID, missedID, device := a.phoneSensors()
	if msgID == "" && missedID == "" {
		return 0, 0, "", voicemail.ErrNoIntegration
	}
	count := func(s haws.State) int { n, _ := strconv.Atoi(s.State); return n }
	a.phoneMu.Lock()
	w := a.phoneWatch
	a.phoneMu.Unlock()
	if w != nil && w.Ready() {
		m, _ := w.State(msgID)
		c, _ := w.State(missedID)
		return count(m), count(c), device, nil
	}
	states, err := a.ha.GetStates(ctx, haws.StateFilter{EntityIDs: []string{msgID, missedID}})
	if err != nil {
		return 0, 0, device, err
	}
	for _, s := range states {
		if s.EntityID == msgID {
			messages = count(s)
		} else {
			missed = count(s)
		}
	}
	return messages, missed, device, nil
}

// voicemailSatellite is the satellite of a device: its speaker, the action
// that titles its player screen and its assist_satellite.
func (a *app) voicemailSatellite(ctx context.Context, deviceID string) voicemail.Satellite {
	sat := voicemail.Satellite{Speaker: a.speakerOf(ctx, deviceID), Entity: a.deviceEntity(deviceID, "assist_satellite")}
	if node := a.deviceNode(deviceID); node != "" && a.hasAction(ctx, "esphome", node+"_media_title", "title", "subtitle", "source") {
		sat.TitleAction = "esphome." + node + "_media_title"
	}
	return sat
}

// afterAnswer runs fn once the satellite has said Mira's answer and is out
// of the conversation: it waits for it to start speaking, then to be idle,
// watching its state. A satellite that does not speak within a while gets
// fn all the same (a safety net, not the signal).
func (a *app) afterAnswer(entity string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if entity != "" {
		a.ha.WaitState(ctx, entity, 15*time.Second, func(s string) bool { return s == "responding" })
		a.ha.WaitState(ctx, entity, time.Minute, func(s string) bool { return !inConversation(s) })
	}
	fn(ctx)
}

// inConversation says whether a satellite's state is a conversation:
// listening, thinking or speaking.
func inConversation(state string) bool {
	return state == "listening" || state == "processing" || state == "responding"
}

// playedThrough watches the satellite's speaker play a recording, by the
// speaker's own state (tried at the Show 2026-10-29): "playing" at once,
// "idle" when it played to its end, "paused" when it was stopped (by Mira,
// or by Home Assistant's pause on "Stopp"). A wake word only lowers it
// (the device ducks music for a turn), so it is no end. True once it played
// to its end; false when it did not start within a minute, was stopped, or
// limit passed. The minute and limit are safety nets, not the signal.
func (a *app) playedThrough(ctx context.Context, speaker string, limit time.Duration) bool {
	if speaker == "" {
		return false
	}
	states := make(chan string, 32)
	stop, err := a.ha.WatchState(ctx, speaker, func(s string) {
		select { // on the connection's read loop: only pass it on
		case states <- s:
		default:
		}
	})
	if err != nil {
		return false
	}
	defer stop()
	start, end := time.NewTimer(time.Minute), time.NewTimer(limit)
	defer start.Stop()
	defer end.Stop()
	started := false
	for {
		select {
		case s := <-states:
			switch {
			case s == "playing":
				started = true
			case !started:
				// What it was before this recording (paused after a "Stopp").
			case s == "idle":
				return true
			case s == "paused" || s == "off" || s == "unavailable":
				return false
			}
		case <-start.C:
			if !started {
				return false
			}
		case <-end.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// announceNext says text at a satellite as a plain announcement, without
// listening afterwards: an answer is the wake word's, at any time, and a
// wake word lowers the recordings (decided 2026-10-29). Only into a quiet
// satellite. Tried at the Show: "responding" while it speaks, then "idle",
// and the call returns as it ends; nothing listens after it.
func (a *app) announceNext(ctx context.Context, entity, text string) bool {
	if entity == "" {
		return false
	}
	if !a.ha.WaitState(ctx, entity, time.Minute, func(s string) bool { return !inConversation(s) }) {
		return false
	}
	err := a.ha.CallService(ctx, "assist_satellite", "announce", map[string]any{
		"message": text, "preannounce": false,
	}, &haws.Target{EntityID: []string{entity}})
	if err != nil {
		a.log.Warn("could not announce the next message", "error", err)
		return false
	}
	return true
}

// watchPhone keeps Mira FRITZ!Box's two sensors current (a filtered
// subscription) and updates the indicators as soon as one changes. While it
// runs, the counts are read from it, not asked for.
func (a *app) watchPhone(ctx context.Context, n *notifier.Notifier) {
	msgID, missedID, _ := a.phoneSensors()
	if msgID == "" && missedID == "" {
		return
	}
	// The handler runs on the connection's read loop: it only passes the
	// change on. Asking Home Assistant there would wait for answers that
	// loop has to read.
	changed := make(chan struct{}, 1)
	w, err := a.ha.WatchStates(ctx, haws.StateFilter{EntityIDs: []string{msgID, missedID}}, func() {
		select {
		case changed <- struct{}{}:
		default: // both sensors at once; one update is enough
		}
	})
	if err != nil {
		a.log.Warn("could not watch the answering machine", "error", err)
		return
	}
	defer w.Stop()
	a.phoneMu.Lock()
	a.phoneWatch = w
	a.phoneMu.Unlock()
	defer func() {
		a.phoneMu.Lock()
		if a.phoneWatch == w {
			a.phoneWatch = nil
		}
		a.phoneMu.Unlock()
	}()
	a.log.Info("watching the answering machine", "sensors", []string{msgID, missedID})
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			sync, done := context.WithTimeout(ctx, 30*time.Second)
			n.SyncIndicators(sync)
			done()
		}
	}
}

// phonePage is what waits on the answering machine for Mira's page: the
// counts of Mira FRITZ!Box's two sensors, and the box's web interface from
// its device in Home Assistant, where the messages play.
func (a *app) phonePage(ctx context.Context) (ui.PhoneNews, error) {
	var news ui.PhoneNews
	if !a.voicemailOn() {
		return news, nil
	}
	messages, missed, device, err := a.phoneCounts(ctx)
	if errors.Is(err, voicemail.ErrNoIntegration) {
		return news, nil
	}
	if err != nil {
		return news, err
	}
	news.Messages, news.Missed = messages, missed
	if messages+missed > 0 {
		news.URL = a.structure.Snapshot().Devices[device].ConfigurationURL
	}
	return news, nil
}
