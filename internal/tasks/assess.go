package tasks

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

// Request is what mira.assess and mira.decide send. Templates in it are
// already rendered by Home Assistant.
type Request struct {
	// Goal is the task's aim in the user's words (assess).
	Goal string `json:"goal"`
	// Question is a yes/no question (decide).
	Question    string        `json:"question"`
	Observation string        `json:"observation"`
	Data        []DataRequest `json:"data"`
	// Memory: relevant (default), profile or none.
	Memory string `json:"memory"`
	// Cooldown like "6h": within it the same task only reports again when
	// the importance rises.
	Cooldown string `json:"cooldown"`
	// Notify false makes a dry run: the verdict, no notice.
	Notify *bool `json:"notify"`
	// Task is the automation's id; the drafts pass {{ this.attributes.id }}.
	Task            string `json:"task"`
	Owner           string `json:"owner"`
	Audience        string `json:"audience"`
	Confidentiality string `json:"confidentiality"`
	DedupeKey       string `json:"dedupe_key"`
	ExpiresIn       string `json:"expires_in"`
}

// Verdict is the answer to an automation (response_variable).
type Verdict struct {
	Importance int    `json:"importance"`
	Spoken     string `json:"spoken"`
	Details    string `json:"details,omitempty"`
	NoticeID   string `json:"notice_id"`
	// Withheld says why a verdict worth a notice became none: "cooldown"
	// or "dry_run".
	Withheld string `json:"withheld,omitempty"`
}

// Decision is mira.decide's answer.
type Decision struct {
	Answer bool   `json:"answer"`
	Reason string `json:"reason"`
}

const (
	memoryRelevant = "relevant"
	memoryProfile  = "profile"
	memoryNone     = "none"

	// recentVerdicts is how many earlier judgements of the same task the
	// model sees, so it does not repeat itself and can see a trend.
	recentVerdicts = 5
	maxFacts       = 5
	allFactsUpTo   = 30
	defaultExpiry  = 12 * time.Hour
)

const verdictSchema = `{"type":"object","properties":{
"importance":{"type":"integer","minimum":0,"maximum":3},"spoken":{"type":"string"},"details":{"type":"string"}},
"required":["importance","spoken"]}`

const decisionSchema = `{"type":"object","properties":{"answer":{"type":"boolean"},"reason":{"type":"string"}},
"required":["answer","reason"]}`

// Assess judges an observation against a goal and, if it is worth it and
// notifying is on, queues a notice: mira.assess.
func (m *Manager) Assess(ctx context.Context, r Request) (Verdict, error) {
	if strings.TrimSpace(r.Goal) == "" {
		return Verdict{}, errors.New("goal is required")
	}
	cooldown, err := optionalDuration(r.Cooldown)
	if err != nil {
		return Verdict{}, fmt.Errorf("cooldown: %w", err)
	}
	expiry := defaultExpiry
	if d, err := optionalDuration(r.ExpiresIn); err != nil {
		return Verdict{}, fmt.Errorf("expires_in: %w", err)
	} else if d > 0 {
		expiry = d
	}
	t := m.texts()
	key := taskKey(r.Task, r.Goal)
	data := m.render(ctx, r.Data)
	prompt := m.judgement(ctx, key, r, data) + t.assessRules
	raw, err := m.opts.Model.Ask(ctx, t.judgeSystem, prompt, json.RawMessage(verdictSchema))
	if err != nil {
		return Verdict{}, err
	}
	var v Verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return Verdict{}, fmt.Errorf("unparseable verdict: %w", err)
	}
	v.Importance = min(max(v.Importance, 0), 3)
	v.Spoken, v.Details = strings.TrimSpace(v.Spoken), strings.TrimSpace(v.Details)
	if v.Spoken == "" {
		v.Importance = 0
	}

	dryRun := r.Notify != nil && !*r.Notify
	switch {
	case v.Importance == 0:
	case dryRun:
		v.Withheld = "dry_run"
	case m.coolingDown(ctx, key, v.Importance, cooldown):
		v.Withheld = "cooldown"
	default:
		details := v.Details
		if details == "" {
			details = data
		}
		dedupe := r.DedupeKey
		if dedupe == "" {
			dedupe = "task:" + key
		}
		audience := r.Audience
		if audience == "" {
			audience = store.AudienceHousehold
		}
		n, _, err := m.opts.Store.PutNotice(ctx, store.Notice{
			ID: ulid.Make().String(), Source: store.SourceTask, DedupeKey: dedupe,
			Importance: v.Importance, Audience: audience, Confidentiality: r.Confidentiality,
			Spoken: v.Spoken, Details: details, ExpiresAt: m.opts.Clock().Add(expiry),
		})
		if err != nil {
			return Verdict{}, err
		}
		v.NoticeID = n.ID
	}
	m.log.Info("assessed", "task", key, "importance", v.Importance, "notice", v.NoticeID, "withheld", v.Withheld)
	m.record(ctx, store.Assessment{Task: key, Kind: store.AssessmentAssess, Goal: r.Goal,
		Input: inputOf(r.Observation, data), Importance: v.Importance, Spoken: v.Spoken,
		NoticeID: v.NoticeID, DryRun: dryRun})
	return v, nil
}

// Decide answers a yes/no question about the house: mira.decide. It never
// makes a notice; the automation acts on the answer.
func (m *Manager) Decide(ctx context.Context, r Request) (Decision, error) {
	if strings.TrimSpace(r.Question) == "" {
		return Decision{}, errors.New("question is required")
	}
	t := m.texts()
	key := taskKey(r.Task, r.Question)
	data := m.render(ctx, r.Data)
	prompt := m.judgement(ctx, key, r, data) + t.decideRules
	raw, err := m.opts.Model.Ask(ctx, t.judgeSystem, prompt, json.RawMessage(decisionSchema))
	if err != nil {
		return Decision{}, err
	}
	var d Decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Decision{}, fmt.Errorf("unparseable decision: %w", err)
	}
	d.Reason = strings.TrimSpace(d.Reason)
	m.log.Info("decided", "task", key, "answer", d.Answer)
	m.record(ctx, store.Assessment{Task: key, Kind: store.AssessmentDecide, Goal: r.Question,
		Input: inputOf(r.Observation, data), Answer: d.Answer, Spoken: d.Reason})
	return d, nil
}

// judgement is the isolated prompt: the task, what happened, the condensed
// data and the context. No house prompt: the model sees what the task asks
// for and nothing else.
func (m *Manager) judgement(ctx context.Context, key string, r Request, data string) string {
	t := m.texts()
	var b strings.Builder
	if r.Goal != "" {
		fmt.Fprintf(&b, t.secGoal, strings.TrimSpace(r.Goal))
	}
	if r.Question != "" {
		fmt.Fprintf(&b, t.secQuestion, strings.TrimSpace(r.Question))
	}
	if obs := strings.TrimSpace(r.Observation); obs != "" {
		fmt.Fprintf(&b, t.secObservation, obs)
	}
	if data != "" {
		fmt.Fprintf(&b, t.secData, data)
	}
	b.WriteString(m.situation(ctx))
	if mem := m.memory(ctx, r); mem != "" {
		fmt.Fprintf(&b, t.secMemory, mem)
	}
	if earlier := m.earlier(ctx, key); earlier != "" {
		fmt.Fprintf(&b, t.secEarlier, earlier)
	}
	return b.String()
}

// situation is the time, quiet hours and who is home: cheap, and often
// what decides ("nobody is home and water runs").
func (m *Manager) situation(ctx context.Context) string {
	t := m.texts()
	now := m.opts.Clock().In(m.opts.Location)
	quiet := t.no
	if inQuietHours(m.opts.QuietHours, now) {
		quiet = t.yes
	}
	home, away := m.presence(ctx)
	return fmt.Sprintf(t.secSituation, i18n.FormatDate(m.opts.Lang, now), now.Format("15:04"), quiet,
		list(t, home), list(t, away))
}

func list(t texts, names []string) string {
	if len(names) == 0 {
		return t.nobody
	}
	return strings.Join(names, ", ")
}

func (m *Manager) presence(ctx context.Context) (home, away []string) {
	if m.opts.Persons == nil {
		return nil, nil
	}
	persons := m.opts.Persons()
	if len(persons) == 0 {
		return nil, nil
	}
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return nil, nil
	}
	byID := map[string]string{}
	for _, s := range states {
		byID[s.EntityID] = s.State
	}
	for _, key := range sortedKeys(persons) {
		p := persons[key]
		if p.Entity == "" {
			continue
		}
		if byID[p.Entity] == "home" {
			home = append(home, key)
		} else {
			away = append(away, key)
		}
	}
	return home, away
}

// memory is the household profile and the facts that match the task. Read
// only: judgements write nothing to memory.
func (m *Manager) memory(ctx context.Context, r Request) string {
	mode := r.Memory
	if mode == "" {
		mode = memoryRelevant
	}
	if mode == memoryNone {
		return ""
	}
	var parts []string
	if profile, _, err := m.opts.Store.Profile(ctx, store.OwnerHouse); err == nil && strings.TrimSpace(profile) != "" {
		parts = append(parts, strings.TrimSpace(profile))
	}
	if mode == memoryRelevant {
		// A household has few facts, and the full-text search only matches
		// whole words ("Wasserverbrauch" misses "Wasser"): while they are
		// few, all go along; beyond that the best matches. Tasks belong to
		// the house for now, so only its memory goes along.
		facts, err := m.opts.Store.AllFacts(ctx, store.OwnerHouse)
		if err == nil && len(facts) > allFactsUpTo {
			facts, err = m.opts.Store.SearchFacts(ctx, r.Goal+" "+r.Question+" "+r.Observation, []string{store.OwnerHouse}, maxFacts)
		}
		if err == nil {
			for _, f := range facts {
				parts = append(parts, "- "+f.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// earlier lists the task's last judgements, newest first.
func (m *Manager) earlier(ctx context.Context, key string) string {
	list, err := m.opts.Store.Assessments(ctx, key, recentVerdicts)
	if err != nil || len(list) == 0 {
		return ""
	}
	t := m.texts()
	var b strings.Builder
	for _, a := range list {
		when := a.CreatedAt.In(m.opts.Location).Format("02.01. 15:04")
		switch {
		case a.Kind == store.AssessmentDecide:
			fmt.Fprintf(&b, t.earlierDecide, when, yesNo(t, a.Answer), a.Spoken)
		case a.Importance == 0:
			fmt.Fprintf(&b, t.earlierQuiet, when)
		default:
			fmt.Fprintf(&b, t.earlierSaid, when, a.Importance, a.Spoken)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func yesNo(t texts, v bool) string {
	if v {
		return t.yes
	}
	return t.no
}

// coolingDown reports whether the task notified within cooldown at the same
// or a higher importance.
func (m *Manager) coolingDown(ctx context.Context, key string, importance int, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return false
	}
	list, err := m.opts.Store.Assessments(ctx, key, 20)
	if err != nil {
		return false
	}
	now := m.opts.Clock()
	for _, a := range list {
		if now.Sub(a.CreatedAt) > cooldown {
			break
		}
		if a.NoticeID != "" && a.Importance >= importance {
			return true
		}
	}
	return false
}

func (m *Manager) record(ctx context.Context, a store.Assessment) {
	a.ID = ulid.Make().String()
	a.CreatedAt = m.opts.Clock()
	if err := m.opts.Store.AddAssessment(ctx, a); err != nil {
		m.log.Warn("could not log an assessment", "task", a.Task, "error", err)
	}
}

// taskKey is the automation's id, or, when the automation did not pass
// one, a key from the goal: the same goal shares its earlier judgements.
func taskKey(task, goal string) string {
	if task = strings.TrimSpace(task); task != "" {
		return task
	}
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(goal))))
	return "goal:" + hex.EncodeToString(sum[:])[:10]
}

func inputOf(observation, data string) string {
	in := strings.TrimSpace(strings.TrimSpace(observation) + "\n" + data)
	if len(in) > 2000 {
		in = in[:2000]
	}
	return in
}

func optionalDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a duration like 6h", s)
	}
	return d, nil
}
