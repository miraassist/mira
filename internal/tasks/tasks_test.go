package tasks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
)

func f(v float64) *float64 { return &v }

func TestUsageCandidate(t *testing.T) {
	// Water at three in the morning: usually nothing, now 40 litres.
	if hit, _ := usageCandidate(40, []float64{0, 0, 0, 0, 0, 0, 0}); !hit {
		t.Error("use where there usually is none is not a candidate")
	}
	// Evening: 80 litres are normal.
	if hit, _ := usageCandidate(90, []float64{60, 80, 70, 95, 75, 85, 65}); hit {
		t.Error("ordinary evening use is a candidate")
	}
	if hit, _ := usageCandidate(400, []float64{60, 80, 70, 95, 75, 85, 65}); !hit {
		t.Error("four times the usual is not a candidate")
	}
	// Learning: too few earlier days.
	if hit, _ := usageCandidate(400, []float64{0, 0}); hit {
		t.Error("candidate while learning")
	}
	if hit, _ := levelCandidate(31, []float64{21, 22, 21.5, 22, 21, 20.5, 22}, "high"); !hit {
		t.Error("31 degrees against 21 is not high")
	}
	if hit, _ := levelCandidate(31, []float64{21, 22, 21.5, 22, 21, 20.5, 22}, "low"); hit {
		t.Error("high value counted as low")
	}
}

var catalogFixture = []Entity{
	{ID: "sensor.wasser", Name: "Wasserzähler", StateClass: "total_increasing", Unit: "L", State: "32117"},
	{ID: "sensor.temp", Name: "Temperatur", StateClass: "measurement", Unit: "°C", State: "21"},
	{ID: "binary_sensor.tuer", Name: "Haustür", Domain: "binary_sensor", State: "off"},
}

func TestValidate(t *testing.T) {
	ok := Plan{Title: "Wasser", Summary: "Ich achte auf den Wasserzähler.", Rules: []Rule{
		{EntityID: "sensor.wasser", Kind: KindUsage, When: WhenNight, Why: "Leck"},
		{EntityID: "binary_sensor.tuer", Kind: KindState, When: WhenNight, States: []string{"on"}, Why: "offen"},
	}}
	if err := Validate(ok, catalogFixture); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]Rule{
		"unknown entity":    {EntityID: "sensor.nope", Kind: KindUsage, When: WhenAlways},
		"usage on a level":  {EntityID: "sensor.temp", Kind: KindUsage, When: WhenAlways},
		"level on counter":  {EntityID: "sensor.wasser", Kind: KindLevel, When: WhenAlways, Direction: "high"},
		"state, no states":  {EntityID: "binary_sensor.tuer", Kind: KindState, When: WhenAlways},
		"bad when":          {EntityID: "sensor.wasser", Kind: KindUsage, When: "sometimes"},
		"threshold, no lim": {EntityID: "sensor.temp", Kind: KindThreshold, When: WhenAlways},
	} {
		p := ok
		p.Rules = []Rule{r}
		if err := Validate(p, catalogFixture); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

type fakeSource struct {
	states []haws.State
	stats  map[string][]haws.StatRow
}

func (s *fakeSource) States(context.Context) ([]haws.State, error) { return s.states, nil }
func (s *fakeSource) Statistics(_ context.Context, ids []string, _ time.Time, _ string) (map[string][]haws.StatRow, error) {
	return s.stats, nil
}

type fakeModel struct {
	answers      []string
	instructions []string
}

func (m *fakeModel) Assess(_ context.Context, instruction string, _ json.RawMessage) (string, error) {
	m.instructions = append(m.instructions, instruction)
	a := m.answers[0]
	if len(m.answers) > 1 {
		m.answers = m.answers[1:]
	}
	return a, nil
}

func setup(t *testing.T, now *time.Time, model *fakeModel) (*Manager, *store.Store, *fakeSource) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src := &fakeSource{states: []haws.State{
		{EntityID: "sensor.wasser", State: "32117", Attributes: map[string]any{"friendly_name": "Wasserzähler", "state_class": "total_increasing", "unit_of_measurement": "L"}},
		{EntityID: "binary_sensor.tuer", State: "off", Attributes: map[string]any{"friendly_name": "Haustür"}},
	}}
	m := New(Options{Lang: i18n.DE, Store: st, Source: src, Planner: model, Judge: model,
		QuietHours: config.QuietHours{From: config.ClockTime{Hour: 23, Minute: 30}, To: config.ClockTime{Hour: 6, Minute: 30}},
		Clock:      func() time.Time { return *now }, Location: time.UTC})
	return m, st, src
}

const waterPlan = `{"title":"Wasser","summary":"Ich achte nachts auf den Wasserzähler und auf die Haustür.","rules":[
{"entity_id":"sensor.wasser","kind":"usage","when":"night","why":"Ein Leck fällt nachts auf."},
{"entity_id":"binary_sensor.tuer","kind":"state","when":"night","states":["on"],"why":"Nachts sollte die Tür zu sein."}]}`

func TestCreateConfirmCheck(t *testing.T) {
	now := time.Date(2026, 10, 15, 3, 20, 0, 0, time.UTC)
	model := &fakeModel{answers: []string{waterPlan}}
	m, st, src := setup(t, &now, model)

	task, _, err := m.Create(context.Background(), "Achte auf den Wasserverbrauch", "telegram:1", "telegram:1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.instructions[0], "sensor.wasser | Wasserzähler") {
		t.Errorf("catalog missing from plan prompt:\n%s", model.instructions[0])
	}
	// Proposed tasks are not checked.
	if err := m.Check(context.Background()); err != nil || len(model.instructions) != 1 {
		t.Fatalf("proposed task checked: %v", err)
	}
	if _, err := m.Activate(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}

	// 02:00-03:00: 40 litres; the same hour on the last seven days: nothing.
	hour := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	var rows []haws.StatRow
	for d := 7; d >= 1; d-- {
		rows = append(rows, haws.StatRow{Start: hour.AddDate(0, 0, -d), Change: f(0)})
	}
	rows = append(rows, haws.StatRow{Start: hour, Change: f(40)})
	src.stats = map[string][]haws.StatRow{"sensor.wasser": rows}
	model.answers = []string{`{"importance":3,"spoken":"Nachts sind 40 Liter Wasser gelaufen.","details":""}`}

	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	judged := model.instructions[len(model.instructions)-1]
	if !strings.Contains(judged, "um 40 L gestiegen") || !strings.Contains(judged, "Achte auf den Wasserverbrauch") {
		t.Errorf("judge got:\n%s", judged)
	}
	notices, _ := st.ListNotices(context.Background(), store.NoticeFilter{States: []string{store.NoticePending}})
	if len(notices) != 1 || notices[0].Importance != 3 || notices[0].Source != store.SourceTask || !strings.Contains(notices[0].Details, "40 L") {
		t.Fatalf("notices: %+v", notices)
	}

	// The same hour is not compared twice; the door opening is a new
	// candidate, judged harmless.
	model.answers = []string{`{"importance":0,"spoken":""}`}
	src.states[1].State = "on"
	now = now.Add(5 * time.Minute)
	m.Check(context.Background())
	if n := len(model.instructions); !strings.Contains(model.instructions[n-1], "Haustür (binary_sensor.tuer) ist jetzt on") {
		t.Errorf("door not judged: %s", model.instructions[n-1])
	}
	before := len(model.instructions)
	now = now.Add(5 * time.Minute)
	m.Check(context.Background()) // still open: no new candidate
	if len(model.instructions) != before {
		t.Error("an open door is judged every five minutes")
	}
	// By day the night rules are quiet.
	src.states[1].State = "off"
	m.Check(context.Background())
	now = time.Date(2026, 10, 15, 12, 20, 0, 0, time.UTC)
	src.states[1].State = "on"
	m.Check(context.Background())
	if len(model.instructions) != before {
		t.Error("night rule fired by day")
	}
}

func TestCooldown(t *testing.T) {
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	model := &fakeModel{answers: []string{`{"importance":2,"spoken":"Tür offen."}`}}
	m, st, _ := setup(t, &now, model)
	task := store.Task{ID: "t1", Goal: "x", Title: "x", Summary: "x", Plan: waterPlan, State: store.TaskActive}
	st.PutTask(context.Background(), task)
	plan := planOf(task)
	m.judge(context.Background(), task, plan, 1, "Tür offen")
	task, _ = st.GetTask(context.Background(), "t1")
	now = now.Add(time.Hour)
	m.judge(context.Background(), task, plan, 1, "Tür offen")
	if len(model.instructions) != 1 {
		t.Errorf("judged again within the cooldown: %d", len(model.instructions))
	}
}

func TestInvalidPlanIsRefused(t *testing.T) {
	now := time.Now()
	// Refused twice.
	model := &fakeModel{answers: []string{`{"title":"x","summary":"y","rules":[{"entity_id":"sensor.ausgedacht","kind":"usage","when":"always","why":"z"}]}`}}
	m, st, _ := setup(t, &now, model)
	if _, _, err := m.Create(context.Background(), "Pass auf", "s", "s"); err == nil || !strings.Contains(err.Error(), "unknown entity") {
		t.Errorf("invented entity accepted: %v", err)
	}
	if list, _ := st.Tasks(context.Background()); len(list) != 0 {
		t.Error("invalid plan stored")
	}
}

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

func TestRepairAndReplan(t *testing.T) {
	now := time.Now()
	// First a counter rule on a measurement (repaired) plus an invented
	// entity (refused), then a valid plan.
	model := &fakeModel{answers: []string{
		`{"title":"x","summary":"y","rules":[{"entity_id":"sensor.ausgedacht","kind":"usage","when":"always","why":"z"}]}`,
		`{"title":"Strom","summary":"Ich achte auf den Zähler.","rules":[{"entity_id":"sensor.wasser","kind":"level","why":"z"}]}`,
	}}
	m, _, _ := setup(t, &now, model)
	_, p, err := m.Create(context.Background(), "Pass auf", "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rules[0].Kind != KindUsage || p.Rules[0].When != WhenAlways {
		t.Errorf("not repaired: %+v", p.Rules[0])
	}
	if !strings.Contains(model.instructions[1], "unknown entity sensor.ausgedacht") {
		t.Error("second attempt did not get the reason")
	}
}

func TestProposeInBackground(t *testing.T) {
	now := time.Now()
	model := &fakeModel{answers: []string{waterPlan}}
	m, st, _ := setup(t, &now, model)
	delivered := make(chan store.Notice, 1)
	m.opts.Deliver = func(_ context.Context, n store.Notice, area string) error {
		delivered <- n
		return nil
	}
	tool := m.Tools()[0]
	out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"goal":"Achte auf das Wasser"}`), SessionKey: "telegram:1", Channel: config.ChannelTelegram})
	if err != nil || !strings.Contains(out, "Hintergrund") {
		t.Fatalf("create: %q %v", out, err)
	}
	select {
	case n := <-delivered:
		if !strings.Contains(n.Spoken, "Wasser") || !strings.Contains(n.Spoken, "Soll ich so vorgehen?") || n.Source != store.SourceTask {
			t.Errorf("proposal: %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no proposal delivered")
	}
	// A yes from elsewhere does not start it; one from the same chat does,
	// and only once.
	confirm := m.Tools()[1]
	if _, err := confirm.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{}`), SessionKey: "telegram:2", Channel: config.ChannelTelegram}); err == nil {
		t.Error("confirmed from another chat")
	}
	out, err = confirm.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"id":"ex3"}`), SessionKey: "telegram:1", Channel: config.ChannelTelegram})
	if err != nil || !strings.Contains(out, "Wasser") {
		t.Fatalf("confirm: %q %v", out, err)
	}
	if list, _ := st.Tasks(context.Background()); len(list) != 1 || list[0].State != store.TaskActive {
		t.Errorf("tasks: %+v", list)
	}
	if _, err := confirm.Run(context.Background(), tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}); err == nil {
		t.Error("confirmed twice")
	}
	_ = st
}

func TestShortcutConfirmsPlainYes(t *testing.T) {
	now := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	model := &fakeModel{answers: []string{waterPlan}}
	m, st, _ := setup(t, &now, model)
	if _, _, err := m.Create(context.Background(), "Achte auf das Wasser", "telegram:1", "telegram:1"); err != nil {
		t.Fatal(err)
	}
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	if _, ok := m.Shortcut(context.Background(), inv, "Nimm die Tür raus."); ok {
		t.Error("a change request was taken as yes")
	}
	if _, ok := m.Shortcut(context.Background(), tools.Invocation{SessionKey: "telegram:2", Channel: config.ChannelTelegram}, "Ja"); ok {
		t.Error("a yes from another chat confirmed")
	}
	now = now.Add(11 * time.Minute)
	if _, ok := m.Shortcut(context.Background(), inv, "Ja."); ok {
		t.Error("a stale proposal was confirmed")
	}
	now = now.Add(-5 * time.Minute)
	if _, ok := m.Shortcut(context.Background(), inv, "Ja, aber ohne die Tür"); ok {
		t.Error("a yes with a change was handled")
	}
	out, ok := m.Shortcut(context.Background(), inv, " Ja, passt! ")
	if !ok || !strings.Contains(out, "Wasser") {
		t.Fatalf("plain yes not handled: %q", out)
	}
	if list, _ := st.Tasks(context.Background()); list[0].State != store.TaskActive {
		t.Error("not active")
	}
	if _, ok := m.Shortcut(context.Background(), inv, "Ja"); ok {
		t.Error("a second yes did something")
	}
}
