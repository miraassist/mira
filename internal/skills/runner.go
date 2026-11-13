package skills

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

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/schedule"
	"github.com/miraassist/mira/internal/store"
)

// Tasker runs one scheduled job and returns its JSON answer. The
// conversation engine implements it (RunTask).
type Tasker interface {
	RunTask(ctx context.Context, skill, instruction string) (string, error)
}

// maxTaskImportance caps what a scheduled job may report. Its input is
// calendars and other outside data; that must never wake anyone at night,
// unless the skill says so for that schedule (urgent: true), up to
// urgentTaskImportance.
const (
	maxTaskImportance    = 2
	urgentTaskImportance = 3
)

// PromptWait is how long a job waits for Home Assistant's prompt when it is
// missing (docs/spec/context.md, 1.2): long enough for Mira and Home
// Assistant starting together, short enough that an evening job is still an
// evening job.
const PromptWait = 5 * time.Minute

type Runner struct {
	Set    *Set
	Tasker Tasker
	Store  *store.Store
	Logger *slog.Logger
	// People are the persons a per_person schedule runs for.
	People func(ctx context.Context) []Person
	// System are Mira's own jobs (rebuilding the profiles): listed with
	// the skills' schedules, changed in time, never switched off.
	System []*SystemJob
	// Ready returns once Mira can run a job at all: Home Assistant's prompt
	// is there. A job that starts while it is missing waits for it instead
	// of failing (docs/spec/context.md, 1.2).
	Ready func(ctx context.Context) error
	// ReadyWait is how long a job waits for it; PromptWait when zero.
	ReadyWait time.Duration

	mu      sync.Mutex
	changed map[string]bool // jobs whose next run is to be taken anew
	queue   []job           // run now, in order
	state   map[string]string
	wake    chan struct{}
}

// SystemJob is one of Mira's own jobs at a time.
type SystemJob struct {
	Name, Description string
	Cron              string
	// Run does the job and says what came of it ("2 profiles rebuilt").
	Run  func(ctx context.Context) (string, error)
	cron schedule.Cron
}

// NewSystemJob checks the time.
func NewSystemJob(name, description, cron string, run func(ctx context.Context) (string, error)) (*SystemJob, error) {
	c, err := schedule.Parse(cron)
	if err != nil {
		return nil, err
	}
	return &SystemJob{Name: name, Description: description, Cron: cron, Run: run, cron: c}, nil
}

// SystemSkill is the name Mira's own jobs go by, beside the skills'.
const SystemSkill = "mira"

// The states of a job asked to run now (Runner.State); "" is idle.
const (
	StateWaiting = "waiting"
	StateRunning = "running"
)

// ErrRunning: the job runs or waits to run already.
var ErrRunning = errors.New("it is running")

// Person is someone a per_person schedule runs for: the receiver key of
// their messages, their name, the login name of their Home Assistant user
// ("" if unknown), the name of their own calendar ("" if none) and their
// personal profile (what Mira knows about them, "" if nothing).
type Person struct {
	Key, Name, Login, Calendar, Profile string
	// Channel: the person has a channel of their own, so a private
	// message reaches them (not only "alone at home is me").
	Channel bool
}

type job struct {
	skill Skill
	index int
	sched Schedule
	sys   *SystemJob
}

func (j job) key() string {
	if j.sys != nil {
		return SystemSkill + "/" + j.sys.Name
	}
	return fmt.Sprintf("%s/%d", j.skill.Name, j.index)
}

func (j job) cron() schedule.Cron {
	if j.sys != nil {
		return j.sys.cron
	}
	return j.sched.Parsed()
}

// LastRunKey is where a schedule's last run is kept: by its name, else its
// place in the skill; per person for a per_person one.
func LastRunKey(skill string, sc Schedule, index int, person string) store.RunKey {
	name := sc.Name
	if name == "" {
		name = fmt.Sprintf("#%d", index+1)
	}
	return store.RunKey{Skill: skill, Schedule: name, Person: person}
}

// jobs are the schedules that are on, as they are now; with all, the off
// ones too.
func (r *Runner) jobs(all bool) []job {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []job
	for _, sk := range r.Set.Skills {
		for i, sc := range sk.Schedules {
			if all || !sc.Off {
				out = append(out, job{skill: sk, index: i, sched: sc})
			}
		}
	}
	for _, sj := range r.System {
		out = append(out, job{sys: sj})
	}
	return out
}

// find is a job by skill and schedule name; r.mu is held.
func (r *Runner) find(skill, name string) (job, *Schedule, error) {
	if skill == SystemSkill {
		for _, sj := range r.System {
			if sj.Name == name {
				return job{sys: sj}, nil, nil
			}
		}
		return job{}, nil, fmt.Errorf("Mira has no schedule %q", name)
	}
	for si := range r.Set.Skills {
		sk := &r.Set.Skills[si]
		if sk.Name != skill {
			continue
		}
		for i := range sk.Schedules {
			if name != "" && sk.Schedules[i].Name == name {
				return job{skill: *sk, index: i, sched: sk.Schedules[i]}, &sk.Schedules[i], nil
			}
		}
		return job{}, nil, fmt.Errorf("skill %s has no schedule %q", skill, name)
	}
	return job{}, nil, fmt.Errorf("no skill %s", skill)
}

// Change gives a skill's named schedule a new time or switches it on or off
// while Mira runs (docs/spec/schedules.md); its next run is taken anew. An
// empty cron keeps the time. Mira's own (skill "mira") take a time only.
func (r *Runner) Change(skill, name string, ch ScheduleChange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, sc, err := r.find(skill, name)
	if err != nil {
		return err
	}
	if j.sys != nil {
		if ch.Off {
			return errors.New("Mira's own schedules cannot be switched off")
		}
		if ch.Cron != "" {
			c, err := schedule.Parse(ch.Cron)
			if err != nil {
				return err
			}
			j.sys.Cron, j.sys.cron = ch.Cron, c
		}
	} else {
		if ch.Cron != "" {
			if err := sc.SetCron(ch.Cron); err != nil {
				return err
			}
		}
		sc.Off = ch.Off
	}
	if r.changed == nil {
		r.changed = map[string]bool{}
	}
	r.changed[j.key()] = true
	r.signal()
	return nil
}

// RunNow runs a schedule at once, after the job that runs now; also one
// that is off, to try it. It answers at once.
func (r *Runner) RunNow(skill, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, _, err := r.find(skill, name)
	if err != nil {
		return err
	}
	if r.state[j.key()] != "" {
		return ErrRunning
	}
	if r.state == nil {
		r.state = map[string]string{}
	}
	r.state[j.key()] = StateWaiting
	r.queue = append(r.queue, j)
	r.signal()
	return nil
}

// State is whether a schedule waits to run or runs now ("" for neither).
func (r *Runner) State(skill, name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, _, err := r.find(skill, name)
	if err != nil {
		return ""
	}
	return r.state[j.key()]
}

func (r *Runner) setState(j job, s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil {
		r.state = map[string]string{}
	}
	if s == "" {
		delete(r.state, j.key())
	} else {
		r.state[j.key()] = s
	}
}

// signal wakes Run; r.mu is held.
func (r *Runner) signal() {
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runner) wakeup() chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	return r.wake
}

// Run starts every schedule of every enabled skill and Mira's own, and
// blocks until ctx ends. Jobs run one after the other, never in parallel;
// one asked to run now goes first. A schedule changed meanwhile (Change)
// gets its next run anew.
func (r *Runner) Run(ctx context.Context) error {
	log := r.log()
	wake := r.wakeup()
	next := map[string]time.Time{}
	for {
		r.mu.Lock()
		for k := range r.changed {
			delete(next, k)
		}
		r.changed = nil
		var now *job
		if len(r.queue) > 0 {
			j := r.queue[0]
			r.queue = r.queue[1:]
			now = &j
		}
		r.mu.Unlock()
		if now != nil {
			r.run(ctx, *now, log)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		jobs := r.jobs(false)
		var soonest *job
		for i := range jobs {
			j := &jobs[i]
			at, ok := next[j.key()]
			if !ok {
				at = j.cron().Next(r.Set.clock().In(r.Set.loc()))
				next[j.key()] = at
				log.Info("scheduled", "job", j.key(), "next", at.Format("2006-01-02 15:04"))
			}
			if soonest == nil || at.Before(next[soonest.key()]) {
				soonest = j
			}
		}
		var timer <-chan time.Time
		if soonest != nil {
			wait := next[soonest.key()].Sub(r.Set.clock()) // the set's clock, as next was taken
			if wait < 0 {
				wait = 0
			}
			timer = time.After(wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
			continue
		case <-timer:
		}
		j := *soonest
		r.run(ctx, j, log)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		next[j.key()] = j.cron().Next(r.Set.clock().In(r.Set.loc()).Add(time.Second))
	}
}

func (r *Runner) log() *slog.Logger {
	log := r.Logger
	if log == nil {
		log = slog.Default()
	}
	return log.With("component", "skills")
}

// run does one job and keeps its last run; the job's state is cleared
// after it.
func (r *Runner) run(ctx context.Context, j job, log *slog.Logger) {
	r.setState(j, StateRunning)
	defer r.setState(j, "")
	r.waitReady(ctx, j, log)
	if j.sys != nil {
		started := r.Set.clock()
		text, err := j.sys.Run(ctx)
		run := store.LastRun{Started: started, Ended: r.Set.clock(), Status: store.RunDone, Text: text}
		if err != nil {
			run.Status, run.Error = store.RunFailed, err.Error()
			log.Error("scheduled task failed", "job", j.key(), "error", err)
		}
		r.keep(ctx, store.RunKey{Skill: SystemSkill, Schedule: j.sys.Name}, run)
		return
	}
	if queued, err := r.RunJob(ctx, j.skill, j.index); err != nil {
		log.Error("scheduled task failed", "skill", j.skill.Name, "error", err)
	} else {
		log.Info("scheduled task done", "skill", j.skill.Name, "message", queued)
	}
}

// waitReady lets a job wait for Home Assistant's prompt while it is
// missing, at most ReadyWait. After that the job runs all the same and
// fails as every job without the prompt (docs/spec/context.md, 1.2).
func (r *Runner) waitReady(ctx context.Context, j job, log *slog.Logger) {
	if r.Ready == nil {
		return
	}
	wait := r.ReadyWait
	if wait == 0 {
		wait = PromptWait
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	started := time.Now()
	err := r.Ready(waitCtx)
	waited := time.Since(started)
	switch {
	case ctx.Err() != nil: // Mira is shutting down
	case err != nil:
		log.Warn("home assistant's prompt did not come; the job runs and fails", "job", j.key(),
			"waited", wait.String())
	case waited > time.Second:
		log.Info("the job waited for home assistant's prompt", "job", j.key(), "ms", waited.Milliseconds())
	}
}

// forgetGone deletes the last runs of persons no longer among a per-person
// schedule's persons (docs/spec/kv-cleanup.md, step 2). Without any person
// (none known, or Home Assistant not read) nothing goes.
func (r *Runner) forgetGone(ctx context.Context, sk Skill, index int, people []Person) {
	if r.Store == nil || len(people) == 0 {
		return
	}
	keys := make([]string, 0, len(people))
	for _, p := range people {
		keys = append(keys, p.Key)
	}
	k := LastRunKey(sk.Name, sk.Schedules[index], index, "")
	n, err := r.Store.PruneLastRuns(context.WithoutCancel(ctx), k.Skill, k.Schedule, keys)
	switch {
	case err != nil:
		r.log().Warn("could not forget the last runs of persons gone; they stay on the page", "skill", k.Skill,
			"schedule", k.Schedule, "error", err)
	case n > 0:
		r.log().Info("last runs of persons gone forgotten", "skill", k.Skill, "schedule", k.Schedule, "count", n)
	}
}

// keep stores a last run; a failure to keep it is only logged.
func (r *Runner) keep(ctx context.Context, key store.RunKey, run store.LastRun) {
	if r.Store == nil {
		return
	}
	if err := r.Store.SetLastRun(context.WithoutCancel(ctx), key, run); err != nil {
		r.log().Warn("could not keep the last run", "skill", key.Skill, "schedule", key.Schedule,
			"person", key.Person, "error", err)
	}
}

type taskResult struct {
	Importance     int     `json:"importance"`
	Spoken         string  `json:"spoken"`
	Details        string  `json:"details"`
	ExpiresInHours float64 `json:"expires_in_hours"`
}

// RunJob runs schedule index of a skill now and queues its message, if it
// has one: for the house, or, with per_person, one for each person with a
// calendar of their own. It reports whether a message was queued.
func (r *Runner) RunJob(ctx context.Context, sk Skill, index int) (bool, error) {
	if index < 0 || index >= len(sk.Schedules) {
		return false, fmt.Errorf("skill %s has no schedule %d", sk.Name, index)
	}
	if !sk.Schedules[index].PerPerson {
		return r.runFor(ctx, sk, index, Person{})
	}
	var people []Person
	if r.People != nil {
		people = r.People(ctx)
	}
	queued := false
	var errs []error
	for _, p := range people {
		q, err := r.runFor(ctx, sk, index, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Key, err))
		}
		queued = queued || q
	}
	r.forgetGone(ctx, sk, index, people)
	return queued, errors.Join(errs...)
}

// runFor runs one task, for the house when p is empty, else for p, and
// keeps how it ran.
func (r *Runner) runFor(ctx context.Context, sk Skill, index int, p Person) (bool, error) {
	run := store.LastRun{Started: r.Set.clock()}
	queued, spoken, err := r.task(ctx, sk, index, p)
	run.Ended = r.Set.clock()
	switch {
	case err != nil:
		run.Status, run.Error = store.RunFailed, err.Error()
	case queued:
		run.Status, run.Text = store.RunDone, spoken
	default:
		run.Status = store.RunNothing
	}
	r.keep(ctx, LastRunKey(sk.Name, sk.Schedules[index], index, p.Key), run)
	return queued, err
}

// task runs one task and queues its message; it returns what is to be
// spoken.
func (r *Runner) task(ctx context.Context, sk Skill, index int, p Person) (bool, string, error) {
	t := i18n.Pick(catalog, r.Set.Lang)
	task := strings.TrimSpace(sk.Schedules[index].Task)
	// A schedule for one person: found before the job runs, so a wrong name
	// costs no model call and sends nothing to the house instead.
	var to string
	if name := sk.Schedules[index].To; name != "" {
		var err error
		if to, err = r.receiver(ctx, name); err != nil {
			return false, "", err
		}
	}
	if p.Key != "" {
		cal := p.Calendar
		if cal == "" {
			cal = "-"
		}
		login := p.Login
		if login == "" {
			login = "-"
		}
		task = strings.NewReplacer("{{person}}", p.Name, "{{person_calendar}}", cal, "{{person_login}}", login).Replace(task)
		profile := strings.TrimSpace(p.Profile)
		if profile == "" {
			profile = t.noProfile
		}
		task += "\n\n" + fmt.Sprintf(t.personContext, p.Name) + "\n" + profile
	}
	limit, rules := maxTaskImportance, t.taskRules
	if sk.Schedules[index].Urgent {
		limit, rules = urgentTaskImportance, t.taskRulesUrgent
	}
	instruction := r.Set.withHome(ctx, fmt.Sprintf(t.taskIntro, sk.Name)+sk.Body+"\n\n---\n\n"+task+rules)
	raw, err := r.Tasker.RunTask(ctx, sk.Name, instruction)
	if err != nil {
		return false, "", err
	}
	var res taskResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return false, "", fmt.Errorf("unparseable result %q: %w", excerpt.Of(raw), err)
	}
	res.Spoken = strings.TrimSpace(res.Spoken)
	if res.Importance <= 0 || res.Spoken == "" {
		return false, "", nil
	}
	if res.Importance > limit {
		res.Importance = limit
	}
	if res.ExpiresInHours <= 0 || res.ExpiresInHours > 72 {
		res.ExpiresInHours = 12
	}
	now := r.Set.clock()
	// One message per job, day (and person): a rerun updates it.
	dedupe := fmt.Sprintf("skill:%s:%d:%s", sk.Name, index, now.In(r.Set.loc()).Format("2006-01-02"))
	receiver, confidentiality := store.ReceiverHouse, store.ConfidentialityNormal
	if p.Key != "" {
		dedupe += ":" + p.Key
		receiver = p.Key
	}
	if to != "" {
		dedupe += ":" + to
		receiver, confidentiality = to, store.ConfidentialityPrivate
	}
	_, _, err = r.Store.PutMessage(ctx, store.Message{
		ID: ulid.Make().String(), Kind: store.KindFor(res.Importance, confidentiality), Source: store.SourceSchedule,
		DedupeKey:  dedupe,
		Importance: res.Importance, Receiver: receiver,
		Spoken: res.Spoken, Details: strings.TrimSpace(res.Details),
		ExpiresAt: now.Add(time.Duration(res.ExpiresInHours * float64(time.Hour))),
	})
	return err == nil, res.Spoken, err
}

// receiver finds the person a schedule's to names, by key, name or login,
// case and spaces aside, among those with a channel of their own.
func (r *Runner) receiver(ctx context.Context, name string) (string, error) {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	want := norm(name)
	var people []Person
	if r.People != nil {
		people = r.People(ctx)
	}
	for _, p := range people {
		if !p.Channel {
			continue
		}
		if norm(p.Key) == want || norm(p.Name) == want || (p.Login != "" && norm(p.Login) == want) {
			return p.Key, nil
		}
	}
	return "", fmt.Errorf("no person %q with a channel of their own", name)
}

// NextRun is when a schedule runs next; zero while it is off.
func (r *Runner) NextRun(sc Schedule) time.Time {
	if sc.Off {
		return time.Time{}
	}
	return sc.Parsed().Next(r.Set.clock().In(r.Set.loc()))
}

// Schedules are the skills' schedules as they are now (with the changes
// made while Mira runs).
func (r *Runner) Schedules(skill string) []Schedule {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sk := range r.Set.Skills {
		if sk.Name == skill {
			return append([]Schedule(nil), sk.Schedules...)
		}
	}
	return nil
}

// NextSystemRun is when one of Mira's own jobs runs next.
func (r *Runner) NextSystemRun(sj *SystemJob) time.Time {
	r.mu.Lock()
	c := sj.cron
	r.mu.Unlock()
	return c.Next(r.Set.clock().In(r.Set.loc()))
}
