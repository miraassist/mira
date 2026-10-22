package threads

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// fakeActions is Home Assistant with a Show in the bedroom and an Echo
// without alarms in the living room.
type fakeActions struct {
	calls []string
	data  []map[string]any
}

func (f *fakeActions) Has(_ context.Context, action string) (bool, error) {
	return strings.HasPrefix(action, "esphome.show_"), nil
}
func (f *fakeActions) Call(_ context.Context, action string, data map[string]any) error {
	f.calls = append(f.calls, action)
	f.data = append(f.data, data)
	return nil
}
func (f *fakeActions) Place(_ context.Context, area string) map[string]string {
	device := map[string]string{"schlafzimmer": "show", "wohnzimmer": "wohnzimmer"}[area]
	return map[string]string{"area": area, "area_name": area, "satellite": "assist_satellite." + device, "satellite.device": device}
}

func wecker() skills.Skill {
	return skills.Skill{Name: "wecker", Description: "Wecker stellen", Body: "Frag nach der Zeit.",
		Target: &skills.Target{
			Fields: []skills.TargetField{
				{Name: "time", Description: "Uhrzeit"},
				{Name: "days", Description: "Tage", Optional: true},
			},
			Finish: skills.Finish{Name: skills.FinishAction, Action: &skills.Action{
				Action: "esphome.{{ satellite.device }}_alarm_set",
				Data:   map[string]any{"time": "{{ time }}", "days": "{{ days }}", "label": "Mira"}}},
			Undo: &skills.Action{Action: "esphome.{{ satellite.device }}_alarm_delete",
				Data: map[string]any{"time": "{{ time }}", "label": ""}},
		}}
}

func actionSetup(t *testing.T) (*Manager, *store.Store, *fakeActions) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ha := &fakeActions{}
	return New(Options{Lang: i18n.DE, Store: st, Targets: []skills.Skill{wecker()}, Actions: ha}), st, ha
}

func voiceIn(t *testing.T, st *store.Store, key, area string) tools.Invocation {
	t.Helper()
	if _, err := st.UpsertSession(context.Background(), store.Session{Key: key, Channel: string(config.ChannelVoice),
		ConversationID: key, AreaID: area}, time.Hour); err != nil {
		t.Fatal(err)
	}
	return tools.Invocation{SessionKey: key, Channel: config.ChannelVoice}
}

// "Weck mich morgen um 6" in the bedroom: no yes asked, the Show's alarm is
// set at once, an optional field may stay empty, and "nein, halb sieben"
// takes it back.
func TestActionFinishSetsAndUndoes(t *testing.T) {
	m, _, ha := actionSetup(t)
	ctx := context.Background()
	inv := voiceIn(t, m.opts.Store, "voice:a", "schlafzimmer")
	if _, err := m.Start(ctx, inv, "wecker", "Weck mich morgen um 6"); err != nil {
		t.Fatal(err)
	}
	out, err := m.Fill(ctx, inv, map[string]string{"time": "06:00"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Erledigt: esphome.show_alarm_set") || len(ha.calls) != 1 {
		t.Fatalf("out = %q, calls = %v", out, ha.calls)
	}
	if d := ha.data[0]; d["time"] != "06:00" || d["label"] != "Mira" || d["days"] != "" {
		t.Errorf("data = %v", d)
	}
	out, err = m.Undo(ctx, "area:schlafzimmer")
	if err != nil || !strings.HasPrefix(out, "Zurückgenommen") {
		t.Fatalf("undo: %q %v", out, err)
	}
	if ha.calls[1] != "esphome.show_alarm_delete" || ha.data[1]["time"] != "06:00" {
		t.Errorf("undo called %v %v", ha.calls[1], ha.data[1])
	}
	if out, _ := m.Undo(ctx, "area:schlafzimmer"); out != m.texts().nothingToUndo {
		t.Errorf("second undo: %q", out)
	}
}

// Where the satellite has no such action, or there is no satellite at all,
// Mira says so instead of calling anything.
func TestActionFinishWhereItCannot(t *testing.T) {
	m, st, ha := actionSetup(t)
	ctx := context.Background()
	inv := voiceIn(t, st, "voice:b", "wohnzimmer")
	m.Start(ctx, inv, "wecker", "Weck mich um 6")
	if out, _ := m.Fill(ctx, inv, map[string]string{"time": "06:00"}); !strings.Contains(out, "esphome.wohnzimmer_alarm_set") {
		t.Errorf("echo without alarms: %q", out)
	}
	chat := tools.Invocation{SessionKey: "telegram:7", Channel: config.ChannelTelegram}
	m.Start(ctx, chat, "wecker", "Weck mich um 6")
	if out, _ := m.Fill(ctx, chat, map[string]string{"time": "06:00"}); !strings.Contains(out, "nur an einem Satelliten") {
		t.Errorf("telegram: %q", out)
	}
	if len(ha.calls) != 0 {
		t.Errorf("called %v", ha.calls)
	}
}

// A known person's thread lives with them, not in the room (agenten.md, What a
// turn belongs to): the satellite an action names is the one of the turn that
// acts. "Weck mich" in the bedroom sets the Show's alarm; a yes said in the
// living room acts there, where the Echo has no alarms.
func TestActionFinishForAKnownPerson(t *testing.T) {
	m, st, ha := actionSetup(t)
	ctx := context.Background()
	bed := voiceIn(t, st, "voice:person:anna", "schlafzimmer")
	bed.Person, bed.Place, bed.Area = "anna", "person:anna", "schlafzimmer"
	m.Start(ctx, bed, "wecker", "Weck mich morgen um 6")
	out, err := m.Fill(ctx, bed, map[string]string{"time": "06:00"})
	if err != nil || !strings.HasPrefix(out, "Erledigt: esphome.show_alarm_set") {
		t.Fatalf("bedroom: %q %v, calls %v", out, err, ha.calls)
	}

	sk := wecker()
	sk.Target.Confirm = true
	m.targets["wecker"] = sk
	m.Start(ctx, bed, "wecker", "Weck mich übermorgen um 7")
	if out, _ := m.Fill(ctx, bed, map[string]string{"time": "07:00"}); !strings.Contains(out, "07:00") {
		t.Fatalf("proposal: %q", out)
	}
	living := bed
	living.Area = "wohnzimmer"
	if _, err := m.Confirm(ctx, living); err == nil || !strings.Contains(err.Error(), "esphome.wohnzimmer_alarm_set") {
		t.Errorf("yes in the living room: %v", err)
	}
	if len(ha.calls) != 1 {
		t.Errorf("called %v", ha.calls)
	}
}
