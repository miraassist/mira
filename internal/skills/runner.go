package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

// Tasker runs one scheduled job and returns its JSON answer. The
// conversation engine implements it (RunTask).
type Tasker interface {
	RunTask(ctx context.Context, instruction string) (string, error)
}

// maxTaskImportance caps what a scheduled job may report. Its input is
// calendars and other outside data; that must never wake anyone at night.
const maxTaskImportance = 2

type Runner struct {
	Set    *Set
	Tasker Tasker
	Store  *store.Store
	Logger *slog.Logger
	// People are the persons a per_person schedule runs for.
	People func(ctx context.Context) []Person
}

// Person is someone a per_person schedule runs for: the receiver key of
// their messages, their name, the name of their own calendar ("" if none)
// and their personal profile (what Mira knows about them, "" if nothing).
type Person struct {
	Key, Name, Calendar, Profile string
}

type job struct {
	skill Skill
	index int
	sched Schedule
}

// Run starts every schedule of every enabled skill and blocks until ctx
// ends. Jobs run one after the other, never in parallel.
func (r *Runner) Run(ctx context.Context) error {
	log := r.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "skills")
	var jobs []job
	for _, sk := range r.Set.Skills {
		for i, sc := range sk.Schedules {
			jobs = append(jobs, job{skill: sk, index: i, sched: sc})
		}
	}
	if len(jobs) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	next := make([]time.Time, len(jobs))
	for i, j := range jobs {
		next[i] = j.sched.Parsed().Next(r.Set.clock().In(r.Set.loc()))
		log.Info("scheduled", "skill", j.skill.Name, "cron", j.sched.Cron, "next", next[i].Format("2006-01-02 15:04"))
	}
	for {
		soonest := 0
		for i := range next {
			if next[i].Before(next[soonest]) {
				soonest = i
			}
		}
		wait := time.Until(next[soonest])
		if wait < 0 {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		j := jobs[soonest]
		if queued, err := r.RunJob(ctx, j.skill, j.index); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Error("scheduled task failed", "skill", j.skill.Name, "error", err)
		} else {
			log.Info("scheduled task done", "skill", j.skill.Name, "message", queued)
		}
		next[soonest] = j.sched.Parsed().Next(r.Set.clock().In(r.Set.loc()).Add(time.Second))
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
		task = strings.NewReplacer("{{person}}", p.Name, "{{person_calendar}}", cal).Replace(task)
		profile := strings.TrimSpace(p.Profile)
		if profile == "" {
			profile = t.noProfile
		}
		task += "\n\n" + fmt.Sprintf(t.personContext, p.Name) + "\n" + profile
	}
	instruction := fmt.Sprintf(t.taskIntro, sk.Name) + sk.Body + "\n\n---\n\n" + task + t.taskRules
	raw, err := r.Tasker.RunTask(ctx, instruction)
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
	if res.Importance > maxTaskImportance {
		res.Importance = maxTaskImportance
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
