package haconv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/mailcal"
)

// inboxStub stands for the inbox: it only checks its own token, as the
// inbox does.
type inboxStub struct{ hits int }

func (s *inboxStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer inbox-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.hits++
	w.WriteHeader(http.StatusAccepted)
}

func call(t *testing.T, h http.Handler, method, path, token string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(`{"doc_id": 1, "owner": "anna"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// docs/spec/inbox.md, step 1: the service's token does not open the inbox,
// and the inbox token opens nothing else.
func TestIN02_InboxTokenOpensNothingElse(t *testing.T) {
	inbox := &inboxStub{}
	h := New(Options{Token: "api-token", Version: "test", Inbox: inbox})
	if code := call(t, h, http.MethodPost, InboxPath, "api-token"); code != http.StatusUnauthorized || inbox.hits != 0 {
		t.Fatalf("the API token opened the inbox: %d", code)
	}
	if code := call(t, h, http.MethodPost, InboxPath, "inbox-token"); code != http.StatusAccepted || inbox.hits != 1 {
		t.Fatalf("the inbox token: %d", code)
	}
	for _, path := range []string{"/v1/status", "/v1/health", "/v1/messages"} {
		if code := call(t, h, http.MethodGet, path, "inbox-token"); code != http.StatusUnauthorized {
			t.Errorf("the inbox token opened %s: %d", path, code)
		}
	}
}

func TestIN03_WithoutTheInboxItIsOff(t *testing.T) {
	h := New(Options{Token: "api-token", Version: "test"})
	for _, token := range []string{"api-token", "anything"} {
		if code := call(t, h, http.MethodPost, InboxPath, token); code != http.StatusNotFound {
			t.Fatalf("token %s: %d", token, code)
		}
	}
	// The log line once per start: newInbox in cmd/mira (TestIN03_InboxOffIsLogged).
}

type mailInboxStub struct{ got []mailcal.Mail }

func (s *mailInboxStub) Owns(entry string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "postfach")
}

func (s *mailInboxStub) EnqueueMail(_ context.Context, m mailcal.Mail) bool {
	s.got = append(s.got, m)
	return true
}

type mailCalStub struct{ handled int }

func (m *mailCalStub) AppNames() []string { return []string{"pakete"} }

func (m *mailCalStub) Handle(context.Context, mailcal.Mail) (mailcal.Outcome, error) {
	m.handled++
	return mailcal.Outcome{Action: "created", Title: "Paket"}, nil
}

func postMail(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/mail", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer api-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// docs/spec/inbox.md, step 6.
func TestIN24_AMailOfTheInboxIsAnsweredAtOnce(t *testing.T) {
	in, cal := &mailInboxStub{}, &mailCalStub{}
	h := New(Options{Token: "api-token", Version: "test", Mail: cal, MailInbox: in})
	rec := postMail(t, h, `{"entry": "Postfach anna", "subject": "Elternbrief", "text": "Elternabend"}`)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued":true`) {
		t.Fatalf("answer = %d %s", rec.Code, rec.Body)
	}
	if len(in.got) != 1 || cal.handled != 0 {
		t.Fatalf("inbox %d, calendar %d", len(in.got), cal.handled)
	}
}

func TestIN25_ParcelMailsStayAsTheyAre(t *testing.T) {
	in, cal := &mailInboxStub{}, &mailCalStub{}
	h := New(Options{Token: "api-token", Version: "test", Mail: cal, MailInbox: in})
	rec := postMail(t, h, `{"entry": "Pakete", "subject": "Paket", "text": "kommt morgen"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"action":"created"`) || cal.handled != 1 || len(in.got) != 0 {
		t.Fatalf("answer = %d %s, calendar %d, inbox %d", rec.Code, rec.Body, cal.handled, len(in.got))
	}
}

func TestIN26_TheServiceTakesImapEntryAndUID(t *testing.T) {
	in := &mailInboxStub{}
	h := New(Options{Token: "api-token", Version: "test", MailInbox: in})
	postMail(t, h, `{"entry": "Postfach anna", "text": "x", "imap_entry": "01ABC", "uid": "7"}`)
	if len(in.got) != 1 || in.got[0].ImapEntry != "01ABC" || in.got[0].UID != "7" {
		t.Fatalf("got = %+v", in.got)
	}
	// The integration's side (it sends them, no calendar repair for the
	// inbox): tests/ha/test_mail.py, test_IN26_….
}
