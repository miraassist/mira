// Package voicemail is the answering machine at the satellite
// (docs/spec/anrufbeantworter.md): the skill anrufbeantworter's tools, which
// read and play the FRITZ!Box's messages through the integration Mira
// FRITZ!Box, and what is new for the indicators.
package voicemail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Skill is the skill whose tools these are.
const Skill = "anrufbeantworter"

// Tool names.
const (
	ToolList   = "voicemail_list"
	ToolPlay   = "voicemail_play"
	ToolDelete = "voicemail_delete"
)

// ErrNoIntegration: Home Assistant has no Mira FRITZ!Box to ask.
var ErrNoIntegration = errors.New("the integration Mira FRITZ!Box is missing")

// Message is one message as get_messages gives it.
type Message struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Date           string `json:"date"`
	Minutes        int    `json:"minutes"`
	New            bool   `json:"new"`
	MediaContentID string `json:"media_content_id"`
	// Machine is the answering machine's name ("Anrufbeantworter privat");
	// an older Mira FRITZ!Box sends none.
	Machine string `json:"answering_machine_name"`
}

// told is how many messages Mira names; of more she says how many.
const told = 3

// Call is one call as get_calls gives it.
type Call struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Date string `json:"date"`
}

// News is what is new: messages not heard, missed calls not seen.
type News struct{ Messages, Missed int }

// Satellite is what the tools need of the satellite of a turn.
type Satellite struct {
	// Speaker is its media player; empty: it plays nothing.
	Speaker string
	// Overlay is its screen's overlay action (esphome.<node>_notify); empty
	// without a screen.
	Overlay string
	// Entity is its assist_satellite, watched to play after Mira's answer.
	Entity string
}

type Options struct {
	Lang     i18n.Lang
	Location *time.Location
	Clock    func() time.Time
	// Ask calls an action of Mira FRITZ!Box with a response; Do one without.
	Ask func(ctx context.Context, service string, data map[string]any) (json.RawMessage, error)
	Do  func(ctx context.Context, service string, data map[string]any) error
	// Satellite finds the satellite of a device.
	Satellite func(ctx context.Context, deviceID string) Satellite
	// AfterAnswer runs fn once the satellite has said Mira's answer (or
	// after a while, when it says nothing).
	AfterAnswer func(entity string, fn func(ctx context.Context))
	// Play plays a recording on a speaker, as music: a wake word lowers it
	// and Home Assistant's pause stops it (tried at the Show 2026-10-29).
	Play func(ctx context.Context, speaker, mediaContentID string) error
	// Announce says a text at a satellite as a plain announcement, without
	// listening afterwards (the wake word interrupts at any time), and
	// returns once it is said; false when it could not be said.
	Announce func(ctx context.Context, entity, text string) bool
	// Played watches the speaker play a recording, by its own state: true
	// once it played to its end; false when it never started, was stopped,
	// or limit (a safety net only) passed.
	Played func(ctx context.Context, speaker string, limit time.Duration) bool
	// Show shows a hint on a screen for d.
	Show func(ctx context.Context, action, text, icon string, d time.Duration) error

	// Listening is a thread with a plan (docs/spec/anrufbeantworter.md,
	// step 4). Plan writes the plan of the turn's listening thread, starting
	// it when none runs, and returns its id; PlanThread finds the turn's;
	// Tick ticks the step that names a message.
	Plan       func(ctx context.Context, inv tools.Invocation, text string) (string, error)
	PlanThread func(ctx context.Context, inv tools.Invocation) string
	// PlanText is the plan of the turn's listening thread, "" without one.
	PlanText func(ctx context.Context, inv tools.Invocation) string
	Tick     func(ctx context.Context, threadID, match string) error
	// Announced records "Nächste …" as the thread's announcement on the
	// device, so the answer there joins the thread.
	Announced func(ctx context.Context, deviceID, threadID string)
	// Hold keeps the answering machine's screen hint off the device while
	// it plays, and shows the count when hold is false again.
	Hold func(ctx context.Context, deviceID string, hold bool)
	// Stop stops what a speaker plays (its state goes "paused").
	Stop func(ctx context.Context, speaker string) error
}

// step is how a message appears in the plan, and how its step is found.
func step(id string) string { return "(id " + id + ")" }

// Machine is the answering machine as Mira sees it.
type Machine struct {
	o Options
	t texts

	mu      sync.Mutex
	missing bool
	// playing is the run of recordings each device plays now: the
	// listening thread's doing at that satellite. A new voicemail_play there
	// ends it first, as does the end of its thread.
	playing map[string]*playRun
	// deleted are the messages deleted while a run may still hold them.
	deleted map[string]bool
}

// playRun is one voicemail_play's recordings at a device, on behalf of
// its listening thread.
type playRun struct {
	stop    context.CancelFunc
	thread  string
	speaker string
	// current is the message playing now, "" between recordings.
	current string
}

func New(o Options) *Machine {
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	return &Machine{o: o, t: i18n.Pick(catalog, o.Lang)}
}

// Missing says whether the last call found no Mira FRITZ!Box.
func (m *Machine) Missing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.missing
}

// MissingText is the skill's problem without the integration.
func (m *Machine) MissingText() string { return m.t.missing }

func (m *Machine) ask(ctx context.Context, service string, data map[string]any, into any) error {
	raw, err := m.o.Ask(ctx, service, data)
	m.mu.Lock()
	m.missing = errors.Is(err, ErrNoIntegration)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func (m *Machine) messages(ctx context.Context, newOnly bool) ([]Message, error) {
	var out struct{ Messages []Message }
	err := m.ask(ctx, "get_messages", map[string]any{"new_only": newOnly}, &out)
	return out.Messages, err
}

func (m *Machine) missed(ctx context.Context) ([]Call, error) {
	var out struct{ Calls []Call }
	err := m.ask(ctx, "get_calls", map[string]any{"kind": "missed", "new_only": true}, &out)
	return out.Calls, err
}

// Probe asks the integration once, so a missing one shows as a problem.
func (m *Machine) Probe(ctx context.Context) error {
	_, err := m.messages(ctx, true)
	return err
}

// when is a date as Mira says it: "heute 14:12", "gestern 9:05", "27.10. 18:30".
func (m *Machine) when(date string) string {
	d, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return date
	}
	d = d.In(m.o.Location)
	now := m.o.Clock().In(m.o.Location)
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	switch day(d) {
	case day(now):
		return fmt.Sprintf(m.t.today, d.Format("15:04"))
	case day(now.AddDate(0, 0, -1)):
		return fmt.Sprintf(m.t.yesterday, d.Format("15:04"))
	}
	return d.Format("02.01. 15:04")
}

// Tools are the skill's tools.
func (m *Machine) Tools() []tools.InternalTool {
	t := m.t
	id := fmt.Sprintf(`"id":{"type":"string","description":%q}`, t.argID)
	return []tools.InternalTool{
		{Name: ToolList, Description: t.listDesc, ReadOnly: true, Skill: Skill,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"new_only":{"type":"boolean","description":%q}},"required":[]}`, t.argNew),
			Run:    m.list},
		{Name: ToolPlay, Description: t.playDesc, Skill: Skill, PlaysAfter: true,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"ids":{"type":"array","items":{"type":"string"},"description":%q}},"required":["ids"]}`, t.argIDs),
			Run:    m.play},
		{Name: ToolDelete, Description: t.deleteDesc, Skill: Skill,
			Schema: `{"type":"object","properties":{` + id + `},"required":["id"]}`, Run: m.delete},
	}
}

func (m *Machine) list(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct {
		NewOnly bool `json:"new_only"`
	}
	_ = json.Unmarshal(inv.Args, &args)
	all, err := m.messages(ctx, false)
	if err != nil {
		return m.failed(err), nil
	}
	msgs := all
	if args.NewOnly {
		msgs = slices.DeleteFunc(slices.Clone(all), func(x Message) bool { return !x.New })
	}
	calls, err := m.missed(ctx)
	if err != nil {
		return m.failed(err), nil
	}
	t := m.t
	if m.o.Plan != nil {
		if planned := m.planned(ctx, inv, all); len(planned) > 0 {
			if _, err := m.o.Plan(ctx, inv, m.plan(planned)); err != nil {
				return "", err
			}
		}
	}
	var b strings.Builder
	if len(msgs) == 0 {
		b.WriteString(t.noMessages)
	} else {
		fmt.Fprintf(&b, t.messagesHead, len(msgs))
		if len(msgs) > told {
			fmt.Fprintf(&b, t.onlyNewest, told, len(msgs)-told)
		}
		// The answering machine's name only where there are several.
		machines := map[string]bool{}
		for _, x := range msgs {
			machines[x.Machine] = true
		}
		for _, x := range msgs {
			state := t.heard
			if x.New {
				state = t.isNew
			}
			fmt.Fprintf(&b, "\n- id %s: %s, %s, %s, %s", x.ID, x.Name, m.when(x.Date), fmt.Sprintf(t.minutes, max(x.Minutes, 1)), state)
			if len(machines) > 1 && x.Machine != "" {
				fmt.Fprintf(&b, ", %s", x.Machine)
			}
		}
	}
	b.WriteString("\n")
	if len(calls) == 0 {
		b.WriteString(t.noMissed)
	} else {
		// By caller: "Ben, 2 times, last today 16:40".
		type caller struct {
			count int
			last  string
		}
		var order []string
		by := map[string]*caller{}
		for _, c := range calls {
			if by[c.Name] == nil {
				by[c.Name] = &caller{last: c.Date}
				order = append(order, c.Name)
			}
			by[c.Name].count++
		}
		b.WriteString(t.missedHead)
		for _, name := range order {
			fmt.Fprintf(&b, "\n- "+t.missedLine, name, by[name].count, m.when(by[name].last))
		}
		// Said now: seen.
		if err := m.o.Do(ctx, "mark_calls_seen", nil); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// plan is the listening thread's plan: the messages newest first, the
// heard ones ticked, each with its id, and the answering machine where there
// are several.
func (m *Machine) plan(msgs []Message) string {
	machines := map[string]bool{}
	for _, x := range msgs {
		machines[x.Machine] = true
	}
	var b strings.Builder
	b.WriteString(m.t.planHead)
	for _, x := range msgs {
		box := " "
		if !x.New {
			box = "x"
		}
		fmt.Fprintf(&b, "\n- [%s] %s, %s", box, x.Name, m.when(x.Date))
		if len(machines) > 1 && x.Machine != "" {
			fmt.Fprintf(&b, ", %s", x.Machine)
		}
		fmt.Fprintf(&b, " %s", step(x.ID))
	}
	return b.String()
}

// inPlan finds the messages a plan names.
var inPlan = regexp.MustCompile(`\(id ([^)]+)\)`)

// planned are the messages the listening plan holds: the new ones, and
// those it held already (ticked once heard), never the whole history.
// Asking again keeps what was heard in this thread and adds what came.
func (m *Machine) planned(ctx context.Context, inv tools.Invocation, all []Message) []Message {
	had := map[string]bool{}
	if m.o.PlanText != nil {
		for _, match := range inPlan.FindAllStringSubmatch(m.o.PlanText(ctx, inv), -1) {
			had[match[1]] = true
		}
	}
	var out []Message
	for _, x := range all {
		if x.New || had[x.ID] {
			out = append(out, x)
		}
	}
	return out
}

// tick ticks a message's step in the listening thread, if one runs.
func (m *Machine) tick(ctx context.Context, threadID, id string) {
	if threadID == "" || m.o.Tick == nil {
		return
	}
	_ = m.o.Tick(ctx, threadID, step(id))
}

func (m *Machine) play(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct{ IDs []string }
	if err := json.Unmarshal(inv.Args, &args); err != nil || len(args.IDs) == 0 {
		return "", errors.New("ids is required")
	}
	t := m.t
	if inv.Channel != config.ChannelVoice || inv.DeviceID == "" {
		return t.notVoice, nil
	}
	sat := m.o.Satellite(ctx, inv.DeviceID)
	if sat.Speaker == "" {
		return t.noSpeaker, nil
	}
	msgs, err := m.messages(ctx, false)
	if err != nil {
		return m.failed(err), nil
	}
	// One after the other, in the order asked for.
	type playing struct {
		msg       Message
		who, when string
	}
	var queue []playing
	for _, id := range args.IDs {
		i := slices.IndexFunc(msgs, func(x Message) bool { return x.ID == id })
		if i < 0 {
			return fmt.Sprintf(t.unknown, id), nil
		}
		queue = append(queue, playing{msg: msgs[i], who: msgs[i].Name, when: m.when(msgs[i].Date)})
	}
	threadID := ""
	if m.o.PlanThread != nil {
		threadID = m.o.PlanThread(ctx, inv)
	}
	device := inv.DeviceID
	// This run takes over the device: one still playing there ends now and
	// its recording stops, before anything of this one plays.
	this := m.takeOver(ctx, device, threadID, sat.Speaker)
	m.o.AfterAnswer(sat.Entity, func(ctx context.Context) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if !m.adopt(device, this, cancel) {
			return // already taken over by a newer run
		}
		defer m.release(device, this)
		if m.o.Hold != nil {
			m.o.Hold(ctx, device, true)
			defer func() {
				if m.current(device, this) { // a newer run keeps holding
					m.o.Hold(context.WithoutCancel(ctx), device, false)
				}
			}()
		}
		for i, p := range queue {
			if m.isDeleted(p.msg.ID) {
				continue // deleted while it waited its turn
			}
			// The box's minutes are rounded up: the recording is no longer.
			// Only a safety net; the satellite's state says when it ends.
			limit := time.Duration(max(p.msg.Minutes, 1))*time.Minute + 30*time.Second
			// Mira's answer names the first; the satellite each further one.
			if i > 0 {
				// The thread's announcement: a turn there soon after joins
				// the thread.
				if m.o.Announced != nil {
					m.o.Announced(ctx, device, threadID)
				}
				if !m.o.Announce(ctx, sat.Entity, fmt.Sprintf(t.next, p.who, p.when)) || ctx.Err() != nil {
					return
				}
			}
			text := fmt.Sprintf(t.screen, p.who, p.when)
			if sat.Overlay != "" {
				// Seen while it plays: playing shows nothing by itself. It
				// goes when the recording ends (below).
				if err := m.o.Show(ctx, sat.Overlay, text, "mdi:voicemail", limit); err != nil {
					_ = err // the recording plays all the same
				}
			}
			m.setCurrent(this, p.msg.ID)
			if err := m.o.Play(ctx, sat.Speaker, p.msg.MediaContentID); err != nil {
				return
			}
			// Played to its end, by the speaker's state; stopped, the rest
			// does not play and what was not heard to the end stays new.
			played := m.o.Played(ctx, sat.Speaker, limit)
			m.setCurrent(this, "")
			if sat.Overlay != "" {
				_ = m.o.Show(ctx, sat.Overlay, text, "mdi:voicemail", time.Second)
			}
			if ctx.Err() != nil {
				return
			}
			if !played {
				if m.isDeleted(p.msg.ID) {
					continue // stopped because it was deleted: on to the next
				}
				return
			}
			_ = m.o.Do(ctx, "mark_heard", map[string]any{"message_ids": []string{p.msg.ID}})
			m.tick(ctx, threadID, p.msg.ID)
		}
	})
	first := queue[0]
	if len(queue) == 1 {
		return fmt.Sprintf(t.playing, first.who, first.when), nil
	}
	return fmt.Sprintf(t.playingMany, len(queue), first.who, first.when), nil
}

// takeOver ends the run playing at the device, if any, stopping its
// recording, and makes a new one the device's.
func (m *Machine) takeOver(ctx context.Context, device, thread, speaker string) *playRun {
	m.mu.Lock()
	if m.playing == nil {
		m.playing = map[string]*playRun{}
	}
	old := m.playing[device]
	r := &playRun{thread: thread, speaker: speaker}
	m.playing[device] = r
	m.mu.Unlock()
	m.end(ctx, old)
	return r
}

// end ends a run: no more of it plays, and what it plays now stops.
func (m *Machine) end(ctx context.Context, r *playRun) {
	if r == nil {
		return
	}
	m.mu.Lock()
	stop, playing, speaker := r.stop, r.current != "", r.speaker
	r.current = ""
	m.mu.Unlock()
	if stop != nil {
		stop()
	}
	if playing && m.o.Stop != nil {
		if err := m.o.Stop(ctx, speaker); err != nil {
			_ = err // the run is over all the same
		}
	}
}

// ThreadEnded ends the runs of a listening thread that ended: cancelled
// ("Abbrechen"), expired or done. Its recordings stop with it.
func (m *Machine) ThreadEnded(ctx context.Context, thread string) {
	m.mu.Lock()
	var ending []*playRun
	for device, r := range m.playing {
		if r.thread == thread && thread != "" {
			ending = append(ending, r)
			delete(m.playing, device)
		}
	}
	m.mu.Unlock()
	for _, r := range ending {
		m.end(ctx, r)
	}
}

func (m *Machine) setCurrent(r *playRun, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.current = id
}

func (m *Machine) isDeleted(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleted[id]
}

// deletedNow notes a deleted message and stops it where it plays now: its
// run goes on with the next.
func (m *Machine) deletedNow(ctx context.Context, id string) {
	m.mu.Lock()
	if m.deleted == nil {
		m.deleted = map[string]bool{}
	}
	m.deleted[id] = true
	var speakers []string
	for _, r := range m.playing {
		if r.current == id {
			speakers = append(speakers, r.speaker)
		}
	}
	m.mu.Unlock()
	for _, sp := range speakers {
		if m.o.Stop != nil {
			_ = m.o.Stop(ctx, sp)
		}
	}
}

// adopt gives the device's run its stop, once it starts; false when a newer
// run took the device meanwhile.
func (m *Machine) adopt(device string, r *playRun, stop context.CancelFunc) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.playing[device] != r {
		return false
	}
	r.stop = stop
	return true
}

// current says whether r is still the device's run.
func (m *Machine) current(device string, r *playRun) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playing[device] == r
}

// release forgets r once it is done, unless a newer run took its place.
func (m *Machine) release(device string, r *playRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.playing[device] == r {
		delete(m.playing, device)
	}
}

func (m *Machine) delete(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct{ ID string }
	if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.ID) == "" {
		return "", errors.New("id is required")
	}
	if err := m.o.Do(ctx, "delete_messages", map[string]any{"message_ids": []string{args.ID}}); err != nil {
		return m.failed(err), nil
	}
	m.deletedNow(ctx, args.ID)
	if m.o.PlanThread != nil {
		m.tick(ctx, m.o.PlanThread(ctx, inv), args.ID)
	}
	return m.t.deleted, nil
}

// News is what is new now, for the indicators.
func (m *Machine) News(ctx context.Context) (News, error) {
	msgs, err := m.messages(ctx, true)
	if err != nil {
		return News{}, err
	}
	calls, err := m.missed(ctx)
	if err != nil {
		return News{}, err
	}
	return News{Messages: len(msgs), Missed: len(calls)}, nil
}

// NewsLine is what "Gibt es Neues?" adds about the answering machine; ""
// when nothing is new there.
func (m *Machine) NewsLine(ctx context.Context) string {
	news, err := m.News(ctx)
	if err != nil || news.Messages+news.Missed == 0 {
		return ""
	}
	return fmt.Sprintf(m.t.newsLine, news.Messages, news.Missed)
}

func (m *Machine) failed(err error) string {
	if errors.Is(err, ErrNoIntegration) {
		return m.t.missing
	}
	return fmt.Sprintf(m.t.boxFailed, err)
}
