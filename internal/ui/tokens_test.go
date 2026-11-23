package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

// tokenServer is the page with real access tokens; it answers with the
// service's token "secret".
func tokenServer(t *testing.T) (*httptest.Server, *access.Keeper, *store.Store, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	k, err := access.New(ctx, st, "secret", log)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(k.Check(New(Options{Token: "secret", Tokens: k, Logger: log, Store: st})))
	t.Cleanup(srv.Close)
	return srv, k, st, logs
}

func callAs(t *testing.T, srv *httptest.Server, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(resp.Body)
	return resp.StatusCode, b.String()
}

func TestAT10_ThePageMakesATokenAndShowsItOnce(t *testing.T) {
	srv, _, _, logs := tokenServer(t)
	code, body := call(t, srv, "POST", "/v1/ui/tokens", "secret", `{"name":"Elektro Meier"}`)
	var made struct {
		Token string       `json:"token"`
		Info  access.Token `json:"info"`
	}
	if err := json.Unmarshal([]byte(body), &made); err != nil || code != http.StatusCreated ||
		!strings.HasPrefix(made.Token, "mira_p_") || made.Info.Name != "Elektro Meier" {
		t.Fatalf("make: %d %s", code, body)
	}
	code, body = call(t, srv, "GET", "/v1/ui/tokens", "secret", "")
	if code != http.StatusOK || !strings.Contains(body, "Elektro Meier") || strings.Contains(body, made.Token) {
		t.Fatalf("list: %d %s", code, body)
	}
	if !strings.Contains(logs.String(), `msg="access token made on the page"`) || !strings.Contains(logs.String(), `name="Elektro Meier"`) ||
		strings.Contains(logs.String(), made.Token) {
		t.Fatalf("log: %s", logs)
	}
	// The same name again: 409, nothing stored (AT-06 on the page).
	if code, body := call(t, srv, "POST", "/v1/ui/tokens", "secret", `{"name":"Elektro Meier"}`); code != http.StatusConflict ||
		!strings.Contains(body, "name taken") {
		t.Fatalf("again: %d %s", code, body)
	}
	// The page shows it once, in its own dialog, read-only, to copy and close
	// (decision 20).
	fn := regexp.MustCompile(`(?s)async function tokenMake\(form\) \{.*?\n\}`).Find(page)
	for _, want := range []string{"await ask({", "value: r.token", "readonly: true", "ok: T.tok_copy_close",
		"copyText(r.token)"} {
		if !bytes.Contains(fn, []byte(want)) {
			t.Fatalf("tokenMake lacks %s:\n%s", want, fn)
		}
	}
	// A taken name is said under the field, not only at the page's top.
	if !bytes.Contains(fn, []byte(`fail(e.message === "name taken" ? T.tok_taken : e.message)`)) {
		t.Fatalf("tokenMake does not say a taken name under the field:\n%s", fn)
	}
	// Copying works over http too (decision 21).
	cp := regexp.MustCompile(`(?s)async function copyText\(text\) \{.*?\n\}`).Find(page)
	if !bytes.Contains(cp, []byte("navigator.clipboard")) || !bytes.Contains(cp, []byte(`document.execCommand("copy")`)) {
		t.Fatalf("copyText:\n%s", cp)
	}
	// Home Assistant users who are no admin may not (decision 8).
	if code, _ := callAs(t, srv, "POST", "/v1/ui/tokens", `{"name":"Other"}`,
		map[string]string{HeaderHAUser: "u1", HeaderHAAdmin: "0"}); code != http.StatusForbidden {
		t.Fatalf("a user without admin rights: %d", code)
	}
	if code, _ := callAs(t, srv, "GET", "/v1/ui/tokens", "", map[string]string{HeaderHAUser: "u1", HeaderHAAdmin: "0"}); code != http.StatusForbidden {
		t.Fatalf("a user without admin rights lists: %d", code)
	}
}

func TestAT11_DeletingAsksInThePagesDialog(t *testing.T) {
	srv, k, _, logs := tokenServer(t)
	token, made, _ := k.Make(context.Background(), "Elektro Meier")
	if code, body := call(t, srv, "DELETE", "/v1/ui/tokens/"+made.ID, "secret", ""); code != http.StatusOK ||
		!strings.Contains(body, `"deleted":true`) {
		t.Fatalf("delete: %d %s", code, body)
	}
	if code, _ := call(t, srv, "GET", "/v1/ui/tokens", token, ""); code != http.StatusUnauthorized {
		t.Fatalf("the deleted token still works: %d", code)
	}
	if code, body := call(t, srv, "DELETE", "/v1/ui/tokens/"+made.ID, "secret", ""); code != http.StatusNotFound ||
		!strings.Contains(body, "no such token") {
		t.Fatalf("again: %d %s", code, body)
	}
	if !strings.Contains(logs.String(), `msg="access token deleted on the page"`) {
		t.Fatalf("log: %s", logs)
	}
	// The page asks in its own dialog, never the browser's.
	fn := regexp.MustCompile(`(?s)async function tokenDelete\(id, name\) \{.*?\n\}`).Find(page)
	if fn == nil || !bytes.Contains(fn, []byte("await ask({ text: T.tok_delete_ask")) ||
		bytes.Contains(fn, []byte("confirm(")) {
		t.Fatalf("tokenDelete:\n%s", fn)
	}
}

func TestAT12_ThePageShowsWhatThePartnerDid(t *testing.T) {
	srv, k, st, _ := tokenServer(t)
	_, made, _ := k.Make(context.Background(), "Elektro Meier")
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for i, p := range []string{"/v1/status", "/v1/ui/tasks", "/v1/ui/config"} {
		if err := st.RecordAccessCall(context.Background(), store.AccessCall{TokenID: made.ID, TokenName: made.Name,
			PartnerUser: "anna", Method: "GET", Path: p, Status: 200, At: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	code, body := call(t, srv, "GET", "/v1/ui/tokens/"+made.ID+"/activity", "secret", "")
	var got struct {
		Calls []store.AccessCall `json:"calls"`
		Page  int                `json:"page"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != http.StatusOK || len(got.Calls) != 3 || got.Page != 50 {
		t.Fatalf("activity: %d %s", code, body)
	}
	if got.Calls[0].Path != "/v1/ui/config" || got.Calls[2].Path != "/v1/status" || got.Calls[0].PartnerUser != "anna" {
		t.Fatalf("not newest first: %+v", got.Calls)
	}
	// The page shows time, partner user, what and the result.
	fn := regexp.MustCompile(`(?s)async function tokenActivity\(id, offset\) \{.*?\n\}`).Find(page)
	for _, want := range []string{"c.at", "c.partner_user", "c.method", "c.path", "c.status"} {
		if !bytes.Contains(fn, []byte(want)) {
			t.Fatalf("tokenActivity lacks %s:\n%s", want, fn)
		}
	}
}

func TestAT14_APartnerSeesOnlyTheHousesMemory(t *testing.T) {
	// Since decision 23 a partner token reaches no route of the page: the
	// memory is refused before the page's own check.
	srv, k, st, _ := tokenServer(t)
	ctx := context.Background()
	if _, err := st.SaveFact(ctx, store.Fact{ID: "f1", Owner: store.OwnerHouse, Text: "Der Müll kommt dienstags"}); err != nil {
		t.Fatal(err)
	}
	token, _, _ := k.Make(ctx, "Elektro Meier")
	if code, body := call(t, srv, "GET", "/v1/ui/memory", token, ""); code != http.StatusForbidden ||
		strings.Contains(body, "dienstags") {
		t.Fatalf("memory: %d %s", code, body)
	}
	if code, _ := call(t, srv, "DELETE", "/v1/ui/memory/f1", token, ""); code != http.StatusForbidden {
		t.Fatalf("forgetting the house's fact: %d", code)
	}
	// The service's own token still sees it.
	if _, body := call(t, srv, "GET", "/v1/ui/memory", "secret", ""); !strings.Contains(body, "dienstags") {
		t.Fatalf("the integration's token: %s", body)
	}
}

func TestAT15_APartnerTokenCannotManageAccess(t *testing.T) {
	srv, k, _, _ := tokenServer(t)
	token, made, _ := k.Make(context.Background(), "Elektro Meier")
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/ui/tokens", ""},
		{"POST", "/v1/ui/tokens", `{"name":"Hintertür"}`},
		{"DELETE", "/v1/ui/tokens/" + made.ID, ""},
		{"GET", "/v1/ui/tokens/" + made.ID + "/activity", ""},
	} {
		if code, _ := call(t, srv, c.method, c.path, token, c.body); code != http.StatusForbidden {
			t.Fatalf("%s %s: %d", c.method, c.path, code)
		}
	}
	if list, _ := k.List(context.Background()); len(list) != 1 {
		t.Fatalf("tokens now: %+v", list)
	}
}
