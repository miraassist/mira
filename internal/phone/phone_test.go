package phone

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// testBook is the phone book Mira FRITZ!Box answers in these tests.
var testBook = Phonebook{Books: []string{"Telefonbuch"}, URL: "http://192.168.178.1", Contacts: []Contact{
	{Name: "Oma Erika", Numbers: []Number{{Number: "0172 1111111", Type: "mobile", Main: true}}},
	{Name: "Oma Hilde", Numbers: []Number{{Number: "0721 222222", Type: "home", Main: true}}},
	{Name: "Ben Müller", Numbers: []Number{{Number: "0721 333333", Type: "home"}, {Number: "0151 444444", Type: "mobile"}}},
	{Name: "Anna Weber", Numbers: []Number{{Number: "0721 555555", Type: "home"}, {Number: "0160 666666", Type: "mobile", Main: true}}},
	{Name: "Praxis Faxmann", Numbers: []Number{{Number: "0721 777777", Type: "fax_work", Main: true}}},
}}

type call struct {
	service string
	data    map[string]any
}

type env struct {
	tool  tools.InternalTool
	calls []call
	line  Line
	book  *Book
}

func setup(t *testing.T, bookOn bool) *env {
	t.Helper()
	e := &env{line: Line{Node: "show", Phone: true, Intercom: true, State: Ready}}
	e.book = &Book{On: func() bool { return bookOn },
		Read: func(context.Context) (Phonebook, error) { return testBook, nil }}
	e.tool = Tool(Options{Lang: i18n.DE, Book: e.book,
		Line: func(context.Context, string) Line { return e.line },
		Device: func(_ context.Context, target string) (string, string, bool) {
			switch strings.ToLower(strings.TrimPrefix(strings.ToLower(target), "die ")) {
			case "küche":
				return "Kitchen", "dev-kitchen", true
			case "wohnzimmer":
				return "Show", "dev-show", true
			}
			return "", "", false
		},
		Call: func(_ context.Context, service string, data map[string]any) error {
			e.calls = append(e.calls, call{service, data})
			return nil
		},
	})
	return e
}

func (e *env) run(t *testing.T, channel config.Channel, args string) string {
	t.Helper()
	out, err := e.tool.Run(context.Background(), tools.Invocation{Channel: channel, DeviceID: "dev-show", Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTE01_AContactIsCalledFromTheSatellite(t *testing.T) {
	e := setup(t, true)
	out := e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`)
	if len(e.calls) != 1 || e.calls[0].service != "show_phone_call" || e.calls[0].data["number"] != "01721111111" {
		t.Fatalf("calls %+v", e.calls)
	}
	if !strings.Contains(out, "Oma Erika (Handy)") || strings.Contains(out, "0172") {
		t.Errorf("out %q", out)
	}
}

// TE-15 A satellite that can show the name gets it with the call
func TestTE15_TheNameGoesWithTheCallWhereTheDeviceShowsIt(t *testing.T) {
	e := setup(t, true)
	e.line.Named = true
	e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`)
	if len(e.calls) != 1 || e.calls[0].service != "show_phone_call_named" || e.calls[0].data["name"] != "Oma Erika" ||
		e.calls[0].data["number"] != "01721111111" {
		t.Fatalf("calls %+v", e.calls)
	}
	// a number has no name: the plain call
	e.run(t, config.ChannelVoice, `{"target":"0721 123456"}`)
	if len(e.calls) != 2 || e.calls[1].service != "show_phone_call" {
		t.Errorf("calls %+v", e.calls)
	}
}

func TestTE02_SeveralContactsFit(t *testing.T) {
	e := setup(t, true)
	out := e.run(t, config.ChannelVoice, `{"target":"Oma"}`)
	if len(e.calls) != 0 || !strings.Contains(out, "Oma Erika") || !strings.Contains(out, "Oma Hilde") {
		t.Errorf("calls %+v, out %q", e.calls, out)
	}
}

func TestTE03_SeveralNumbers(t *testing.T) {
	e := setup(t, true)
	out := e.run(t, config.ChannelVoice, `{"target":"Ben"}`)
	if len(e.calls) != 0 || !strings.Contains(out, "Handy") || !strings.Contains(out, "privat") {
		t.Errorf("calls %+v, out %q", e.calls, out)
	}
	e.run(t, config.ChannelVoice, `{"target":"Ben","kind":"Handy"}`)
	if len(e.calls) != 1 || e.calls[0].data["number"] != "0151444444" {
		t.Errorf("calls %+v", e.calls)
	}
}

func TestTE04_TheMainNumberCountsAsClear(t *testing.T) {
	e := setup(t, true)
	e.run(t, config.ChannelVoice, `{"target":"anna"}`)
	if len(e.calls) != 1 || e.calls[0].data["number"] != "0160666666" {
		t.Errorf("calls %+v", e.calls)
	}
}

func TestTE05_ARoomGoesOverTheIntercom(t *testing.T) {
	e := setup(t, true)
	e.line.State = "Login refused" // the intercom needs no registrar
	out := e.run(t, config.ChannelVoice, `{"target":"die Küche"}`)
	if len(e.calls) != 1 || e.calls[0].service != "show_intercom_call" || e.calls[0].data["device"] != "Kitchen" {
		t.Errorf("calls %+v, out %q", e.calls, out)
	}
	if out := e.run(t, config.ChannelVoice, `{"target":"Wohnzimmer"}`); len(e.calls) != 1 || !strings.Contains(out, "steht") {
		t.Errorf("own room: %q", out)
	}
}

func TestTE06_ANumberIsDialedAsItIs(t *testing.T) {
	e := setup(t, false)
	e.run(t, config.ChannelVoice, `{"target":"0721 123456"}`)
	if len(e.calls) != 1 || e.calls[0].data["number"] != "0721123456" {
		t.Errorf("calls %+v", e.calls)
	}
}

func TestTE07_OnlyInAVoiceConversation(t *testing.T) {
	e := setup(t, true)
	for _, ch := range []config.Channel{config.ChannelTelegram, config.ChannelBackground} {
		if out := e.run(t, ch, `{"target":"Oma Erika"}`); len(e.calls) != 0 || !strings.Contains(out, "Sprachgespräch") {
			t.Errorf("%s: %q", ch, out)
		}
	}
}

func TestTE08_NotSignedInAtTheRegistrar(t *testing.T) {
	e := setup(t, true)
	for state, want := range map[string]string{"Connecting": "noch nicht", "Login refused": "abgelehnt",
		"Cannot sign in": "erreicht die Telefonanlage nicht", "Not set up": "keine Anmeldung", "In call": "belegt", "unavailable": "nicht erreichbar"} {
		e.line.State = state
		if out := e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`); len(e.calls) != 0 || !strings.Contains(out, want) {
			t.Errorf("%s: %q", state, out)
		}
	}
	e.line = Line{Node: "echo"}
	if out := e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`); !strings.Contains(out, "kann nicht telefonieren") {
		t.Errorf("no phone: %q", out)
	}
}

func TestTE09_WithoutTheSkillNoNames(t *testing.T) {
	e := setup(t, false)
	if out := e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`); len(e.calls) != 0 || !strings.Contains(out, "Telefonbuch ist aus") {
		t.Errorf("out %q", out)
	}
	e.run(t, config.ChannelVoice, `{"target":"Küche"}`)
	e.run(t, config.ChannelVoice, `{"target":"110 112"}`)
	if len(e.calls) != 2 {
		t.Errorf("calls %+v", e.calls)
	}
}

func TestTE10_ThePhoneBookIsReadBeforeEveryCall(t *testing.T) {
	var reads int
	var fail error
	b := &Book{On: func() bool { return true }, Read: func(context.Context) (Phonebook, error) {
		reads++
		if fail != nil {
			return Phonebook{}, fail
		}
		return testBook, nil
	}}
	ctx := context.Background()
	b.Contacts(ctx)
	b.Contacts(ctx)
	// read each time, so a changed number counts at once
	if reads != 2 {
		t.Errorf("reads %d", reads)
	}
	if st := b.Status(); !st.On || len(st.Books) != 1 || st.Books[0] != "Telefonbuch" || st.Link != "http://192.168.178.1" || st.Error != "" {
		t.Errorf("status %+v", st)
	}
	// a box that does not answer: the contacts read last stand in, the page says why
	fail = errors.New("the FRITZ!Box does not answer")
	if cs, err := b.Contacts(ctx); err != nil || len(cs) == 0 {
		t.Errorf("contacts %d, err %v", len(cs), err)
	}
	if st := b.Status(); !strings.Contains(st.Error, "does not answer") || st.Missing {
		t.Errorf("status %+v", st)
	}
	// never read and no answer: the error
	bad := &Book{On: func() bool { return true }, Read: func(context.Context) (Phonebook, error) { return Phonebook{}, fail }}
	if _, err := bad.Contacts(ctx); err == nil {
		t.Error("no error")
	}
}

func TestTE16_WithoutMiraFritzBoxTheSkillIsAProblem(t *testing.T) {
	e := setup(t, true)
	e.book.Read = func(context.Context) (Phonebook, error) { return Phonebook{}, ErrNoIntegration }
	if out := e.run(t, config.ChannelVoice, `{"target":"Oma Erika"}`); len(e.calls) != 0 || !strings.Contains(out, "nicht lesen") {
		t.Errorf("out %q", out)
	}
	if st := e.book.Status(); !st.Missing {
		t.Errorf("status %+v", st)
	}
	if !strings.Contains(MissingText(i18n.DE), "Mira FRITZ!Box fehlt") {
		t.Error(MissingText(i18n.DE))
	}
}

func TestTE13_FaxNumbersAreNotDialed(t *testing.T) {
	e := setup(t, true)
	if out := e.run(t, config.ChannelVoice, `{"target":"Praxis Faxmann"}`); len(e.calls) != 0 || !strings.Contains(out, "keine Nummer") {
		t.Errorf("out %q", out)
	}
}
