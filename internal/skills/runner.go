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

	"github.com/miraassist/mira/internal/i18n"
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

type Runner struct {
	Set    *Set
	Tasker Tasker
	Store  *store.Store
	Logger *slog.Logger
	// People are the persons a per_person schedule runs for.
	People func(ctx context.Context) []Person

	mu      sync.Mutex
	changed map[string]bool // jobs whose next run is to be taken anew
	wake    chan struct{}
}

// Person is someone a per_person schedule runs for: the receiver key of
// their messages, their name, the login name of their Home Assistant user
// ("" if unknown), the name of their own calendar ("" if none) and their
// personal profile (what Mira knows about them, "" if nothing).
type Person struct {
	Key, Name, Login, Calendar, Profile string
}

type job struct {
	skill Skill
	index int
	sched Schedule
}

func (j job) key() string { return fmt.Sprintf("%s/%d", j.skill.Name, j.index) }

// jobs are the schedules that are on, as they are now.
func (r *Runner) jobs() []job {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []job
	for _, sk := range r.Set.Skills {
		for i, sc := range sk.Schedules {
			if !sc.Off {
				out = append(out, job{skill: sk, index: i, sched: sc})
			}
		}
	}
	return out
}

// Change gives a skill's named schedule a new time or switches it on or off
// while Mira runs (docs/spec/zeitplan.md); its next run is taken anew. An
// empty cron keeps the time.
func (r *Runner) Change(skill, name string, ch ScheduleChange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for si := range r.Set.Skills {
		sk := &r.Set.Skills[si]
		if sk.Name != skill {
			continue
		}
		for i := range sk.Schedules {
			sc := &sk.Schedules[i]
			if name == "" || sc.Name != name {
				continue
			}
			if ch.Cron != "" {
				if err := sc.SetCron(ch.Cron); err != nil {
					return err
				}
			}
			sc.Off = ch.Off
			if r.changed == nil {
				r.changed = map[string]bool{}
			}
			r.changed[job{skill: *sk, index: i}.key()] = true
			r.signal()
			return nil
		}
		return fmt.Errorf("skill %s has no schedule %q", skill, name)
	}
	return fmt.Errorf("no skill %s", skill)
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

// Run starts every schedule of every enabled skill and blocks until ctx
// ends. Jobs run one after the other, never in parallel. A schedule changed
// meanwhile (Change) gets its next run anew.
func (r *Runner) Run(ctx context.Context) error {
	log := r.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "skills")
	wake := r.wakeup()
	next := map[string]time.Time{}
	for {
		r.mu.Lock()
		for k := range r.changed {
			delete(next, k)
		}
		r.changed = nil
		r.mu.Unlock()
		jobs := r.jobs()
		var soonest *job
		for i := range jobs {
			j := &jobs[i]
			at, ok := next[j.key()]
			if !ok {
				at = j.sched.Parsed().Next(r.Set.clock().In(r.Set.loc()))
				next[j.key()] = at
				log.Info("scheduled", "skill", j.skill.Name, "schedule", j.sched.Name, "cron", j.sched.Cron, "next", at.Format("2006-01-02 15:04"))
			}
			if soonest == nil || at.Before(next[soonest.key()]) {
				soonest = j
			}
		}
		var timer <-chan time.Time
		if soonest != nil {
			wait := time.Until(next[soonest.key()])
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
		if queued, err := r.RunJob(ctx, j.skill, j.index); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Error("scheduled task failed", "skill", j.skill.Name, "error", err)
		} else {
			log.Info("scheduled task done", "skill", j.skill.Name, "message", queued)
		}
		next[j.key()] = j.sched.Parsed().Next(r.Set.clock().In(r.Set.loc()).Add(time.Second))
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
	return queued, errors.Join(errs...)
}

// runFor runs one task: for the house when p is empty, else for p.
func (r *Runner) runFor(ctx context.Context, sk Skill, index int, p Person) (bool, error) {
	t := i18n.Pick(catalog, r.Set.Lang)
	task := strings.TrimSpace(sk.Schedules[index].Task)
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
		return false, err
	}
	var res taskResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return false, fmt.Errorf("unparseable result: %w", err)
	}
	res.Spoken = strings.TrimSpace(res.Spoken)
	if res.Importance <= 0 || res.Spoken == "" {
		return false, nil
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
	receiver := store.ReceiverHouse
	if p.Key != "" {
		dedupe += ":" + p.Key
		receiver = p.Key
	}
	_, _, err = r.Store.PutMessage(ctx, store.Message{
		ID: ulid.Make().String(), Source: store.SourceSchedule,
		DedupeKey:  dedupe,
		Importance: res.Importance, Receiver: receiver,
		Spoken: res.Spoken, Details: strings.TrimSpace(res.Details),
		ExpiresAt: now.Add(time.Duration(res.ExpiresInHours * float64(time.Hour))),
	})
	return err == nil, err
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
