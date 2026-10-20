package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/trace"
)

// Expect is what a step must lead to (docs/spec/evals.md): the result and
// the behaviour decide; the answer is reported.
type Expect struct {
	Result    []ResultCheck `yaml:"result"`
	Behaviour Behaviour     `yaml:"behaviour"`
	Answer    AnswerCheck   `yaml:"answer"`
}

// ResultCheck is one check of the state afterwards; exactly one is set.
type ResultCheck struct {
	EventExists   *EventCheck   `yaml:"event_exists"`
	CalendarEmpty *EventCheck   `yaml:"calendar_empty"`
	TodoItem      *TodoCheck    `yaml:"todo_item"`
	AlarmSet      *AlarmCheck   `yaml:"alarm_set"`
	FactSaved     *FactCheck    `yaml:"fact_saved"`
	MessageFor    *MessageCheck `yaml:"message_for"`
	EntityState   *StateCheck   `yaml:"entity_state"`
	TaskExists    *TaskCheck    `yaml:"task_exists"`
}

type EventCheck struct {
	Calendar string `yaml:"calendar"`
	Day      string `yaml:"day"`
	Start    string `yaml:"start"`
	Title    string `yaml:"title"`
}

type TodoCheck struct {
	List     string `yaml:"list"`
	Contains string `yaml:"contains"`
	Due      string `yaml:"due"`
}

type AlarmCheck struct {
	Device string `yaml:"device"`
	Time   string `yaml:"time"`
	Not    bool   `yaml:"not"`
}

type FactCheck struct {
	Owner    string `yaml:"owner"`
	Contains string `yaml:"contains"`
}

type MessageCheck struct {
	Receiver string `yaml:"receiver"`
	Contains string `yaml:"contains"`
}

// StateCheck wants an entity in a state; "opening|open" allows either.
type StateCheck struct {
	Entity string `yaml:"entity"`
	State  string `yaml:"state"`
}

type TaskCheck struct {
	Contains string `yaml:"contains"`
}

// Behaviour checks the turn's trace. An entry is "tool:NAME",
// "thread:TARGET[:STATE]", "action:NAME" or "blocked:NAME", matched as a
// prefix.
type Behaviour struct {
	Must      []string `yaml:"must"`
	MustNot   []string `yaml:"must_not"`
	MaxRounds int      `yaml:"max_rounds"`
}

// AnswerCheck checks what was said. An entry of Contains may give
// alternatives with "|".
type AnswerCheck struct {
	Contains    []string `yaml:"contains"`
	NotContains []string `yaml:"not_contains"`
}

// RunResult is one run of a scenario.
type RunResult struct {
	Passed   bool         `json:"passed"`
	AnswerOK bool         `json:"answer_ok"`
	Failures []string     `json:"failures,omitempty"`
	Answers  []string     `json:"answer_notes,omitempty"`
	Steps    []StepResult `json:"steps"`
}

type StepResult struct {
	Say     string   `json:"say"`
	Text    string   `json:"text"`
	Seconds float64  `json:"seconds"`
	ModelMs int64    `json:"model_ms"`
	Rounds  int      `json:"rounds"`
	Tokens  int      `json:"tokens"`
	Trace   []string `json:"trace"`
}

func (r RunResult) summary() string {
	state := "ok"
	if !r.Passed {
		state = "FAIL " + strings.Join(r.Failures, "; ")
	}
	var times []string
	for _, s := range r.Steps {
		times = append(times, fmt.Sprintf("%.1fs", s.Seconds))
	}
	return fmt.Sprintf("%s (%s)", state, strings.Join(times, ", "))
}

func (r *evalRun) runScenario(ctx context.Context, sc Scenario, run int) RunResult {
	var res RunResult
	fail := func(format string, args ...any) { res.Failures = append(res.Failures, fmt.Sprintf(format, args...)) }
	before, err := r.snapshot(ctx, sc)
	if err != nil {
		fail("snapshot: %v", err)
		return res
	}
	defer func() {
		if err := r.restore(ctx, before); err != nil {
			fmt.Printf("  cleanup: %v\n", err)
		}
	}()
	if err := r.setUp(ctx, sc); err != nil {
		fail("setup: %v", err)
		return res
	}
	conv := fmt.Sprintf("eval-%s-%d-%d", sc.Name, run, time.Now().Unix())
	answerOK := true
	for i, step := range sc.Steps {
		if step.At == "" && step.Channel == "" {
			// The fake Show (testenv/techo5_fake): evals run without hardware.
			step.At = "schlafzimmer"
		}
		t, err := r.say(ctx, step, conv)
		sr := StepResult{Say: step.Say, Text: t.Text, Seconds: t.Took.Seconds()}
		for _, e := range t.Trace {
			if e.Kind == trace.Round {
				sr.Rounds++
				sr.ModelMs += e.Ms
				sr.Tokens += e.Tokens
			}
			sr.Trace = append(sr.Trace, traceKey(e))
		}
		res.Steps = append(res.Steps, sr)
		if err != nil {
			fail("step %d: %v", i+1, err)
			break
		}
		limit := voiceLimit
		if step.Channel == "telegram" {
			limit = telegramLimit
		}
		if step.MaxSeconds > 0 {
			limit = time.Duration(step.MaxSeconds * float64(time.Second))
		}
		if t.Took > limit {
			fail("step %d took %.1fs, over %s", i+1, t.Took.Seconds(), limit)
		}
		for _, f := range checkBehaviour(step.Expect.Behaviour, sr) {
			fail("step %d: %s", i+1, f)
		}
		for _, c := range step.Expect.Result {
			if f := r.checkResult(ctx, c); f != "" {
				fail("step %d: %s", i+1, f)
			}
		}
		for _, f := range checkAnswer(step, t.Text) {
			answerOK = false
			res.Answers = append(res.Answers, fmt.Sprintf("step %d: %s", i+1, f))
		}
	}
	res.Passed = len(res.Failures) == 0
	res.AnswerOK = answerOK
	return res
}

func traceKey(e trace.Event) string {
	switch e.Kind {
	case trace.Thread:
		return "thread:" + e.Name + ":" + e.Detail
	case trace.Round:
		return fmt.Sprintf("round:%s:%dms", e.Name, e.Ms)
	}
	return e.Kind + ":" + e.Name
}

func checkBehaviour(b Behaviour, s StepResult) []string {
	var out []string
	has := func(want string) bool {
		for _, k := range s.Trace {
			if strings.HasPrefix(k, want) {
				return true
			}
		}
		return false
	}
	for _, m := range b.Must {
		if !has(m) {
			out = append(out, "missing "+m)
		}
	}
	for _, m := range b.MustNot {
		if has(m) {
			out = append(out, "must not "+m)
		}
	}
	if b.MaxRounds > 0 && s.Rounds > b.MaxRounds {
		out = append(out, fmt.Sprintf("%d model rounds, at most %d", s.Rounds, b.MaxRounds))
	}
	return out
}

var (
	markdown = regexp.MustCompile("\\*\\*|^#|`|^\\s*[-*] ")
	entityID = regexp.MustCompile(`\b(light|switch|sensor|binary_sensor|calendar|todo|cover|climate|assist_satellite|media_player)\.[a-z0-9_]+`)
)

func checkAnswer(step Step, text string) []string {
	var out []string
	low := strings.ToLower(text)
	for _, c := range step.Expect.Answer.Contains {
		ok := false
		for _, alt := range strings.Split(c, "|") {
			if strings.Contains(low, strings.ToLower(strings.TrimSpace(alt))) {
				ok = true
			}
		}
		if !ok {
			out = append(out, fmt.Sprintf("answer lacks %q", c))
		}
	}
	for _, c := range step.Expect.Answer.NotContains {
		if strings.Contains(low, strings.ToLower(c)) {
			out = append(out, fmt.Sprintf("answer has %q", c))
		}
	}
	if step.Channel != "telegram" {
		for _, line := range strings.Split(text, "\n") {
			if markdown.MatchString(line) {
				out = append(out, "Markdown in a spoken answer")
				break
			}
		}
		if entityID.MatchString(text) {
			out = append(out, "entity id in a spoken answer")
		}
	}
	return out
}

// day turns "+3" into the date three days from today.
func day(rel string) (time.Time, error) {
	n := 0
	if rel != "" {
		v, err := strconv.Atoi(strings.TrimPrefix(rel, "+"))
		if err != nil {
			return time.Time{}, fmt.Errorf("day %q is not like +3", rel)
		}
		n = v
	}
	y, m, d := time.Now().AddDate(0, 0, n).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local), nil
}

type calEvent struct {
	Summary string            `json:"summary"`
	UID     string            `json:"uid"`
	Start   map[string]string `json:"start"`
}

func (r *evalRun) events(ctx context.Context, calendar string, from, to time.Time) ([]calEvent, error) {
	var out []calEvent
	path := fmt.Sprintf("/api/calendars/%s?start=%s&end=%s", calendar,
		url.QueryEscape(from.Format(time.RFC3339)), url.QueryEscape(to.Format(time.RFC3339)))
	err := r.ha(ctx, "GET", path, nil, &out)
	return out, err
}

func (r *evalRun) checkResult(ctx context.Context, c ResultCheck) string {
	switch {
	case c.EventExists != nil, c.CalendarEmpty != nil:
		ec, want := c.EventExists, true
		if ec == nil {
			ec, want = c.CalendarEmpty, false
		}
		d, err := day(ec.Day)
		if err != nil {
			return err.Error()
		}
		evs, err := r.events(ctx, ec.Calendar, d, d.AddDate(0, 0, 1))
		if err != nil {
			return err.Error()
		}
		found := false
		for _, e := range evs {
			start := e.Start["dateTime"]
			if ec.Title != "" && !strings.Contains(strings.ToLower(e.Summary), strings.ToLower(ec.Title)) {
				continue
			}
			if ec.Start != "" && !strings.Contains(start, "T"+ec.Start) {
				continue
			}
			found = true
		}
		if want && !found {
			return fmt.Sprintf("no event %q at %s %s in %s", ec.Title, ec.Day, ec.Start, ec.Calendar)
		}
		if !want && found {
			return fmt.Sprintf("event %q still in %s on %s", ec.Title, ec.Calendar, ec.Day)
		}
	case c.TodoItem != nil:
		items, err := r.todoItems(ctx, c.TodoItem.List)
		if err != nil {
			return err.Error()
		}
		for _, it := range items {
			if !strings.Contains(strings.ToLower(it.Summary), strings.ToLower(c.TodoItem.Contains)) {
				continue
			}
			if c.TodoItem.Due != "" {
				d, err := day(c.TodoItem.Due)
				if err != nil {
					return err.Error()
				}
				if !strings.HasPrefix(it.Due, d.Format("2006-01-02")) {
					return fmt.Sprintf("%q is due %q, not %s", it.Summary, it.Due, d.Format("2006-01-02"))
				}
			}
			return ""
		}
		return fmt.Sprintf("no item %q on %s", c.TodoItem.Contains, c.TodoItem.List)
	case c.AlarmSet != nil:
		alarms, err := r.alarms(ctx, c.AlarmSet.Device)
		if err != nil {
			return err.Error()
		}
		found := false
		for _, a := range alarms {
			if a.Time == c.AlarmSet.Time {
				found = true
			}
		}
		if found == c.AlarmSet.Not {
			if c.AlarmSet.Not {
				return fmt.Sprintf("an alarm at %s is still on %s", c.AlarmSet.Time, c.AlarmSet.Device)
			}
			return fmt.Sprintf("no alarm at %s on %s", c.AlarmSet.Time, c.AlarmSet.Device)
		}
	case c.FactSaved != nil:
		var mem struct {
			Memories []struct {
				Owner string `json:"owner"`
				Facts []struct {
					Text string `json:"text"`
				} `json:"facts"`
			} `json:"memories"`
		}
		if err := r.agent(ctx, "GET", "/v1/ui/memory", nil, &mem); err != nil {
			return err.Error()
		}
		for _, m := range mem.Memories {
			if m.Owner != c.FactSaved.Owner {
				continue
			}
			for _, f := range m.Facts {
				if strings.Contains(strings.ToLower(f.Text), strings.ToLower(c.FactSaved.Contains)) {
					return ""
				}
			}
		}
		return fmt.Sprintf("no fact %q in the memory of %s", c.FactSaved.Contains, c.FactSaved.Owner)
	case c.MessageFor != nil:
		var msgs struct {
			Items []map[string]any `json:"items"`
		}
		if err := r.agent(ctx, "GET", "/v1/ui/messages", nil, &msgs); err != nil {
			return err.Error()
		}
		for _, m := range msgs.Items {
			if fmt.Sprint(m["receiver"]) == c.MessageFor.Receiver &&
				strings.Contains(strings.ToLower(fmt.Sprint(m["spoken"])), strings.ToLower(c.MessageFor.Contains)) {
				return ""
			}
		}
		return fmt.Sprintf("no message for %s with %q", c.MessageFor.Receiver, c.MessageFor.Contains)
	case c.EntityState != nil:
		var st struct {
			State string `json:"state"`
		}
		if err := r.ha(ctx, "GET", "/api/states/"+c.EntityState.Entity, nil, &st); err != nil {
			return err.Error()
		}
		if !slices.Contains(strings.Split(c.EntityState.State, "|"), st.State) {
			return fmt.Sprintf("%s is %s, not %s", c.EntityState.Entity, st.State, c.EntityState.State)
		}
	case c.TaskExists != nil:
		var tpl string
		if err := r.ha(ctx, "POST", "/api/template", map[string]any{"template": `{% for e in label_entities('mira') %}{{ state_attr(e, 'friendly_name') }};{% endfor %}`}, &tpl); err != nil {
			return err.Error()
		}
		if !strings.Contains(strings.ToLower(tpl), strings.ToLower(c.TaskExists.Contains)) {
			return fmt.Sprintf("no task like %q (have %s)", c.TaskExists.Contains, tpl)
		}
	default:
		return "empty result check"
	}
	return ""
}
