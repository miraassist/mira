package access

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

const integrationToken = "the-integrations-token"

// seen is what the handler behind the check got.
type seen struct {
	auth, partnerUser, haUser, haAdmin string
	caller                             Caller
	called                             bool
}

func newKeeper(t *testing.T) (*Keeper, *store.Store, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var logs bytes.Buffer
	k, err := New(ctx, st, integrationToken, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return k, st, &logs
}

// serve sends one call through the check to a handler that notes what it got.
func serve(k *Keeper, method, path, bearer string, headers map[string]string) (*httptest.ResponseRecorder, *seen) {
	s := &seen{}
	h := k.Check(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.called = true
		s.auth = r.Header.Get("Authorization")
		s.partnerUser = r.Header.Get(HeaderPartnerUser)
		s.haUser = r.Header.Get(headerHAUser)
		s.haAdmin = r.Header.Get(headerHAAdmin)
		s.caller, _ = CallerFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, s
}

func TestAT01_APartnerTokenIsMadeAndWorks(t *testing.T) {
	k, _, _ := newKeeper(t)
	token, made, err := k.Make(context.Background(), "Elektro Meier")
	if err != nil {
		t.Fatal(err)
	}
	if made.Kind != KindPartner || made.Name != "Elektro Meier" {
		t.Fatalf("made %+v", made)
	}
	rec, s := serve(k, http.MethodGet, "/v1/status", token, nil)
	if rec.Code != http.StatusOK || !s.called {
		t.Fatalf("status %d, called %v", rec.Code, s.called)
	}
	// Passed on with the integration's token, as an admin without a user.
	if s.auth != "Bearer "+integrationToken {
		t.Fatalf("passed on with %q", s.auth)
	}
	if s.caller.Kind != KindPartner || s.caller.Name != "Elektro Meier" {
		t.Fatalf("caller %+v", s.caller)
	}
	// A token not made here is not taken for a partner's.
	if _, s := serve(k, http.MethodGet, "/v1/status", "mira_p_madeup", nil); s.auth != "Bearer mira_p_madeup" {
		t.Fatalf("an unknown token was changed to %q", s.auth)
	}
}

func TestAT02_TheTokenIsShownOnlyOnceAndStoredAsAHash(t *testing.T) {
	k, st, _ := newKeeper(t)
	token, _, err := k.Make(context.Background(), "Elektro Meier")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "mira_p_") || len(token) < 40 {
		t.Fatalf("token %q", token)
	}
	stored, err := st.AccessTokens(context.Background())
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored %v, %v", stored, err)
	}
	if stored[0].Hash != hashOf(token) || stored[0].Prefix != token[:prefixLen] {
		t.Fatalf("stored %+v", stored[0])
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM access_tokens WHERE hash = ? OR prefix = ? OR name = ?`,
		token, token, token).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the token itself is in the database: %d, %v", n, err)
	}
	list, _ := k.List(context.Background())
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), token) {
		t.Fatalf("the list shows the token: %s", b)
	}
}

func TestAT03_WhoamiNamesTheCaller(t *testing.T) {
	k, _, _ := newKeeper(t)
	token, _, _ := k.Make(context.Background(), "Elektro Meier")
	for _, tc := range []struct{ bearer, name, kind string }{
		{token, "Elektro Meier", KindPartner},
		{integrationToken, KindIntegration, KindIntegration},
	} {
		rec, _ := serve(k, http.MethodGet, "/v1/whoami", tc.bearer, nil)
		var got map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusOK || got["name"] != tc.name || got["kind"] != tc.kind {
			t.Fatalf("%s: %d %v", tc.kind, rec.Code, got)
		}
	}
	if rec, _ := serve(k, http.MethodGet, "/v1/whoami", "wrong", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong token: %d", rec.Code)
	}
}

func TestAT04_ADeletedTokenIsRefusedAtOnce(t *testing.T) {
	k, _, _ := newKeeper(t)
	token, made, _ := k.Make(context.Background(), "Elektro Meier")
	if _, s := serve(k, http.MethodGet, "/v1/status", token, nil); s.auth != "Bearer "+integrationToken {
		t.Fatalf("before: passed on with %q", s.auth)
	}
	if _, err := k.Delete(context.Background(), made.ID); err != nil {
		t.Fatal(err)
	}
	// Passed on unchanged: the handler's own check refuses it.
	if _, s := serve(k, http.MethodGet, "/v1/status", token, nil); s.auth != "Bearer "+token || s.caller.Kind != "" {
		t.Fatalf("after: passed on with %q as %+v", s.auth, s.caller)
	}
	if rec, _ := serve(k, http.MethodGet, "/v1/whoami", token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("whoami after: %d", rec.Code)
	}
	if _, err := k.Delete(context.Background(), made.ID); err != store.ErrNotFound {
		t.Fatalf("deleting again: %v", err)
	}
}

func TestAT05_XPartnerUserCountsOnlyWithAPartnerToken(t *testing.T) {
	k, _, _ := newKeeper(t)
	_, s := serve(k, http.MethodGet, "/v1/status", integrationToken, map[string]string{HeaderPartnerUser: "anna"})
	if s.partnerUser != "" {
		t.Fatalf("the handler saw the partner user %q", s.partnerUser)
	}
	token, _, _ := k.Make(context.Background(), "Elektro Meier")
	_, s = serve(k, http.MethodGet, "/v1/ui/tasks", token, map[string]string{HeaderPartnerUser: "anna",
		headerHAUser: "someone", headerHAAdmin: "0"})
	if s.caller.PartnerUser != "anna" {
		t.Fatalf("caller %+v", s.caller)
	}
	// As an admin without a Home Assistant user (decision 10).
	if s.haUser != "" || s.haAdmin != "" {
		t.Fatalf("Home Assistant's headers passed on: %q %q", s.haUser, s.haAdmin)
	}
}

func TestAT13_ThePartnerTokenIsRefusedAtTheSetupAndTheBackup(t *testing.T) {
	k, _, _ := newKeeper(t)
	token, _, _ := k.Make(context.Background(), "Elektro Meier")
	for _, p := range []string{"/v1/pair", "/v1/unpair", "/v1/prove", "/v1/setup", "/v1/setup/models", "/v1/backup",
		"/v1/restore"} {
		rec, s := serve(k, http.MethodPost, p, token, nil)
		if rec.Code != http.StatusForbidden || s.called {
			t.Fatalf("%s: %d, called %v", p, rec.Code, s.called)
		}
	}
	if rec, s := serve(k, http.MethodPost, "/v1/pair", integrationToken, nil); rec.Code != http.StatusOK || !s.called {
		t.Fatalf("the integration's token at the setup: %d", rec.Code)
	}
}

func TestAT06_ASecondTokenWithTheSameNameIsRefused(t *testing.T) {
	k, st, _ := newKeeper(t)
	if _, _, err := k.Make(context.Background(), "Elektro Meier"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Make(context.Background(), " Elektro Meier "); err != ErrNameTaken {
		t.Fatalf("second: %v", err)
	}
	if _, _, err := k.Make(context.Background(), "  "); err != ErrNoName {
		t.Fatalf("no name: %v", err)
	}
	if list, _ := st.AccessTokens(context.Background()); len(list) != 1 {
		t.Fatalf("stored %d", len(list))
	}
}

func TestAT07_APartnerCallIsRecordedWithItsPartnerUser(t *testing.T) {
	k, st, logs := newKeeper(t)
	token, made, _ := k.Make(context.Background(), "Elektro Meier")
	serve(k, http.MethodGet, "/v1/ui/tasks", token, map[string]string{HeaderPartnerUser: "anna"})
	serve(k, http.MethodPost, "/v1/backup", token, map[string]string{HeaderPartnerUser: "anna"})
	serve(k, http.MethodGet, "/v1/status", integrationToken, nil)
	calls, err := st.AccessCalls(context.Background(), made.ID, 50, 0)
	if err != nil || len(calls) != 2 {
		t.Fatalf("calls %+v, %v", calls, err)
	}
	got := map[string]store.AccessCall{}
	for _, c := range calls {
		got[c.Path] = c
	}
	if c := got["/v1/ui/tasks"]; c.TokenName != "Elektro Meier" || c.PartnerUser != "anna" || c.Method != "GET" ||
		c.Status != http.StatusOK {
		t.Fatalf("recorded %+v", c)
	}
	if c := got["/v1/backup"]; c.Status != http.StatusForbidden {
		t.Fatalf("a refused call recorded as %+v", c)
	}
	if !strings.Contains(logs.String(), `msg="partner call" token="Elektro Meier" partner_user=anna method=GET path=/v1/ui/tasks status=200`) {
		t.Fatalf("log:\n%s", logs)
	}
}

func TestAT08_TheCallRunsAlthoughItsRecordFails(t *testing.T) {
	k, st, logs := newKeeper(t)
	token, _, _ := k.Make(context.Background(), "Elektro Meier")
	if _, err := st.DB().Exec(`DROP TABLE access_log`); err != nil {
		t.Fatal(err)
	}
	rec, s := serve(k, http.MethodGet, "/v1/status", token, map[string]string{HeaderPartnerUser: "anna"})
	if rec.Code != http.StatusOK || !s.called {
		t.Fatalf("status %d, called %v", rec.Code, s.called)
	}
	if !strings.Contains(logs.String(), "could not record a partner call") ||
		!strings.Contains(logs.String(), "path=/v1/status") {
		t.Fatalf("log:\n%s", logs)
	}
}
