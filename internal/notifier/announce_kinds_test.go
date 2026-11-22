package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// The announcement knows the kinds (docs/spec/messages.md, step 6).

const deviceAnsage = "Nenn die Geräte mit ihrem Namen und biete an, sie einzubinden: alle oder nur einzelne."

// foundDevice puts a found device the way the code does: its thread of
// geraet-einbinden at the house and its entry in the house's device message.
func (e *env) foundDevice(thread, name string) {
	e.t.Helper()
	ctx := context.Background()
	th := &store.Thread{ID: thread, Target: "geraet-einbinden", Place: store.ReceiverHouse, State: "collecting",
		Request: name, Content: json.RawMessage(`{"plan":"# Neues Gerät\n- [ ] Einbinden\n- [ ] Raum\n- [ ] Name"}`),
		ExpiresAt: e.clk.Now().Add(24 * time.Hour)}
	if err := e.st.PutThread(ctx, th); err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.st.PutMessage(ctx, store.Message{ID: "m-" + thread, Kind: store.KindDevice, Source: store.SourceDevices,
		DedupeKey: "devices:" + thread, Importance: 2, Receiver: store.ReceiverHouse,
		Spoken: "Neues Gerät gefunden: " + name, Details: "device:id-" + thread + ": " + name + " (Tuya Smoke Detector)",
		ThreadID: thread, ExpiresAt: e.clk.Now().Add(24 * time.Hour)}); err != nil {
		e.t.Fatal(err)
	}
}

// withAnsage gives the notifier the skills' instructions and records what
// the model was asked.
func (e *env) withAnsage(asked *[]string) {
	e.n.instruction = func(_ context.Context, kind string) (string, error) {
		if kind == store.KindDevice {
			return deviceAnsage, nil
		}
		return "", errors.New("no skill")
	}
	e.n.composer = func(ctx context.Context, instruction string, schema json.RawMessage) (string, error) {
		*asked = append(*asked, instruction)
		return echoComposer(ctx, instruction, schema)
	}
}

func TestMS37_AFoundDevicesAnnouncementOffersToAddIt(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	// Given a device message with one found device and its thread
	e.foundDevice("th-dg", "Smart Smoke Detector DG")
	// When it is announced
	out := e.presence("bad")
	if len(out.Announced) != 1 || len(asked) != 1 {
		t.Fatalf("announced %v, model asked %d times", out.Announced, len(asked))
	}
	// Then the model words it with the message's kind, entry and thread and
	// the section "Ansage" of geraet-einbinden
	for _, want := range []string{"Art device", "Neues Gerät gefunden: Smart Smoke Detector DG",
		"Vorgang th-dg, Skill geraet-einbinden, Plan: # Neues Gerät - [ ] Einbinden - [ ] Raum - [ ] Name",
		"Details: device:id-th-dg: Smart Smoke Detector DG (Tuya Smoke Detector)",
		"Art device (Skill geraet-einbinden):\n" + deviceAnsage, "folge ihr: Sie sagt, was die Nachricht bedeutet und was du anbietest"} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the model was not told %q:\n%s", want, asked[0])
		}
	}
	// And the announcement is the model's answer
	if got := e.ha.calls[0].Data["start_message"]; got != "Neues Gerät gefunden: Smart Smoke Detector DG." {
		t.Errorf("spoken %q", got)
	}
}

func TestMS38_SeveralFoundDevicesAreOfferedAllOrOneByOne(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	// Given a device message with three found devices, each with its thread
	e.foundDevice("th-1", "Rauchmelder DG")
	e.foundDevice("th-2", "Rauchmelder KG")
	e.foundDevice("th-3", "Bewegungsmelder")
	// When it is announced
	e.presence("bad")
	// Then the model gets all three entries with their threads in one request
	if len(asked) != 1 {
		t.Fatalf("model asked %d times", len(asked))
	}
	for _, th := range []string{"th-1", "th-2", "th-3"} {
		if !strings.Contains(asked[0], "Vorgang "+th+", Skill geraet-einbinden") {
			t.Errorf("thread %s missing:\n%s", th, asked[0])
		}
	}
	// And the instruction asks to offer adding them all or the ones named,
	// once for the kind
	if strings.Count(asked[0], deviceAnsage) != 1 {
		t.Errorf("the kind's instruction is not there once:\n%s", asked[0])
	}
}

func TestMS39_ASingleSimpleEntryIsWordedToo(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	// Given a simple message with one entry
	e.message("w", 2, func(m *store.Message) { m.Spoken = "Die Waschmaschine ist fertig" })
	// When it is announced, then the model words it and its answer is said
	e.presence("bad")
	if len(asked) != 1 || !strings.Contains(asked[0], "Art simple") {
		t.Fatalf("model asked %d times: %v", len(asked), asked)
	}
	if strings.Contains(asked[0], "Anweisungen für die Arten") {
		t.Errorf("a simple message has no kind instruction:\n%s", asked[0])
	}
}

func TestMS40_AFailedWordingSaysTheSentenceOfAFailedModel(t *testing.T) {
	for name, c := range map[string]struct {
		answer string
		err    error
		say    string
	}{
		"no JSON":     {answer: "Neues Gerät gefunden.", say: "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal."},
		"no answer":   {err: errors.New("dial tcp: connection refused"), say: "Ich erreiche mein KI-Modell gerade nicht."},
		"key refused": {err: &llm.Error{StatusCode: 401, Body: "bad key"}, say: "Mein KI-Modell lehnt meinen Schlüssel ab. Das muss auf meiner Seite unter Modelle behoben werden."},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, nil)
			e.clk.Set(at("12:00"))
			var asked []string
			e.withAnsage(&asked)
			// Given a device message waits
			e.foundDevice("th-dg", "Smart Smoke Detector DG")
			// When the model's answer is no JSON, or the model does not answer
			e.n.composer = func(context.Context, string, json.RawMessage) (string, error) { return c.answer, c.err }
			if _, err := e.n.OnPresence(context.Background(), "bad"); err == nil {
				t.Fatal("a failed wording is no error")
			}
			// Then Mira says the sentence of a failed model at the satellite,
			// without listening after it, and no entry is read out instead
			calls := e.ha.calls
			if len(calls) != 1 || calls[0].Service != "announce" || calls[0].Data["message"] != c.say {
				t.Fatalf("calls = %+v", calls)
			}
			// And the log names the error and the messages
			if log := e.logged(); !strings.Contains(log, "could not word the announcement; it waits for the next occasion") ||
				!strings.Contains(log, "m-th-dg") {
				t.Errorf("log:\n%s", log)
			}
			// And the message still waits and is announced at the next occasion
			if e.state("m-th-dg") != store.MessagePending {
				t.Errorf("the message is %s", e.state("m-th-dg"))
			}
			e.n.composer = echoComposer
			e.ha.calls = nil
			if out := e.presence("bad"); len(out.Announced) != 1 {
				t.Errorf("next occasion: %+v", out)
			}
		})
	}
}

func TestMS41_AFailedWordingAtMessagesReadIsTheToolsError(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	// Given a simple message with two entries waits
	e.message("w", 2, func(m *store.Message) { m.Spoken = "Die Waschmaschine ist fertig" })
	e.message("t", 2, func(m *store.Message) { m.Spoken = "Der Trockner ist fertig" })
	// When messages_read cannot have them worded
	e.n.composer = func(context.Context, string, json.RawMessage) (string, error) { return "kein JSON", nil }
	_, err := e.n.ReadOut(context.Background(), "bad", "", "")
	// Then the tool gives the error, with the sentence of a failed model
	var ce *ComposeError
	if !errors.As(err, &ce) || ce.Say != "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal." {
		t.Fatalf("err = %v", err)
	}
	// And the message still waits
	if e.state("w") != store.MessagePending {
		t.Errorf("the message is %s", e.state("w"))
	}
}

func TestMS42_AKindWithoutItsAnsageIsNotAnnounced(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	e.n.instruction = func(context.Context, string) (string, error) { return "", errors.New("no section Ansage") }
	// Given the skill has no section "Ansage", a device message is not
	// announced; it waits
	e.foundDevice("th-dg", "Smart Smoke Detector DG")
	if _, err := e.n.OnPresence(context.Background(), "bad"); err == nil {
		t.Error("no error without the instruction")
	}
	if len(e.ha.calls) != 0 || len(asked) != 0 {
		t.Errorf("calls %+v, model asked %d times", e.ha.calls, len(asked))
	}
	if e.state("m-th-dg") != store.MessagePending {
		t.Errorf("the message is %s", e.state("m-th-dg"))
	}
}

func TestMS43_SeveralKindsWithThreadsEachNamedThenWhichFirst(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	e.n.instruction = func(_ context.Context, kind string) (string, error) {
		return "Anweisung für " + kind, nil
	}
	// Given a postfach message with a mail and a device message with two
	// found devices wait
	e.foundDevice("th-1", "Rauchmelder DG")
	e.foundDevice("th-2", "Bewegungsmelder")
	th := &store.Thread{ID: "th-mail", Target: "termin", Place: "person:ben", State: "collecting", Request: "Elternbrief",
		ExpiresAt: e.clk.Now().Add(24 * time.Hour)}
	if err := e.st.PutThread(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	e.message("mail", 2, func(m *store.Message) {
		m.Kind, m.Spoken, m.ThreadID = store.KindAppointment, "Elternbrief mit einem Termin", "th-mail"
	})
	// When they are announced together
	e.presence("bad")
	if len(asked) != 1 {
		t.Fatalf("model asked %d times", len(asked))
	}
	// Then the model is told to name each kind briefly as its instruction
	// says and to ask what to start with
	for _, want := range []string{"Anweisung für device", "Anweisung für appointment",
		"Sind Nachrichten mehrerer Arten mit Vorgängen dabei, nenn jede kurz so, wie ihre Anweisung es sagt, und frag am Ende, womit du anfangen sollst"} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the model was not told %q:\n%s", want, asked[0])
		}
	}
}

func TestMS44_TheAfterwordIsWordedByTheModel(t *testing.T) {
	e := newEnv(t, nil)
	e.clk.Set(at("12:00"))
	var asked []string
	e.withAnsage(&asked)
	// Given a device message waits in the room
	e.foundDevice("th-dg", "Smart Smoke Detector DG")
	// When an answer in that room gets its afterword, then the model words it
	// with the message, its kind, entry and thread and the kind's instruction
	got := e.n.Afterword(context.Background(), "bad", "")
	if got != "Neues Gerät gefunden: Smart Smoke Detector DG." || len(asked) != 1 {
		t.Fatalf("afterword %q, model asked %d times", got, len(asked))
	}
	for _, want := range []string{"Art device", "Vorgang th-dg, Skill geraet-einbinden", deviceAnsage, "Beginne mit „Übrigens“"} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the model was not told %q:\n%s", want, asked[0])
		}
	}
	// And the message counts as heard
	if e.state("m-th-dg") != store.MessageDelivered {
		t.Errorf("the message is %s", e.state("m-th-dg"))
	}

	// When the wording fails, the sentence of a failed model is the
	// afterword and the message still waits
	e.foundDevice("th-kg", "Smart Smoke Detector KG")
	e.n.composer = func(context.Context, string, json.RawMessage) (string, error) { return "kein JSON", nil }
	if got := e.n.Afterword(context.Background(), "bad", ""); got != "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal." {
		t.Errorf("afterword %q", got)
	}
	if e.state("m-th-kg") != store.MessagePending {
		t.Errorf("the message is %s", e.state("m-th-kg"))
	}
}
