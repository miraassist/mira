package timer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

type call struct {
	name   string
	data   map[string]any
	device string
}

type fakeIntents struct {
	calls  []call
	result IntentResult
	err    error
}

func (f *fakeIntents) Handle(ctx context.Context, name string, data map[string]any, device, lang string) (IntentResult, error) {
	f.calls = append(f.calls, call{name, data, device})
	return f.result, f.err
}

func countdownTool(in *fakeIntents, device string) tools.InternalTool {
	for _, tl := range Tools(Options{
		Lang: i18n.DE, Clock: func() time.Time { return now }, Location: time.UTC, HA: &fakeHA{}, Intents: in,
		Players: func(string) []string { return nil }, Lights: func(string) []string { return nil },
		Area: func(string) string { return "" }, AreaName: func(s string) string { return s },
		DeviceOf: func(ctx context.Context, key string) string { return device },
	}) {
		if tl.Name == ToolTimer {
			return tl
		}
	}
	panic("no timer tool")
}

var voice = tools.Invocation{SessionKey: "voice:1", Area: "kueche"}

func TestStartRunsOnTheSatelliteThatHeardIt(t *testing.T) {
	in := &fakeIntents{}
	out := run(t, countdownTool(in, "sat-kueche"), voice, `{"action":"start","minutes":10,"name":"Nudeln"}`)
	if out != "Timer gestellt: Nudeln, 10 Minuten." {
		t.Errorf("answer: %q", out)
	}
	if len(in.calls) != 1 || in.calls[0].name != "HassStartTimer" || in.calls[0].device != "sat-kueche" ||
		in.calls[0].data["minutes"] != 10 || in.calls[0].data["name"] != "Nudeln" || len(in.calls[0].data) != 2 {
		t.Errorf("calls: %+v", in.calls)
	}
	// Hours, minutes and seconds are said the way a person says them.
	if out := run(t, countdownTool(in, "s"), voice, `{"action":"start","hours":1,"minutes":1,"seconds":30}`); out != "Timer gestellt: 1 Stunde 1 Minute 30 Sekunden." {
		t.Errorf("long: %q", out)
	}
}

func TestEachActionIsOneIntent(t *testing.T) {
	for args, want := range map[string]string{
		`{"action":"add","minutes":5}`:        "HassIncreaseTimer",
		`{"action":"remove","seconds":30}`:    "HassDecreaseTimer",
		`{"action":"pause","name":"Nudeln"}`:  "HassPauseTimer",
		`{"action":"resume"}`:                 "HassUnpauseTimer",
		`{"action":"cancel","name":"Nudeln"}`: "HassCancelTimer",
		`{"action":"cancel","all":true}`:      "HassCancelAllTimers",
		`{"action":"status"}`:                 "HassTimerStatus",
	} {
		in := &fakeIntents{result: IntentResult{Canceled: 2}}
		out := run(t, countdownTool(in, "sat"), voice, args)
		if len(in.calls) != 1 || in.calls[0].name != want {
			t.Errorf("%s: calls %+v, want %s", args, in.calls, want)
		}
		if strings.Contains(args, `"all"`) && out != "2 Timer abgebrochen." {
			t.Errorf("cancel all: %q", out)
		}
	}
}

func TestStatusSaysWhatIsLeft(t *testing.T) {
	in := &fakeIntents{result: IntentResult{Timers: []HATimer{
		{Name: "Nudeln", IsActive: true, TotalSecondsLeft: 570},
		{Name: "", IsActive: false, TotalSecondsLeft: 3725},
	}}}
	out := run(t, countdownTool(in, "sat"), voice, `{"action":"status"}`)
	want := "- Nudeln: noch 9 Minuten 30 Sekunden\n- –: noch 1 Stunde 2 Minuten 5 Sekunden (angehalten)"
	if out != want {
		t.Errorf("status:\n%s\nwant:\n%s", out, want)
	}
	if out := run(t, countdownTool(&fakeIntents{}, "sat"), voice, `{"action":"status"}`); out != "Es läuft kein Timer." {
		t.Errorf("none: %q", out)
	}
}

func TestWhatHomeAssistantRefusesIsSaidToTheModel(t *testing.T) {
	for speech, want := range map[string]string{
		"Multiple timers matched":           "mehrere Timer",
		"Timer not found":                   "Einen solchen Timer gibt es nicht",
		"Device does not support timers: x": "keinen Lautsprecher mit Timern",
		"Something else broke":              "Something else broke",
	} {
		in := &fakeIntents{result: IntentResult{Error: speech}}
		if out := run(t, countdownTool(in, "sat"), voice, `{"action":"cancel"}`); !strings.Contains(out, want) {
			t.Errorf("%q -> %q, want %q", speech, out, want)
		}
	}
	if out := run(t, countdownTool(&fakeIntents{err: errors.New("down")}, "sat"), voice, `{"action":"status"}`); !strings.Contains(out, "Der Timer geht nicht: down") {
		t.Errorf("transport error: %q", out)
	}
}

func TestNoSatelliteNoTimer(t *testing.T) {
	in := &fakeIntents{}
	if out := run(t, countdownTool(in, ""), tools.Invocation{SessionKey: "telegram:1"}, `{"action":"start","minutes":5}`); !strings.Contains(out, "keinen Lautsprecher mit Timern") {
		t.Errorf("answer: %q", out)
	}
	if out := run(t, countdownTool(in, "sat"), voice, `{"action":"start"}`); !strings.Contains(out, "braucht es eine Dauer") {
		t.Errorf("no duration: %q", out)
	}
	if len(in.calls) != 0 {
		t.Errorf("called anyway: %+v", in.calls)
	}
}

// The real answers of Home Assistant, as /api/intent/handle gives them.
func TestHTTPIntentsParsesWhatHomeAssistantAnswers(t *testing.T) {
	answers := map[string]string{
		"HassTimerStatus":     `{"speech":{},"card":{},"language":"de","response_type":"action_done","speech_slots":{"timers":[{"id":"01","name":"Pasta","device_id":"d","is_active":true,"total_seconds_left":58}]},"data":{"success":[],"failed":[]}}`,
		"HassCancelAllTimers": `{"speech":{},"card":{},"language":"de","response_type":"action_done","speech_slots":{"canceled":2},"data":{"success":[],"failed":[]}}`,
		"HassCancelTimer":     `{"speech":{"plain":{"speech":"Timer not found","extra_data":null}},"card":{},"language":"de","response_type":"error","data":{"code":"failed_to_handle"}}`,
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/intent/handle" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(answers[got["name"].(string)]))
	}))
	defer srv.Close()
	h := HTTPIntents{URL: srv.URL + "/", Token: "tok"}
	ctx := context.Background()

	res, err := h.Handle(ctx, "HassTimerStatus", map[string]any{"name": "Pasta"}, "dev-1", "de")
	if err != nil || len(res.Timers) != 1 || res.Timers[0].Name != "Pasta" || res.Timers[0].TotalSecondsLeft != 58 || !res.Timers[0].IsActive {
		t.Fatalf("status: %+v %v", res, err)
	}
	if got["device_id"] != "dev-1" || got["language"] != "de" || got["data"].(map[string]any)["name"] != "Pasta" {
		t.Errorf("request: %v", got)
	}
	if res, _ := h.Handle(ctx, "HassCancelAllTimers", nil, "dev-1", "de"); res.Canceled != 2 {
		t.Errorf("canceled: %+v", res)
	}
	if res, _ := h.Handle(ctx, "HassCancelTimer", nil, "dev-1", "de"); res.Error != "Timer not found" {
		t.Errorf("error: %+v", res)
	}
	if _, err := (HTTPIntents{URL: srv.URL, Token: "wrong"}).Handle(ctx, "HassTimerStatus", nil, "d", "de"); err == nil {
		t.Error("a rejected token is not an error")
	}
}
