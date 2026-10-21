// Package threads runs threads ("Vorgänge", docs/spec/vorgaenge.md): a
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
	"log/slog"
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
	// Deliver brings a thread's question or proposal to whoever asked,
	// like a reminder: spoken in the area, otherwise privately.
	Deliver func(ctx context.Context, n store.Message, areaID string) error
	Persons func() map[string]config.Person
	// InlineWait is how long a turn waits for background work before it
	// says "I'll come back to you"; default 5s, within Home Assistant's
	// 15 seconds for a voice turn.
	InlineWait time.Duration
	Clock      func() time.Time
	Logger     *slog.Logger
}

type Manager struct {
	opts    Options
	log     *slog.Logger
	targets map[string]skills.Skill
}

func New(opts Options) *Manager {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.InlineWait == 0 {
		opts.InlineWait = 5 * time.Second
	}
	m := &Manager{opts: opts, log: opts.Logger.With("component", "threads"), targets: map[string]skills.Skill{}}
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

func (m *Manager) texts() texts { return i18n.Pick(catalog, m.opts.Lang) }

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

// current is the thread a tool call acts on: the turn's own, or else the
// newest open one at its place.
func (m *Manager) current(ctx context.Context, inv tools.Invocation) (store.Thread, error) {
	if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil && sess.ThreadID != "" {
		if th, err := m.opts.Store.GetThread(ctx, sess.ThreadID); err == nil && store.ThreadOpen(th.State) {
			return th, nil
		}
	}
	place, _ := m.place(ctx, inv)
	open, err := m.opts.Store.OpenThreads(ctx, place)
	if err != nil {
		return store.Thread{}, err
	}
	if len(open) == 0 {
		return store.Thread{}, store.ErrNotFound
	}
	_ = m.opts.Store.SetSessionThread(ctx, inv.SessionKey, open[0].ID)
	return open[0], nil
}

func (m *Manager) save(ctx context.Context, th *store.Thread) error {
	sk := m.targets[th.Target]
	th.ExpiresAt = m.opts.Clock().Add(sk.Target.TimeoutDuration())
	if sk.Target.Plan {
		if c := planOf(*th); c.DoneAt != nil {
			th.ExpiresAt = c.DoneAt.Add(planGrace)
		}
	}
	return m.opts.Store.PutThread(ctx, *th)
}

func (m *Manager) logEntry(th *store.Thread, who, text string) {
	th.Log = append(th.Log, store.ThreadEntry{At: m.opts.Clock(), Who: who, Text: text})
}

// Start opens a thread for a target at the turn's place. An older open
// thread with the same target there ends: the user starts over. A plan is
// different: it may be well under way, so the user decides whether to go on
// or start anew.
func (m *Manager) Start(ctx context.Context, inv tools.Invocation, target, request string) (string, error) {
	sk, ok := m.targets[target]
	if !ok {
		return "", fmt.Errorf(m.texts().noTarget, target)
	}
	place, _ := m.place(ctx, inv)
	if open, err := m.opts.Store.OpenThreads(ctx, place); err == nil {
		for _, old := range open {
			if old.Target != target {
				continue
			}
			if sk.Target.Plan {
				_ = m.opts.Store.SetSessionThread(ctx, inv.SessionKey, old.ID)
				return fmt.Sprintf(m.texts().planRunning, old.Request, m.planStatus(old)), nil
			}
			m.end(ctx, old, store.ThreadCancelled)
		}
	}
	th := store.Thread{ID: strings.ToLower(ulid.Make().String()), Target: target, Place: place, Person: inv.Person,
		State: store.ThreadCollecting, Request: strings.TrimSpace(request), Fields: map[string]string{}}
	m.logEntry(&th, "user", th.Request)
	if err := m.save(ctx, &th); err != nil {
		return "", err
	}
	if err := m.opts.Store.SetSessionThread(ctx, inv.SessionKey, th.ID); err != nil {
		return "", err
	}
	m.log.Info("thread started", "thread", th.ID, "target", target, "place", place)
	trace.Add(ctx, trace.Thread, target, "started")
	return m.texts().started + "\n\n" + sk.Body + "\n\n" + m.status(th), nil
}

// Fill takes field values from the model, checks them and, once all are
// there, runs the target's work.
func (m *Manager) Fill(ctx context.Context, inv tools.Invocation, values map[string]string) (string, error) {
	th, err := m.current(ctx, inv)
	if err != nil {
		return "", errors.New(m.texts().noThread)
	}
	sk := m.targets[th.Target]
	t := m.texts()
	if sk.Target.Plan {
		return "", errors.New(t.planNoFields)
	}
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
		_, _ = m.opts.Store.WithdrawThreadMessages(ctx, th.ID)
	}
	if len(problems) > 0 || len(missing(sk.Target, th.Fields)) > 0 {
		if err := m.save(ctx, &th); err != nil {
			return "", err
		}
		return strings.Join(append(problems, m.status(th)), "\n"), nil
	}
	return m.work(ctx, inv, th)
}

// work runs the finish once the fields are complete.
func (m *Manager) work(ctx context.Context, inv tools.Invocation, th store.Thread) (string, error) {
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
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		res, err := fin.Work(ctx, th)
		if err != nil {
			m.log.Error("thread work failed", "thread", th.ID, "error", err)
			res = Result{Cancel: fmt.Sprintf(t.workFailed, th.Request)}
		}
		mu.Lock()
		defer mu.Unlock()
		if !abandoned {
			inTime <- res
			return
		}
		// The user may have cancelled meanwhile.
		if cur, err := m.opts.Store.GetThread(ctx, th.ID); err != nil || cur.State != store.ThreadWorking {
			return
		}
		m.apply(ctx, th, res, true)
	}()
	timer := time.NewTimer(m.opts.InlineWait)
	defer timer.Stop()
	select {
	case res := <-inTime:
		return m.apply(ctx, th, res, false), nil
	case <-timer.C:
	}
	mu.Lock()
	defer mu.Unlock()
	select {
	case res := <-inTime: // arrived just now
		return m.apply(ctx, th, res, false), nil
	default:
		abandoned = true
	}
	return t.working, nil
}

// apply records a work result and, for background work, tells the user.
// It returns what the model should say for foreground work.
func (m *Manager) apply(ctx context.Context, th store.Thread, res Result, tell bool) string {
	t := m.texts()
	var say string
	if res.Content != nil {
		th.Content = res.Content
	}
	switch {
	case res.Question != "":
		th.State = store.ThreadWaiting
		say = res.Question
	case res.Proposal != "":
		th.State = store.ThreadProposed
		say = res.Proposal
	case res.Cancel != "":
		th.State = store.ThreadCancelled
		say = res.Cancel
	default:
		th.State = store.ThreadDone
		say = res.Done
	}
	m.logEntry(&th, "mira", say)
	if err := m.save(ctx, &th); err != nil {
		m.log.Error("could not save a thread", "thread", th.ID, "error", err)
	}
	m.log.Info("thread advanced", "thread", th.ID, "state", th.State)
	trace.Add(ctx, trace.Thread, th.Target, th.State)
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

// tell delivers a thread's question or proposal as a message of the thread:
// a reply to it continues the thread.
func (m *Manager) tell(ctx context.Context, th store.Thread, text string) {
	if m.opts.Deliver == nil {
		return
	}
	area := ""
	receiver := store.ReceiverHouse
	if a, ok := strings.CutPrefix(th.Place, "area:"); ok {
		area = a
	} else if p, ok := strings.CutPrefix(th.Place, "person:"); ok {
		receiver = p
	} else {
		var persons map[string]config.Person
		if m.opts.Persons != nil {
			persons = m.opts.Persons()
		}
		receiver = tools.ReceiverFor("", tools.Invocation{SessionKey: th.Place, Channel: config.ChannelTelegram}, persons)
	}
	n := store.Message{ID: ulid.Make().String(), Source: store.SourceTask, DedupeKey: "thread:" + th.ID + ":" + ulid.Make().String(),
		Importance: 2, Receiver: receiver, Spoken: text, Details: m.summary(th), ThreadID: th.ID,
		ExpiresAt: th.ExpiresAt}
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
	return m.confirm(ctx, th)
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
	return m.texts().cancelled, nil
}

// end closes a thread and withdraws its open messages.
func (m *Manager) end(ctx context.Context, th store.Thread, state string) {
	th.State = state
	if err := m.opts.Store.PutThread(ctx, th); err != nil {
		m.log.Error("could not end a thread", "thread", th.ID, "error", err)
	}
	if n, err := m.opts.Store.WithdrawThreadMessages(ctx, th.ID); err == nil && n > 0 {
		m.log.Info("thread messages withdrawn", "thread", th.ID, "messages", n)
	}
	m.log.Info("thread ended", "thread", th.ID, "state", state)
}

// Run ends threads nobody answered in time, until ctx ends.
func (m *Manager) Run(ctx context.Context) error {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		m.expire(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (m *Manager) expire(ctx context.Context) {
	open, err := m.opts.Store.OpenThreads(ctx, "")
	if err != nil {
		return
	}
	now := m.opts.Clock()
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
