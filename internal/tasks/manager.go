package tasks

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

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/ha/structure"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
)

// Source is the part of Home Assistant the tasks read.
type Source interface {
	States(ctx context.Context) ([]haws.State, error)
	Statistics(ctx context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error)
}

// Assessor runs one request with a JSON answer: the conversation engine.
type Assessor interface {
	Assess(ctx context.Context, instruction string, schema json.RawMessage) (string, error)
}

type Options struct {
	Lang     i18n.Lang
	Store    *store.Store
	Source   Source
	Snapshot func() structure.Snapshot
	// Planner makes the plan; Judge rates candidates. Both may be the same.
	Planner, Judge Assessor
	QuietHours     config.QuietHours
	// Cooldown is the least time between two notices of the same rule.
	Cooldown time.Duration
	Clock    func() time.Time
	Location *time.Location
	Logger   *slog.Logger
	// Deliver, when set, makes planning run in the background: planning
	// reads the whole house and takes longer than a voice turn may. The
	// proposal is delivered to whoever asked, like a reminder.
	Deliver func(ctx context.Context, n store.Notice, areaID string) error
	// Persons are the configured persons, to address a proposal.
	Persons func() map[string]config.Person
}

type Manager struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// held remembers whether a state or threshold rule held at the last
	// check, so it fires when it starts to hold, not every five minutes.
	held map[string]bool
	// checkedHour is the last hour whose statistics were compared.
	checkedHour time.Time
}

func New(opts Options) *Manager {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Cooldown == 0 {
		opts.Cooldown = 6 * time.Hour
	}
	if opts.Snapshot == nil {
		opts.Snapshot = func() structure.Snapshot { return structure.Snapshot{} }
	}
	return &Manager{opts: opts, log: opts.Logger.With("component", "tasks"), held: map[string]bool{}}
}

func (m *Manager) texts() texts { return i18n.Pick(catalog, m.opts.Lang) }

// Create plans a task for a goal and stores it as proposed. The plan is
// checked against the house: entities must exist and suit their rule.
func (m *Manager) Create(ctx context.Context, goal, sessionKey string) (store.Task, Plan, error) {
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return store.Task{}, Plan{}, err
	}
	cat := Catalog(states, m.opts.Snapshot())
	instruction := m.planInstruction(goal, cat)
	var p Plan
	// A second attempt gets the reason the first plan was refused.
	for attempt := 0; ; attempt++ {
		raw, err := m.opts.Planner.Assess(ctx, instruction, json.RawMessage(planSchema))
		if err != nil {
			return store.Task{}, Plan{}, err
		}
		p = Plan{}
		err = json.Unmarshal([]byte(raw), &p)
		if err == nil {
			repair(&p, cat)
			err = Validate(p, cat)
		}
		if err == nil {
			break
		}
		if attempt == 1 {
			return store.Task{}, Plan{}, fmt.Errorf("no valid plan: %w", err)
		}
		m.log.Info("plan refused, planning again", "error", err)
		instruction = m.planInstruction(goal, cat) + fmt.Sprintf(m.texts().replan, err.Error())
	}
	planJSON, _ := json.Marshal(p)
	t := store.Task{ID: strings.ToLower(ulid.Make().String()[:10]), Goal: strings.TrimSpace(goal), Title: p.Title,
		Summary: p.Summary, Plan: string(planJSON), State: store.TaskProposed, SessionKey: sessionKey}
	if err := m.opts.Store.PutTask(ctx, t); err != nil {
		return store.Task{}, Plan{}, err
	}
	m.log.Info("task proposed", "task", t.ID, "title", p.Title, "rules", len(p.Rules))
	return t, p, nil
}

// Propose plans a task in the background and delivers the proposal, or
// the failure, to whoever asked: spoken in the room for a voice turn, on
// Telegram for a chat. ctx only carries values; planning outlives the turn.
func (m *Manager) Propose(ctx context.Context, goal string, inv tools.Invocation) {
	var area string
	if inv.Channel == config.ChannelVoice {
		if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil {
			area = sess.AreaID
		}
	}
	var persons map[string]config.Person
	if m.opts.Persons != nil {
		persons = m.opts.Persons()
	}
	audience := tools.AudienceFor("", inv, persons)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		t := m.texts()
		n := store.Notice{ID: ulid.Make().String(), Source: store.SourceTask, Importance: 2, Audience: audience}
		task, _, err := m.Create(ctx, goal, inv.SessionKey)
		if err != nil {
			m.log.Error("could not plan a task", "goal", goal, "error", err)
			n.DedupeKey = "task-plan:" + n.ID
			n.Spoken = fmt.Sprintf(t.planFailed, strings.TrimSpace(goal))
		} else {
			n.DedupeKey = "task-plan:" + task.ID
			n.Spoken = fmt.Sprintf(t.proposal, task.Title, task.Summary)
			n.Details = m.Describe(task)
		}
		if err := m.opts.Deliver(ctx, n, area); err != nil {
			m.log.Error("could not deliver a task proposal", "error", err)
		}
	}()
}

// Activate starts a proposed task; without an id, the newest proposed one.
func (m *Manager) Activate(ctx context.Context, id string) (store.Task, error) {
	if id == "" {
		list, err := m.opts.Store.Tasks(ctx)
		if err != nil {
			return store.Task{}, err
		}
		var newest store.Task
		for _, t := range list {
			if t.State == store.TaskProposed && t.CreatedAt.After(newest.CreatedAt) {
				newest = t
			}
		}
		if newest.ID == "" {
			return store.Task{}, store.ErrNotFound
		}
		id = newest.ID
	}
	t, err := m.opts.Store.GetTask(ctx, id)
	if err != nil {
		return store.Task{}, err
	}
	t.State = store.TaskActive
	if err := m.opts.Store.PutTask(ctx, t); err != nil {
		return store.Task{}, err
	}
	m.log.Info("task active", "task", t.ID, "title", t.Title)
	return t, nil
}

// Run checks the active tasks every five minutes until ctx ends.
func (m *Manager) Run(ctx context.Context) error {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		if err := m.Check(ctx); err != nil && ctx.Err() == nil {
			m.log.Error("task check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// Check runs every rule of every active task once and queues a notice for
// each candidate the judge finds worth it.
func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list, err := m.opts.Store.Tasks(ctx)
	if err != nil {
		return err
	}
	var active []store.Task
	for _, t := range list {
		if t.State == store.TaskActive {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return nil
	}
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return err
	}
	byID := map[string]haws.State{}
	for _, s := range states {
		byID[s.EntityID] = s
	}
	cat := map[string]Entity{}
	for _, e := range Catalog(states, structure.Snapshot{}) {
		cat[e.ID] = e
	}

	now := m.opts.Clock().In(m.opts.Location)
	// The hour that ended; statistics for it are complete a few minutes
	// after the hour.
	hour := now.Truncate(time.Hour).Add(-time.Hour)
	doHourly := now.Sub(hour.Add(time.Hour)) >= 15*time.Minute && hour.After(m.checkedHour)
	var stats map[string][]haws.StatRow
	if doHourly {
		var ids []string
		for _, t := range active {
			for _, r := range planOf(t).Rules {
				if r.Kind == KindUsage || r.Kind == KindLevel {
					ids = append(ids, r.EntityID)
				}
			}
		}
		if len(ids) > 0 {
			if stats, err = m.opts.Source.Statistics(ctx, ids, hour.AddDate(0, 0, -baselineDays), "hour"); err != nil {
				return fmt.Errorf("statistics: %w", err)
			}
		}
		m.checkedHour = hour
	}

	t := m.texts()
	for _, task := range active {
		plan := planOf(task)
		for i, r := range plan.Rules {
			e := cat[r.EntityID]
			if e.Name == "" {
				e.Name = r.EntityID
			}
			var evidence string
			switch r.Kind {
			case KindUsage, KindLevel:
				if !doHourly || !m.applies(r.When, hour) {
					continue
				}
				pick := func(s haws.StatRow) *float64 { return s.Change }
				if r.Kind == KindLevel {
					pick = func(s haws.StatRow) *float64 { return s.Mean }
				}
				current, base, ok := hourly(stats[r.EntityID], hour, m.opts.Location, pick)
				if !ok {
					continue
				}
				if r.Kind == KindUsage {
					if hit, s := usageCandidate(current, base); hit {
						evidence = usageEvidence(t, e, hour, current, s)
					}
				} else if hit, s := levelCandidate(current, base, r.Direction); hit {
					evidence = levelEvidence(t, e, hour, current, s)
				}
			case KindState, KindThreshold:
				st, ok := byID[r.EntityID]
				if !ok {
					continue
				}
				key := fmt.Sprintf("%s:%d", task.ID, i)
				var holds bool
				if r.Kind == KindState {
					holds = stateHolds(r, st.State)
				} else {
					holds, _ = thresholdHolds(r, st.State)
				}
				was := m.held[key]
				m.held[key] = holds
				if !holds || was || !m.applies(r.When, now) {
					continue
				}
				evidence = fmt.Sprintf(t.stateEvidence, e.Name, e.ID, st.State, e.Unit, now.Format("15:04"))
			}
			if evidence == "" {
				continue
			}
			if err := m.judge(ctx, task, plan, i, evidence); err != nil {
				m.log.Error("could not judge a candidate", "task", task.ID, "rule", i, "error", err)
			}
		}
	}
	return nil
}

// judge asks the model whether a candidate is worth a notice, and queues it.
func (m *Manager) judge(ctx context.Context, task store.Task, plan Plan, rule int, evidence string) error {
	last := lastAlerts(task)
	now := m.opts.Clock()
	if at, ok := last[rule]; ok && now.Sub(at) < m.opts.Cooldown {
		m.log.Info("candidate in cooldown", "task", task.ID, "rule", rule)
		return nil
	}
	t := m.texts()
	r := plan.Rules[rule]
	instruction := fmt.Sprintf(t.judge, task.Goal, task.Summary, r.Why, evidence) + t.judgeRules
	raw, err := m.opts.Judge.Assess(ctx, instruction, json.RawMessage(judgeSchema))
	if err != nil {
		return err
	}
	var v struct {
		Importance int    `json:"importance"`
		Spoken     string `json:"spoken"`
		Details    string `json:"details"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return fmt.Errorf("unparseable verdict: %w", err)
	}
	m.log.Info("candidate judged", "task", task.ID, "rule", rule, "importance", v.Importance)
	if v.Importance <= 0 || strings.TrimSpace(v.Spoken) == "" {
		return nil
	}
	if v.Importance > 3 {
		v.Importance = 3
	}
	details := strings.TrimSpace(v.Details)
	if details == "" {
		details = evidence
	}
	_, _, err = m.opts.Store.PutNotice(ctx, store.Notice{
		ID: ulid.Make().String(), Source: store.SourceTask,
		DedupeKey:  fmt.Sprintf("task:%s:%d", task.ID, rule),
		Importance: v.Importance, Audience: store.AudienceHousehold,
		Spoken: strings.TrimSpace(v.Spoken), Details: details,
		ExpiresAt: now.Add(12 * time.Hour),
	})
	if err != nil {
		return err
	}
	last[rule] = now
	raw2, _ := json.Marshal(last)
	task.LastAlert = string(raw2)
	return m.opts.Store.PutTask(ctx, task)
}

// applies reports whether a rule's time of day matches t.
func (m *Manager) applies(when string, t time.Time) bool {
	night := inQuietHours(m.opts.QuietHours, t.In(m.opts.Location))
	switch when {
	case WhenNight:
		return night
	case WhenDay:
		return !night
	default:
		return true
	}
}

func inQuietHours(q config.QuietHours, t time.Time) bool {
	from, to := q.From.MinutesOfDay(), q.To.MinutesOfDay()
	m := t.Hour()*60 + t.Minute()
	if from == to {
		return false
	}
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

func planOf(t store.Task) Plan {
	var p Plan
	_ = json.Unmarshal([]byte(t.Plan), &p)
	return p
}

func lastAlerts(t store.Task) map[int]time.Time {
	out := map[int]time.Time{}
	if t.LastAlert != "" {
		_ = json.Unmarshal([]byte(t.LastAlert), &out)
	}
	return out
}

// Delete removes a task.
func (m *Manager) Delete(ctx context.Context, id string) error {
	err := m.opts.Store.DeleteTask(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return err
	}
	m.log.Info("task deleted", "task", id)
	return err
}

// Describe renders a plan for the user: its summary and the rules.
func (m *Manager) Describe(t store.Task) string {
	tx := m.texts()
	var b strings.Builder
	fmt.Fprintf(&b, tx.describe, t.ID, t.Title, t.State, t.Summary)
	for _, r := range planOf(t).Rules {
		fmt.Fprintf(&b, "\n- %s (%s, %s): %s", r.EntityID, r.Kind, r.When, r.Why)
	}
	return b.String()
}
