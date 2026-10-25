package phone

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
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

const testBook = `<?xml version="1.0" encoding="utf-8"?>
<phonebooks><phonebook name="Telefonbuch">
<contact><person><realName>Oma Erika</realName></person><telephony>
  <number type="mobile" prio="1">0172 1111111</number></telephony></contact>
<contact><person><realName>Oma Hilde</realName></person><telephony>
  <number type="home" prio="1">0721 222222</number></telephony></contact>
<contact><person><realName>Ben Müller</realName></person><telephony>
  <number type="home" prio="">0721 333333</number>
  <number type="mobile" prio="">0151 444444</number></telephony></contact>
<contact><person><realName>Anna Weber</realName></person><telephony>
  <number type="home" prio="">0721 555555</number>
  <number type="mobile" prio="1">0160 666666</number></telephony></contact>
<contact><person><realName>Praxis Faxmann</realName></person><telephony>
  <number type="fax_work" prio="1">0721 777777</number></telephony></contact>
</phonebook></phonebooks>`

// fritzbox plays a FRITZ!Box's TR-064 with digest authentication; reads
// counts the phone book's downloads.
func fritzbox(t *testing.T, user, password string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	const realm, nonce = "F!Box SOAP-Auth", "abc123"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/book.xml" {
			reads.Add(1)
			io.WriteString(w, testBook)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=MD5, qop="auth"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		get := func(k string) string {
			m := regexp.MustCompile(k + `="?([^",]*)"?`).FindStringSubmatch(auth)
			if m == nil {
				return ""
			}
			return m[1]
		}
		h := func(s string) string { s2 := md5.Sum([]byte(s)); return hex.EncodeToString(s2[:]) }
		want := h(h(user+":"+realm+":"+password) + ":" + nonce + ":" + get("nc") + ":" + get("cnonce") + ":auth:" + h("POST:"+r.URL.Path))
		if get("username") != user || get("response") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), "GetPhonebookList"):
			io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetPhonebookListResponse xmlns:u="urn:dslforum-org:service:X_AVM-DE_OnTel:1"><NewPhonebookList>0,1</NewPhonebookList></u:GetPhonebookListResponse></s:Body></s:Envelope>`)
		case strings.Contains(string(body), "GetPhonebook"):
			name := "Telefonbuch"
			if strings.Contains(string(body), "<NewPhonebookID>1<") {
				name = "Google"
			}
			fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetPhonebookResponse xmlns:u="urn:dslforum-org:service:X_AVM-DE_OnTel:1"><NewPhonebookName>%s</NewPhonebookName><NewPhonebookURL>%s/book.xml?sid=1</NewPhonebookURL></u:GetPhonebookResponse></s:Body></s:Envelope>`, name, srv.URL)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reads
}

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
	srv, _ := fritzbox(t, "mira", "geheim")
	e := &env{line: Line{Node: "show", Phone: true, Intercom: true, State: Ready}}
	e.book = &Book{Source: func() (Fritz, bool) {
		return Fritz{User: "mira", Password: "geheim", base: srv.URL}, bookOn
	}}
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
	srv, reads := fritzbox(t, "mira", "geheim")
	password := "geheim"
	b := &Book{Source: func() (Fritz, bool) {
		return Fritz{User: "mira", Password: password, base: srv.URL}, true
	}}
	ctx := context.Background()
	b.Contacts(ctx)
	b.Contacts(ctx)
	// read each time, so a changed number counts at once
	if reads.Load() != 2 {
		t.Errorf("reads %d", reads.Load())
	}
	if st := b.Status(); !st.On || len(st.Books) != 1 || st.Books[0] != "Telefonbuch" || st.Error != "" {
		t.Errorf("status %+v", st)
	}
	// a box that does not answer: the contacts read last stand in, the page says why
	password = "falsch"
	if cs, err := b.Contacts(ctx); err != nil || len(cs) == 0 {
		t.Errorf("contacts %d, err %v", len(cs), err)
	}
	if st := b.Status(); !strings.Contains(st.Error, "refused") {
		t.Errorf("status %+v", st)
	}
	// never read and no answer: the error
	bad := &Book{Source: func() (Fritz, bool) { return Fritz{User: "mira", Password: "falsch", base: srv.URL}, true }}
	if _, err := bad.Contacts(ctx); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("err %v", err)
	}
}

func TestTE12_DefaultsAndSettings(t *testing.T) {
	if l := (Fritz{}).Link(); l != "http://192.168.178.1" {
		t.Errorf("link %s", l)
	}
	if u := (Fritz{}).url(onTelPath); u != "http://192.168.178.1:49000/upnp/control/x_contact" {
		t.Errorf("url %s", u)
	}
	srv, _ := fritzbox(t, "mira", "geheim")
	books, _, err := Fritz{User: "mira", Password: "geheim", base: srv.URL, Books: []string{"Telefonbuch", "google"}}.Read(context.Background())
	if err != nil || len(books) != 2 {
		t.Errorf("books %v, err %v", books, err)
	}
	if _, _, err := (Fritz{User: "mira", Password: "geheim", base: srv.URL, Books: []string{"Privat"}}).Read(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "Telefonbuch, Google") {
		t.Errorf("unknown book: %v", err)
	}
}

func TestTE13_FaxNumbersAreNotDialed(t *testing.T) {
	e := setup(t, true)
	if out := e.run(t, config.ChannelVoice, `{"target":"Praxis Faxmann"}`); len(e.calls) != 0 || !strings.Contains(out, "keine Nummer") {
		t.Errorf("out %q", out)
	}
}
