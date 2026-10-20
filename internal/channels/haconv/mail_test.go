package haconv

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/store"
)

type fakeMail struct{ got mailcal.Mail }

func (f *fakeMail) AppNames() []string { return []string{"pakete"} }
func (f *fakeMail) Handle(_ context.Context, m mailcal.Mail) (mailcal.Outcome, error) {
	if m.Entry != "Pakete" {
		return mailcal.Outcome{}, fmt.Errorf("%w: %q", mailcal.ErrNoApp, m.Entry)
	}
	f.got = m
	return mailcal.Outcome{Action: "created", Title: "Paket von Amazon"}, nil
}

// The integration learns the mail applications from the status and hands
// over the mails of their IMAP entries.
func TestMail(t *testing.T) {
	srv, _, _ := setup(t)
	if _, status := do(t, srv, "GET", "/v1/status", "secret", ""); fmt.Sprint(status["mail_apps"]) != "[]" {
		t.Errorf("without mail: %v", status["mail_apps"])
	}
	if code, _ := do(t, srv, "POST", "/v1/mail", "secret", `{"entry":"Pakete"}`); code != 404 {
		t.Errorf("without mail: %d", code)
	}

	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mail := &fakeMail{}
	srv = httptest.NewServer(New(Options{Token: "secret", Store: st, Mail: mail}))
	defer srv.Close()
	if _, status := do(t, srv, "GET", "/v1/status", "secret", ""); fmt.Sprint(status["mail_apps"]) != "[pakete]" {
		t.Errorf("mail_apps: %v", status["mail_apps"])
	}
	code, out := do(t, srv, "POST", "/v1/mail", "secret",
		`{"entry":"Pakete","sender":"a@dhl.de","subject":"Ihr Paket","date":"2026-10-20T08:00:00+02:00","text":"Hallo"}`)
	if code != 200 || out["action"] != "created" || mail.got.Sender != "a@dhl.de" || mail.got.Date.Hour() != 8 {
		t.Errorf("mail: %d %v %+v", code, out, mail.got)
	}
	if code, _ := do(t, srv, "POST", "/v1/mail", "secret", `{"entry":"Rechnungen"}`); code != 422 {
		t.Errorf("unknown entry: %d", code)
	}
	if code, _ := do(t, srv, "POST", "/v1/mail", "secret", `{"text":"x"}`); code != 400 {
		t.Errorf("no entry: %d", code)
	}
}
