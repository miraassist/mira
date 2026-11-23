// Package threads runs threads ("Vorgänge", docs/spec/threads.md): a
// concern that runs over several turns, such as planning a task with a
// question back and a yes. Home Assistant makes a new conversation of every
// announcement and every wake word; a thread holds them together.
//
// A thread works towards a target, defined in a skill: fields to fill, a
// check per field, and a finish (a building block of the core) that does
// the work. The code keeps the state and decides when the thread is
// complete; the model understands what the user says, fills the fields and
// words the questions.
package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miraassist/mira/internal/clock"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/trace"
)

// Result is what a finish's work yields.
type Result struct {
	// Question asks the user for something the fields leave open; the
	// thread waits for the answer.
	Question string
	// Proposal asks for a yes. Content is kept on the thread: for the
	// commit, or what was done, for a correction.
	Proposal string
	Content  json.RawMessage
	// Done ends the thread successfully; the text is for the user.
	Done string
	// Cancel ends the thread: it cannot be done; the text says why.
	Cancel string
	// cancelled: the work was cancelled with its thread (runWork); nothing
	// to apply.
	cancelled bool
}

// Finisher is a core building block that ends a thread.
type Finisher interface {
	// Async finishers work in the background (drafting takes long); the
	// result reaches the user as a message of the thread.
	Async() bool
	// Work runs once all fields are filled.
	Work(ctx context.Context, th store.Thread) (Result, error)
	// Commit runs after the user's yes, for targets that ask for one.
	Commit(ctx context.Context, th store.Thread) (string, error)
}

// Check validates one field value against the house and may return it
// completed (names resolved to ids); an error says what is wrong.
type Check func(ctx context.Context, value string) (string, error)

type Options struct {
	Lang      i18n.Lang
	Store     *store.Store
	Targets   []skills.Skill
	Checks    map[string]Check
	Finishers map[string]Finisher
	// Actions lets a skill finish with a Home Assistant action; without it
	// such targets cannot finish.
	Actions Actions
	// Hooks decide on a skill's action before it is called, as a call of
	// its own (docs/spec/hooks.md, Skill actions); without them nothing is
	// checked.
	Hooks func(ctx context.Context, a ActionCall) tools.Decision
	// Deliver brings a thread's question or proposal to whoever asked,
	// like a reminder: spoken in the area, otherwise privately.
	Deliver func(ctx context.Context, n store.Message, areaID string) error
	Persons func() map[string]config.Person
	// InlineWait is how long a turn waits for background work before it
	// says "I'll come back to you"; default 5s, within Home Assistant's
	// 15 seconds for a voice turn.
	InlineWait time.Duration
	// AgentWait is how long a voice turn that starts a thread waits for the
	// agents it starts, and one that answers a remote agent's question waits
	// for that agent, so their result comes in the same answer; default
	// 10s. A slower one comes as a message of the thread.
	AgentWait time.Duration
	// Run runs a thread's background work as a run of the agent runtime
	// (a job named after the finish, with the thread's id as its call id):
	// logged, limited and cancellable like every run. Without it the work
	// runs directly.
	Run func(ctx context.Context, id, name string, work func(ctx context.Context) error) error
	// Cancel ends the background work of a thread that ended meanwhile,
	// and the agents still filling its fields.
	Cancel func(id string) bool
	// Dispatch starts an agent that fills a field of th (or its plan), as
	// an asynchronous run with the route back into the thread, under the
	// call id the thread chose (agents.go). Without it no agent fills a
	// field.
	Dispatch func(ctx context.Context, th store.Thread, into, agent, callID string, input json.RawMessage) error
	// Asking is the open agent request of a call: how the remote agent is
	// named and its question (docs/spec/remote-agents.md, D); Answer passes
	// the owner's answer on to it. Without Answer there is no agent_answer.
	Asking func(callID string) (label, question string, ok bool)
	Answer func(ctx context.Context, callID, text string) error
	// RemoteFailure is, for a failed call of a remote agent, its name to
	// the user and what it said (docs/spec/remote-agents.md, decision
	// 22); ok false for another run.
	RemoteFailure func(rec store.AgentRun) (label, said string, ok bool)
	// AgentNames are the names of a target's remote agents as Mira names
	// them to the user, appended to its entry in thread_start's list
	// (docs/spec/remote-agents.md, decision 61); asked at every turn, so a
	// card read later shows from the next turn on.
	AgentNames func(skill string) []string
	// Ended is told when a thread ended (done, cancelled, expired), for what
	// runs on its behalf outside it: the answering machine's recordings stop
	// with their thread.
	Ended func(ctx context.Context, th store.Thread)
	// Record writes a thread's summary into a participant's own history as
	// one turn: the record and the answer that closes it
	// (docs/spec/thread-history.md). Without it nothing is written.
	Record func(ctx context.Context, key, record, ack string) error
	// CodeStarted are the targets whose threads only Mira's code starts
	// (a watch offer, docs/spec/device-watch.md, decision 57): thread_start
	// refuses them.
	CodeStarted []string
	// Done is told once when a thread's plan became done, every step
	// ticked, while the thread stays open for what comes after (a quiz's
	// clip stops then, docs/spec/music-quiz.md, decision 9).
	Done func(ctx context.Context, th store.Thread)
	// Clock is what the waits wait on and what says now; nil is the real
	// clock (docs/spec/tests.md).
	Clock  clock.Clock
	Logger *slog.Logger
}

type Manager struct {
	opts    Options
	log     *slog.Logger
	targets map[string]skills.Skill
	// waiting are the turns waiting for a thread's agents, by thread id:
	// a result that comes in time goes to them instead of a message.
	waitMu  sync.Mutex
	waiting map[string]*waiter
	// holders count the turns that hold a thread (Hold); ended is how a
	// thread ended while held, its summary waiting for the last holder
	// (docs/spec/thread-history.md, decision 9).
	holdMu  sync.Mutex
	holders map[string]int
	ended   map[string]string
}

func New(opts Options) *Manager {
	opts.Clock = clock.Or(opts.Clock)
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.InlineWait == 0 {
		opts.InlineWait = 5 * time.Second
	}
	if opts.AgentWait == 0 {
		opts.AgentWait = 10 * time.Second
	}
	m := &Manager{opts: opts, log: opts.Logger.With("component", "threads"), targets: map[string]skills.Skill{},
		waiting: map[string]*waiter{}, holders: map[string]int{}, ended: map[string]string{}}
	for _, sk := range opts.Targets {
		if sk.Target != nil {
			m.targets[sk.Name] = sk
		}
	}
	return m
}

// HasTargets reports whether any target is defined; without one there are
// no thread tools.
func (m *Manager) HasTargets() bool { return len(m.targets) > 0 }

// HasTarget reports whether the target is defined (its skill is on).
func (m *Manager) HasTarget(name string) bool {
	_, ok := m.targets[name]
	return ok
}

func (m *Manager) texts() texts { return i18n.Pick(catalog, m.opts.Lang) }

// lang is the turn's language, for the skill's texts (docs/spec/skill-languages.md);
// without a turn the language at the start.
func (m *Manager) lang(ctx context.Context) i18n.Lang {
	if l, ok := i18n.FromContext(ctx); ok {
		return l
	}
	return m.opts.Lang
}

// place is where a turn's threads live: what the conversation said it
// belongs to (inv.Place); without that, the satellite's area for voice, the
// conversation (a Telegram chat) otherwise. A thread lives at one place.
func (m *Manager) place(ctx context.Context, inv tools.Invocation) (place, area string) {
	if inv.Place != "" {
		if a, ok := strings.CutPrefix(inv.Place, "area:"); ok {
			return inv.Place, a
		}
		return inv.Place, ""
	}
	if inv.Channel == config.ChannelVoice {
		if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil && sess.AreaID != "" {
			return "area:" + sess.AreaID, sess.AreaID
		}
	}
	return inv.SessionKey, ""
}

// roomBound says whether a target's threads are bound to the room rather
// than the person (docs/spec/threads.md, Assignment 1).
func (m *Manager) roomBound(target string) bool {
	sk, ok := m.targets[target]
	return ok && sk.Target != nil && sk.Target.Bound == skills.BoundRoom
}

// startPlace is where a new thread of target is placed (Assignment 2): a
// room's in the turn's room, a person's at the turn's place (the person
// when known, else the room, a private area always).
func (m *Manager) startPlace(ctx context.Context, inv tools.Invocation, target string) (place, area string) {
	place, area = m.place(ctx, inv)
	if sk, ok := m.targets[target]; ok && sk.Target != nil && sk.Target.Bound == skills.BoundHouse {
		return store.PlaceHouse, area
	}
	if m.roomBound(target) {
		if a := areaOf(m.withTurnArea(ctx, inv)); a != "" {
			return "area:" + a, a
		}
	}
	return place, area
}

// visible are the places whose threads a turn sees (Assignment 3c): its own
// place, and its room.
func visible(place, area string) []string {
	out := []string{place}
	if area != "" && "area:"+area != place {
		out = append(out, "area:"+area)
	}
	return out
}

// turnPlaces are the places a tool call's turn sees.
func (m *Manager) turnPlaces(ctx context.Context, inv tools.Invocation) []string {
	place, _ := m.place(ctx, inv)
	return visible(place, areaOf(m.withTurnArea(ctx, inv)))
}

// openAt are the open threads at the places, newest first.
func (m *Manager) openAt(ctx context.Context, places []string) ([]store.Thread, error) {
	var all []store.Thread
	for _, p := range places {
		if p == "" {
			continue // an empty place would be every place
		}
		open, err := m.opts.Store.OpenThreads(ctx, p)
		if err != nil {
			return nil, err
		}
		all = append(all, open...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	return all, nil
}

// adopt makes a person's thread that started with nobody recognised, and so
// in the room, the person's who goes on with it: from then on it follows
// them (Assignment 3).
func (m *Manager) adopt(ctx context.Context, th store.Thread, turnPlace string) store.Thread {
	person, ok := strings.CutPrefix(turnPlace, "person:")
	if !ok || m.roomBound(th.Target) || !strings.HasPrefix(th.Place, "area:") {
		return th
	}
	cur, err := m.change(ctx, th.ID, func(t *store.Thread) error {
		t.Place, t.Person = turnPlace, person
		return nil
	})
	if err != nil {
		return th
	}
	m.log.Info("thread taken over by its person", "thread", th.ID, "place", turnPlace)
	return cur
}

// areaKey carries the area of the turn whose tool call runs a thread's work
// or commit: an action's satellite placeholders are that turn's.
type areaKey struct{}

// deviceKey carries the satellite's device of that turn.
type deviceKey struct{}

// withTurnArea is ctx carrying the satellite's area of the calling turn:
// the invocation's, else its place's, else its voice session's; and its
// satellite's device, the invocation's, else its voice session's.
func (m *Manager) withTurnArea(ctx context.Context, inv tools.Invocation) context.Context {
	area, device := inv.Area, inv.DeviceID
	if area == "" {
		_, area = m.place(ctx, inv)
	}
	if (area == "" || device == "") && inv.Channel == config.ChannelVoice {
		if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil {
			if area == "" {
				area = sess.AreaID
			}
			if device == "" {
				device = sess.DeviceID
			}
		}
	}
	if area == "" {
		return ctx
	}
	if device != "" {
		ctx = context.WithValue(ctx, deviceKey{}, device)
	}
	return context.WithValue(ctx, areaKey{}, area)
}

func areaOf(ctx context.Context) string {
	a, _ := ctx.Value(areaKey{}).(string)
	return a
}

// deviceOf is the satellite's device withTurnArea carries, "" for none.
func deviceOf(ctx context.Context) string {
	d, _ := ctx.Value(deviceKey{}).(string)
	return d
}

// link makes a turn belong to a thread: its thread, and its target skill
// open, so the turn has the skill's tools (docs/spec/threads.md,
// Assignment).
func (m *Manager) link(ctx context.Context, sessionKey string, th store.Thread) {
	if err := m.opts.Store.SetSessionThread(ctx, sessionKey, th.ID); err != nil {
		m.log.Warn("could not link the turn to its thread", "thread", th.ID, "error", err)
	}
	m.openSkill(ctx, sessionKey, th.Target)
	if !th.Deadline.IsZero() {
		// Taken up: from now on its type's timeout counts, as for every
		// thread (docs/spec/messages.md, decision 11).
		if _, err := m.change(ctx, th.ID, func(t *store.Thread) error {
			t.Deadline = time.Time{}
			m.extend(t)
			return nil
		}); err != nil {
			m.log.Warn("could not extend a thread taken up; it keeps its starter's expiry", "thread", th.ID,
				"expires", th.Deadline, "error", err)
		}
	}
}

// Join makes a session belong to a thread (a thread's history,
// docs/spec/thread-history.md): linked, with the thread's skill open.
func (m *Manager) Join(ctx context.Context, sessionKey, threadID string) error {
	th, err := m.opts.Store.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	m.link(ctx, sessionKey, th)
	return nil
}

// openSkill opens a thread's skill for a turn, so it has the skill's tools.
func (m *Manager) openSkill(ctx context.Context, sessionKey, skill string) {
	if err := m.opts.Store.AddSessionSkill(ctx, sessionKey, skill); err != nil {
		m.log.Warn("could not open the thread's skill for the turn", "skill", skill, "error", err)
	}
}

// current is the thread a tool call acts on: the turn's own, or else the
// newest open one at its place.
func (m *Manager) current(ctx context.Context, inv tools.Invocation) (store.Thread, error) {
	if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil && sess.ThreadID != "" {
		if th, err := m.opts.Store.GetThread(ctx, sess.ThreadID); err == nil && store.ThreadOpen(th.State) {
			return th, nil
		}
	}
	open, err := m.openAt(ctx, m.turnPlaces(ctx, inv))
	if err != nil {
		return store.Thread{}, err
	}
	if len(open) == 0 {
		return store.Thread{}, store.ErrNotFound
	}
	place, _ := m.place(ctx, inv)
	th := m.adopt(ctx, open[0], place)
	m.link(ctx, inv.SessionKey, th)
	// Linking may change it (a starter's expiry ends): its current version.
	if cur, err := m.opts.Store.GetThread(ctx, th.ID); err == nil {
		th = cur
	}
	return th, nil
}

func (m *Manager) save(ctx context.Context, th *store.Thread) error {
	m.extend(th)
	return m.opts.Store.PutThread(ctx, th)
}

// extend sets when a thread expires after being worked on now.
func (m *Manager) extend(th *store.Thread) {
	sk := m.targets[th.Target]
	was := th.ExpiresAt
	th.ExpiresAt = m.opts.Clock.Now().Add(sk.Target.TimeoutDuration())
	if !th.Deadline.IsZero() {
		// Its starter's expiry, until a turn takes it up (decision 11).
		th.ExpiresAt = th.Deadline
	}
	// Taking it up, or a step in it, never shortens it (docs/spec/messages.md,
	// decision 26) ...
	if was.After(th.ExpiresAt) {
		th.ExpiresAt = was
	}
	// ... only a done plan ends sooner.
	if sk.Target.Plan {
		if c := planOf(*th); c.DoneAt != nil {
			th.ExpiresAt = c.DoneAt.Add(sk.Target.DoneAfterDuration())
		}
	}
}

// change applies fn to the thread as it is now and writes it; when another
// run changed it in between, it reads it again and applies fn once more.
func (m *Manager) change(ctx context.Context, id string, fn func(*store.Thread) error) (store.Thread, error) {
	for try := 0; ; try++ {
		th, err := m.opts.Store.GetThread(ctx, id)
		if err != nil {
			return store.Thread{}, err
		}
		if err := fn(&th); err != nil {
			return store.Thread{}, err
		}
		err = m.opts.Store.PutThread(ctx, &th)
		if !errors.Is(err, store.ErrConflict) || try == maxChangeTries-1 {
			return th, err
		}
		// docs/spec/fallbacks.md, row 34.
		m.log.Info("a thread changed meanwhile; reading it again", "thread", id, "try", try+1)
	}
}

// maxChangeTries bounds the retries of a change that keeps meeting others.
const maxChangeTries = 5

func (m *Manager) logEntry(th *store.Thread, who, text string) {
	th.Log = append(th.Log, store.ThreadEntry{At: m.opts.Clock.Now(), Who: who, Text: text})
}

// Start opens a thread for a target at the turn's place. An older open
// thread with the same target there ends: the user starts over. A plan is
// different: it may be well under way, so the user decides whether to go on
// or start anew.
func (m *Manager) Start(ctx context.Context, inv tools.Invocation, target, request string) (string, error) {
	if contains(m.opts.CodeStarted, target) {
		m.log.Info("thread_start refused: Mira's code starts this target", "target", target)
		return "", fmt.Errorf(m.texts().codeStarted, target)
	}
	out, _, err := m.begin(ctx, inv, target, request)
	return out, err
}

// begin opens a thread for a target at the turn's place: what Start does,
// also for a start from Home Assistant (StartFromHA), which has no session.
func (m *Manager) begin(ctx context.Context, inv tools.Invocation, target, request string) (string, store.Thread, error) {
	sk, ok := m.targets[target]
	if !ok {
		return "", store.Thread{}, fmt.Errorf(m.texts().noTarget, target)
	}
	place, _ := m.startPlace(ctx, inv, target)
	if open, err := m.openAt(ctx, m.turnPlaces(ctx, inv)); err == nil {
		for _, old := range open {
			if old.Target != target {
				continue
			}
			if sk.Target.Plan {
				if inv.SessionKey != "" {
					m.link(ctx, inv.SessionKey, old)
				}
				return fmt.Sprintf(m.texts().planRunning, old.Request, m.planStatus(old)), old, nil
			}
			m.end(ctx, old, store.ThreadCancelled)
		}
	}
	turn := m.withTurnArea(ctx, inv)
	th := store.Thread{ID: strings.ToLower(ulid.Make().String()), Target: target, Place: place, Person: inv.Person,
		State: store.ThreadCollecting, Request: strings.TrimSpace(request), Fields: map[string]string{},
		Area: areaOf(turn), Device: deviceOf(turn)}
	m.logEntry(&th, "user", th.Request)
	launches := m.dispatch(&th)
	if err := m.save(ctx, &th); err != nil {
		return "", store.Thread{}, err
	}
	// At a satellite the user waits for the answer: a quick agent's result
	// comes in it rather than as a message afterwards.
	var result *waiter
	if len(launches) > 0 && inv.Channel == config.ChannelVoice {
		result = m.await(th.ID)
	}
	m.start(ctx, th, launches)
	if inv.SessionKey != "" {
		if err := m.opts.Store.SetSessionThread(ctx, inv.SessionKey, th.ID); err != nil {
			return "", store.Thread{}, err
		}
		m.openSkill(ctx, inv.SessionKey, th.Target)
	}
	m.log.Info("thread started", "thread", th.ID, "target", target, "place", place)
	trace.Add(ctx, trace.Thread, target, "started")
	var said string
	if result != nil {
		said = m.waitFor(ctx, th.ID, result)
		if cur, err := m.opts.Store.GetThread(ctx, th.ID); err == nil {
			th = cur
		}
		if _, _, _, _, asks := m.asking(th); asks {
			// Its remote agent asked back while the turn waited: the user
			// answers in the next turn, and the satellite listens for it
			// (docs/spec/remote-agents.md, decisions 41, 51).
			tools.TurnOf(ctx).AgentAsked(th.ID)
		}
	}
	out := m.texts().started + "\n\n" + sk.BodyIn(m.lang(ctx)) + "\n\n" + m.status(th)
	if said != "" {
		out += "\n\n" + said
	}
	return out, th, nil
}

// ErrWorkCancelled is what Options.Run returns for a work cancelled with its
// thread (Cancel): no failure (docs/spec/endings.md, EN-04).
var ErrWorkCancelled = errors.New("the thread's work was cancelled")

// waiter is a turn waiting for its thread's agents. A result is offered to
// it, and it takes it only while it still answers: a turn that ended (its
// caller gone, barge-in) or stopped waiting says no, and the result comes as
// a message (docs/spec/endings.md, EN-01).
type waiter struct {
	offers chan offer
	// gone is closed once the turn stopped waiting.
	gone chan struct{}
}

type offer struct {
	said string
	took chan bool
}

// await registers a turn waiting for th's agents.
func (m *Manager) await(id string) *waiter {
	w := &waiter{offers: make(chan offer), gone: make(chan struct{})}
	m.waitMu.Lock()
	m.waiting[id] = w
	m.waitMu.Unlock()
	return w
}

// unawait takes back a turn's wait registered with await, when it is still
// that one; offers after it are refused.
func (m *Manager) unawait(id string, w *waiter) {
	m.waitMu.Lock()
	if m.waiting[id] == w {
		delete(m.waiting, id)
	}
	m.waitMu.Unlock()
	select {
	case <-w.gone:
	default:
		close(w.gone)
	}
}

// waitFor waits up to AgentWait for an agent's result for thread id; what
// it would have said comes back. Past that, or once the turn ended, the
// result comes as a message.
func (m *Manager) waitFor(ctx context.Context, id string, w *waiter) string {
	timer := m.opts.Clock.NewTimer(m.opts.AgentWait)
	defer timer.Stop()
	defer m.unawait(id, w)
	select {
	case o := <-w.offers:
		if ctx.Err() != nil {
			o.took <- false // the turn ended meanwhile: nobody would hear it
			return ""
		}
		o.took <- true
		return o.said
	case <-timer.C():
	case <-ctx.Done():
	}
	return ""
}

// handOver offers an agent's result to the turn waiting for it; false when
// none waits or it no longer takes it, and the caller sends it as a
// message.
func (m *Manager) handOver(id, said string) bool {
	m.waitMu.Lock()
	w, ok := m.waiting[id]
	m.waitMu.Unlock()
	if !ok {
		return false
	}
	took := make(chan bool, 1)
	select {
	case w.offers <- offer{said: said, took: took}:
		return <-took
	case <-w.gone:
		return false
	}
}

// Fill takes field values from the model, checks them and, once all are
// there, runs the target's work.
func (m *Manager) Fill(ctx context.Context, inv tools.Invocation, values map[string]string) (string, error) {
	th, err := m.current(ctx, inv)
	if err != nil {
		return "", errors.New(m.texts().noThread)
	}
	return m.fill(ctx, inv, th, values)
}

// ConfirmThread takes up a thread by its id, fills the fields given and
// says yes, in one call (docs/spec/inbox.md, decision 34): one round per
// thing, and several such calls in one round for "ja, alle". It acts on the
// thread by its id, not on the turn's thread, so calls side by side do not
// meet. Without an id it is thread_confirm as before.
func (m *Manager) ConfirmWith(ctx context.Context, inv tools.Invocation, id string, values map[string]string) (string, error) {
	if id == "" {
		if len(values) > 0 {
			if out, err := m.Fill(ctx, inv, values); err != nil {
				return "", err
			} else if th, err := m.current(ctx, inv); err != nil || th.State != store.ThreadProposed {
				return out, nil
			}
		}
		return m.Confirm(ctx, inv)
	}
	th, err := m.opts.Store.GetThread(ctx, id)
	if err != nil || !store.ThreadOpen(th.State) || !contains(append(m.turnPlaces(ctx, inv), store.PlaceHouse), th.Place) {
		m.log.Info("thread_confirm refused: not open, or not the asker's", "thread", id, "error", err)
		return "", errors.New(m.texts().noThread)
	}
	if err := m.opts.Store.SetSessionThread(ctx, inv.SessionKey, th.ID); err != nil {
		m.log.Warn("could not link the turn to its thread", "thread", th.ID, "error", err)
	}
	if !th.Deadline.IsZero() {
		// Taken up: from now on its type's timeout counts (decision 11).
		if cur, err := m.change(ctx, th.ID, func(t *store.Thread) error { t.Deadline = time.Time{}; m.extend(t); return nil }); err == nil {
			th = cur
		}
	}
	if len(values) > 0 {
		out, err := m.fill(ctx, inv, th, values)
		if err != nil {
			return "", err
		}
		if th, err = m.opts.Store.GetThread(ctx, id); err != nil || th.State != store.ThreadProposed {
			return out, nil
		}
	}
	return m.confirm(m.withTurnArea(ctx, inv), th)
}

func (m *Manager) fill(ctx context.Context, inv tools.Invocation, th store.Thread, values map[string]string) (string, error) {
	sk := m.targets[th.Target]
	t := m.texts()
	if sk.Target.Plan {
		return "", errors.New(t.planNoFields)
	}
	// A check may decide by the thread's person (the calendar's one rule).
	person := th.Person
	if person == "" {
		person = inv.Person
	}
	ctx = tools.WithPerson(ctx, person)
	var problems []string
	for name, value := range values {
		value = strings.TrimSpace(value)
		f, ok := field(sk.Target, name)
		if !ok {
			problems = append(problems, fmt.Sprintf(t.unknownField, name))
			continue
		}
		if value == "" {
			continue
		}
		if len(f.Enum) > 0 && !contains(f.Enum, value) {
			problems = append(problems, fmt.Sprintf(t.notInEnum, name, strings.Join(f.Enum, ", ")))
			continue
		}
		if f.Check != "" && !notNeeded(value) {
			if check := m.opts.Checks[f.Check]; check != nil {
				checked, err := check(ctx, value)
				if err != nil {
					problems = append(problems, fmt.Sprintf(t.checkFailed, name, err))
					continue
				}
				value = checked
			}
		}
		th.Fields[name] = value
		m.logEntry(&th, "user", name+": "+value)
	}
	if th.State == store.ThreadWaiting || th.State == store.ThreadProposed {
		// An answer or a change: back to collecting; a proposal is moot.
		th.State = store.ThreadCollecting
		if _, err := m.opts.Store.WithdrawThreadMessages(ctx, th.ID); err != nil {
			// docs/spec/fallbacks.md, row 25.
			m.log.Warn("could not withdraw the messages of a thread's proposal; they may still be announced", "thread", th.ID, "error", err)
		}
	}
	if len(problems) > 0 || len(missing(sk.Target, th.Fields)) > 0 {
		// An agent whose input is there now starts.
		launches := m.dispatch(&th)
		if err := m.save(ctx, &th); err != nil {
			return "", err
		}
		m.start(ctx, th, launches)
		return strings.Join(append(problems, m.status(th)), "\n"), nil
	}
	return m.work(ctx, inv, th)
}

// work runs the finish once the fields are complete.
func (m *Manager) work(ctx context.Context, inv tools.Invocation, th store.Thread) (string, error) {
	ctx = m.withTurnArea(ctx, inv)
	fin, err := m.finisher(th.Target)
	if err != nil {
		return "", err
	}
	t := m.texts()
	if !fin.Async() {
		res, err := fin.Work(ctx, th)
		if err != nil {
			return "", err
		}
		return m.apply(ctx, th, res, false), nil
	}
	th.State = store.ThreadWorking
	if err := m.save(ctx, &th); err != nil {
		return "", err
	}
	// The work runs in the background, but the turn waits a moment for
	// it: a result that is there in time is said in this very answer, not
	// announced after an "I'll come back to you" (a fast model drafts in
	// about a second). Only a slow result is announced later.
	var (
		mu        sync.Mutex
		abandoned bool
		inTime    = make(chan Result, 1)
	)
	go func() {
		ctx := context.WithoutCancel(ctx)
		res := m.runWork(ctx, fin, th)
		mu.Lock()
		defer mu.Unlock()
		if !abandoned {
			inTime <- res
			return
		}
		// The user may have cancelled meanwhile.
		cur, err := m.opts.Store.GetThread(ctx, th.ID)
		if err != nil {
			// docs/spec/fallbacks.md, row 21.
			m.log.Error("a thread's work is done but the thread cannot be read again; its result is dropped", "thread", th.ID, "error", err)
			return
		}
		if cur.State != store.ThreadWorking {
			return
		}
		m.apply(ctx, th, res, true)
	}()
	timer := m.opts.Clock.NewTimer(m.opts.InlineWait)
	defer timer.Stop()
	select {
	case res := <-inTime:
		if res.cancelled {
			return t.cancelled, nil
		}
		return m.apply(ctx, th, res, false), nil
	case <-timer.C():
	}
	mu.Lock()
	defer mu.Unlock()
	select {
	case res := <-inTime: // arrived just now
		if res.cancelled {
			return t.cancelled, nil
		}
		return m.apply(ctx, th, res, false), nil
	default:
		abandoned = true
	}
	return t.working, nil
}

// runWork runs a thread's background work, as a job of the runtime when
// there is one; a failure ends the thread with that said.
func (m *Manager) runWork(ctx context.Context, fin Finisher, th store.Thread) Result {
	var res Result
	work := func(ctx context.Context) error {
		var err error
		res, err = fin.Work(ctx, th)
		return err
	}
	var err error
	if m.opts.Run != nil {
		err = m.opts.Run(ctx, th.ID, m.targets[th.Target].Target.Finish.Name, work)
	} else {
		err = work(ctx)
	}
	switch {
	case errors.Is(err, ErrWorkCancelled):
		// Its thread ended and cancelled it: no failure (docs/spec/endings.md,
		// EN-04); the thread is ended already.
		m.log.Info("thread work cancelled with its thread", "thread", th.ID)
		return Result{cancelled: true}
	case err != nil:
		m.log.Error("thread work failed", "thread", th.ID, "error", err)
		res = Result{Cancel: fmt.Sprintf(m.texts().workFailed, th.Request)}
	}
	return res
}

// Resume starts anew the background work of the threads that were working
// when the agent stopped: the work is not stored, and it changes nothing
// before the user's yes. Its result comes as a message of the thread, as
// for slow work.
func (m *Manager) Resume(ctx context.Context) {
	open, err := m.opts.Store.OpenThreads(ctx, "")
	if err != nil {
		m.log.Error("could not read the open threads", "error", err)
		return
	}
	for _, th := range open {
		if th.State != store.ThreadWorking {
			continue
		}
		fin, err := m.finisher(th.Target)
		if err != nil {
			m.log.Warn("a working thread cannot go on", "thread", th.ID, "error", err)
			continue
		}
		m.log.Info("thread work started again", "thread", th.ID, "target", th.Target)
		go func(th store.Thread) {
			// Not ended by a shutdown: a thread that is still working then
			// starts again after the next start.
			ctx := context.WithoutCancel(ctx)
			res := m.runWork(ctx, fin, th)
			if cur, err := m.opts.Store.GetThread(ctx, th.ID); err == nil && cur.State == store.ThreadWorking {
				m.apply(ctx, cur, res, true)
			}
		}(th)
	}
}

// apply records a work result and, for background work, tells the user.
// It returns what the model should say for foreground work.
func (m *Manager) apply(ctx context.Context, th store.Thread, res Result, tell bool) string {
	t := m.texts()
	say := spoken(res)
	if res.Content != nil {
		th.Content = res.Content
	}
	switch {
	case res.Question != "":
		th.State = store.ThreadWaiting
	case res.Proposal != "":
		th.State = store.ThreadProposed
	case res.Cancel != "":
		th.State = store.ThreadCancelled
	default:
		th.State = store.ThreadDone
	}
	m.logEntry(&th, "mira", say)
	if err := m.save(ctx, &th); err != nil {
		m.log.Error("could not save a thread", "thread", th.ID, "error", err)
	}
	m.log.Info("thread advanced", "thread", th.ID, "state", th.State)
	trace.Add(ctx, trace.Thread, th.Target, th.State)
	// A result that ends the thread ends it through the one place
	// (docs/spec/thread-history.md, step 1), before the result is told: the
	// end withdraws the thread's open messages, not this one.
	if !store.ThreadOpen(th.State) {
		m.end(ctx, th, th.State)
	}
	if tell && say != "" {
		m.tell(ctx, th, say)
	}
	if !tell {
		switch th.State {
		case store.ThreadWaiting:
			return fmt.Sprintf(t.askUser, say)
		case store.ThreadProposed:
			return fmt.Sprintf(t.proposeUser, say)
		}
	}
	return say
}

// spoken is what a finish's result says to the user.
func spoken(res Result) string {
	switch {
	case res.Question != "":
		return res.Question
	case res.Proposal != "":
		return res.Proposal
	case res.Cancel != "":
		return res.Cancel
	}
	return res.Done
}

// tell delivers a thread's question or proposal as a message of the thread:
// a reply to it continues the thread.
func (m *Manager) tell(ctx context.Context, th store.Thread, text string) {
	if m.opts.Deliver == nil {
		return
	}
	// Spoken where the thread began, even when it belongs to a person:
	// that is where they are.
	receiver, area := m.owner(th)
	n := store.Message{ID: ulid.Make().String(), Kind: store.KindReply, Source: store.SourceTask, DedupeKey: "thread:" + th.ID + ":" + ulid.Make().String(),
		Importance: 2, Receiver: receiver, Spoken: text, Details: m.summary(th), ThreadID: th.ID,
		ExpiresAt: th.ExpiresAt, OriginDevice: th.Device}
	if err := m.opts.Deliver(ctx, n, area); err != nil {
		m.log.Error("could not deliver a thread message", "thread", th.ID, "error", err)
	}
}

// Confirm commits a thread that waits for a yes.
func (m *Manager) Confirm(ctx context.Context, inv tools.Invocation) (string, error) {
	th, err := m.current(ctx, inv)
	if err != nil {
		return "", errors.New(m.texts().noThread)
	}
	return m.confirm(m.withTurnArea(ctx, inv), th)
}

// ConfirmThread commits a thread by id (the web UI).
func (m *Manager) ConfirmThread(ctx context.Context, id string) (string, error) {
	th, err := m.opts.Store.GetThread(ctx, id)
	if err != nil {
		return "", err
	}
	return m.confirm(ctx, th)
}

func (m *Manager) confirm(ctx context.Context, th store.Thread) (string, error) {
	t := m.texts()
	if th.State != store.ThreadProposed {
		return "", errors.New(t.nothingToConfirm)
	}
	fin, err := m.finisher(th.Target)
	if err != nil {
		return "", err
	}
	m.logEntry(&th, "user", t.yesLogged)
	done, err := fin.Commit(ctx, th)
	if err != nil {
		return "", err
	}
	m.logEntry(&th, "mira", done)
	m.end(ctx, th, store.ThreadDone)
	trace.Add(ctx, trace.Thread, th.Target, "confirmed")
	return done, nil
}

// finisher is the building block that ends a target's threads.
func (m *Manager) finisher(target string) (Finisher, error) {
	sk := m.targets[target]
	if sk.Target.Finish.Action != nil {
		if m.opts.Actions == nil {
			return nil, errors.New("no Home Assistant for the action " + sk.Target.Finish.Action.Action)
		}
		return actionFinish{m, sk}, nil
	}
	if fin := m.opts.Finishers[sk.Target.Finish.Name]; fin != nil {
		return fin, nil
	}
	return nil, fmt.Errorf("no building block %s", sk.Target.Finish.Name)
}

// Cancel ends the turn's thread at the user's request.
func (m *Manager) Cancel(ctx context.Context, inv tools.Invocation) (string, error) {
	th, err := m.current(ctx, inv)
	if err != nil {
		return "", errors.New(m.texts().noThread)
	}
	m.end(ctx, th, store.ThreadCancelled)
	if open := m.stillOpen(ctx, th); open != "" {
		return m.texts().cancelled + " " + open, nil
	}
	return m.texts().cancelled, nil
}

// end closes a thread and withdraws its open messages.
func (m *Manager) end(ctx context.Context, th store.Thread, state string) {
	m.endHow(ctx, th, state, state)
}

// endHow is end with how the summary names the end ("undone" for a thread
// taken back).
func (m *Manager) endHow(ctx context.Context, th store.Thread, state, how string) {
	// Its background work, if any, is no longer wanted, nor its agents.
	if th.State == store.ThreadWorking && m.opts.Cancel != nil {
		m.opts.Cancel(th.ID)
	}
	m.cancelAgents(th)
	if _, err := m.change(ctx, th.ID, func(now *store.Thread) error {
		now.State = state
		return nil
	}); err != nil {
		m.log.Error("could not end a thread", "thread", th.ID, "error", err)
	}
	if n, err := m.opts.Store.WithdrawThreadMessages(ctx, th.ID); err == nil && n > 0 {
		m.log.Info("thread messages withdrawn", "thread", th.ID, "messages", n)
	}
	m.log.Info("thread ended", "thread", th.ID, "state", state)
	if m.opts.Ended != nil {
		m.opts.Ended(ctx, th)
	}
	// The summary and the drop of the history wait for the turns that hold
	// the thread (decision 9), else for the answer of a turn that ended it.
	if m.endHeld(th.ID, how) {
		m.log.Info("thread ended while a turn holds it; its summary waits for that turn", "thread", th.ID)
		return
	}
	if !tools.TurnOf(ctx).HoldSummary(th.ID, how) {
		m.Summarize(ctx, th.ID, how)
	}
}

// Hold keeps a thread's summary and history while a turn that belongs to it
// runs (docs/spec/thread-history.md, decision 9): a thread that ends
// meanwhile is ended at once, and the last holder to let go writes its
// summary and drops its history. release is called once, after the turn
// stored its answer.
func (m *Manager) Hold(threadID string) (release func(ctx context.Context)) {
	m.holdMu.Lock()
	m.holders[threadID]++
	m.holdMu.Unlock()
	var once sync.Once
	return func(ctx context.Context) {
		once.Do(func() {
			m.holdMu.Lock()
			m.holders[threadID]--
			how, ended := m.ended[threadID]
			last := m.holders[threadID] == 0
			if last {
				delete(m.holders, threadID)
				delete(m.ended, threadID)
			}
			m.holdMu.Unlock()
			if last && ended {
				m.Summarize(ctx, threadID, how)
			}
		})
	}
}

// endHeld: the thread is held; its end waits for the last holder.
func (m *Manager) endHeld(threadID, how string) bool {
	m.holdMu.Lock()
	defer m.holdMu.Unlock()
	if m.holders[threadID] == 0 {
		return false
	}
	m.ended[threadID] = how
	return true
}

// Run ends threads nobody answered in time, until ctx ends.
func (m *Manager) Run(ctx context.Context) error {
	tick := m.opts.Clock.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		m.expire(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C():
		}
	}
}

func (m *Manager) expire(ctx context.Context) {
	open, err := m.opts.Store.OpenThreads(ctx, "")
	if err != nil {
		return
	}
	now := m.opts.Clock.Now()
	for _, th := range open {
		if th.State == store.ThreadWorking || !now.After(th.ExpiresAt) {
			continue
		}
		// A plan whose steps were all done is done, not expired.
		if m.isPlan(th) && planOf(th).DoneAt != nil {
			m.end(ctx, th, store.ThreadDone)
			continue
		}
		m.end(ctx, th, store.ThreadExpired)
	}
}

func field(t *skills.Target, name string) (skills.TargetField, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return skills.TargetField{}, false
}

func missing(t *skills.Target, values map[string]string) []string {
	var out []string
	for _, f := range t.Fields {
		if !f.Optional && strings.TrimSpace(values[f.Name]) == "" {
			out = append(out, f.Name)
		}
	}
	return out
}

// notNeeded reports a field the model declared not needed: by protocol
// the value starts with "-" and gives the reason ("- nothing relative in
// the request"). Such a value is an answer, and is not checked against the
// house. A sign, not words: it works in every language.
func notNeeded(value string) bool { return strings.HasPrefix(strings.TrimSpace(value), "-") }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// CancelThread ends a thread by id (the web UI).
func (m *Manager) CancelThread(ctx context.Context, id string) error {
	th, err := m.opts.Store.GetThread(ctx, id)
	if err != nil {
		return err
	}
	if !store.ThreadOpen(th.State) {
		return store.ErrNotFound
	}
	m.end(ctx, th, store.ThreadCancelled)
	return nil
}
