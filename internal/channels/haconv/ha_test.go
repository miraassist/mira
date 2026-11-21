package haconv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/access"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
)

// haSetup is the interface with Mira's connection to a fake Home Assistant
// holding the helper of the spec's example.
func haSetup(t *testing.T) (*httptest.Server, *fakeha.Server, *bytes.Buffer) {
	t.Helper()
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	ha.SetStates([]haws.State{{EntityID: "input_select.angehoerige_zustand", State: "ok",
		Attributes:  map[string]any{"options": []any{"ok", "nachfrage", "hilfe", "alarm", "pause"}, "friendly_name": "Zustand"},
		LastChanged: "2026-11-16T20:31:50+00:00"}})
	client := haClient(t, ha.URL, ha.Token)
	waitConnected(t, client)
	var logs bytes.Buffer
	srv := httptest.NewServer(New(Options{Token: "secret", Version: "test", HA: client,
		Logger: slog.New(slog.NewTextHandler(&logs, nil))}))
	t.Cleanup(srv.Close)
	return srv, ha, &logs
}

func haClient(t *testing.T, url, token string) *haws.Client {
	t.Helper()
	c, err := haws.New(haws.Options{URL: url, Token: token, CallTimeout: 2 * time.Second,
		ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)
	return c
}

func waitConnected(t *testing.T, c *haws.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("Home Assistant not connected: %v", err)
	}
}

func TestHE01_ReadingAHelper(t *testing.T) {
	srv, _, _ := haSetup(t)
	code, out := do(t, srv, "GET", "/v1/ha/states/input_select.angehoerige_zustand", "secret", "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["entity_id"] != "input_select.angehoerige_zustand" || out["state"] != "ok" {
		t.Fatalf("not Home Assistant's state object: %v", out)
	}
	attrs, _ := out["attributes"].(map[string]any)
	if attrs["friendly_name"] != "Zustand" || len(attrs["options"].([]any)) != 5 {
		t.Fatalf("attributes: %v", attrs)
	}
	for _, k := range []string{"last_changed", "last_updated"} {
		if out[k] == nil {
			t.Fatalf("%s missing: %v", k, out)
		}
	}
}

func TestHE02_SettingAHelperByAnAction(t *testing.T) {
	srv, ha, _ := haSetup(t)
	code, out := do(t, srv, "POST", "/v1/ha/services/input_select/select_option", "secret",
		`{"entity_id": "input_select.angehoerige_zustand", "option": "pause"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	calls := ha.ServiceCalls()
	if len(calls) != 1 {
		t.Fatalf("want the action called once, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.Domain != "input_select" || c.Service != "select_option" ||
		c.Data["entity_id"] != "input_select.angehoerige_zustand" || c.Data["option"] != "pause" {
		t.Fatalf("call: %+v", c)
	}
	if len(out) != 0 {
		t.Fatalf("want {}, got %v", out)
	}
}

func TestHE03_AnActionsResponse(t *testing.T) {
	srv, ha, _ := haSetup(t)
	ha.SetServiceResponse("calendar", "get_events", map[string]any{
		"calendar.home": map[string]any{"events": []any{map[string]any{"summary": "Arzt"}}}})
	code, out := do(t, srv, "POST", "/v1/ha/services/calendar/get_events?return_response", "secret",
		`{"entity_id": "calendar.home", "duration": {"hours": 24}}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	got, _ := json.Marshal(out["response"])
	if string(got) != `{"calendar.home":{"events":[{"summary":"Arzt"}]}}` {
		t.Fatalf("response not as it came: %s", got)
	}
}

func TestHE04_AnEntityHomeAssistantDoesNotHave(t *testing.T) {
	srv, _, _ := haSetup(t)
	code, out := do(t, srv, "GET", "/v1/ha/states/input_boolean.gibtsnicht", "secret", "")
	if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "input_boolean.gibtsnicht") {
		t.Fatalf("want 404 naming the entity, got %d %v", code, out)
	}
	// A malformed id is the caller's (400 naming it), not asked of Home Assistant.
	code, out = do(t, srv, "GET", "/v1/ha/states/nodot", "secret", "")
	if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "nodot") {
		t.Fatalf("want 400 naming the value, got %d %v", code, out)
	}
}

func TestHE05_AnActionHomeAssistantRefuses(t *testing.T) {
	srv, ha, logs := haSetup(t)
	const msg = "Option sonstwas is not valid for entity input_select.angehoerige_zustand"
	ha.RefuseService("input_select", "select_option", "service_validation_error", msg)
	code, out := do(t, srv, "POST", "/v1/ha/services/input_select/select_option", "secret",
		`{"entity_id": "input_select.angehoerige_zustand", "option": "sonstwas"}`)
	errText, _ := out["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(errText, msg) || !strings.Contains(errText, "input_select.select_option") {
		t.Fatalf("want 400 with the message and the action, got %d %v", code, out)
	}
	if !strings.Contains(logs.String(), "input_select.select_option") || !strings.Contains(logs.String(), "sonstwas is not valid") {
		t.Fatalf("log does not name the action and the message:\n%s", logs)
	}
	// Data that is no JSON object is the caller's too.
	code, out = do(t, srv, "POST", "/v1/ha/services/input_select/select_option", "secret", `[1]`)
	if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "[1]") {
		t.Fatalf("want 400 naming the data, got %d %v", code, out)
	}
}

func TestHE06_HomeAssistantCannotBeReached(t *testing.T) {
	// A Home Assistant that takes the request and drops the connection.
	var hits atomic.Int32
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(dead.Close)
	client, err := haws.New(haws.Options{URL: dead.URL, Token: "t", CallTimeout: 2 * time.Second,
		HTTPClient: &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: (&net.Dialer{}).DialContext}},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	srv := httptest.NewServer(New(Options{Token: "secret", HA: client, Logger: slog.New(slog.NewTextHandler(&logs, nil))}))
	t.Cleanup(srv.Close)

	code, out := do(t, srv, "GET", "/v1/ha/states/input_select.angehoerige_zustand", "secret", "")
	if code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "input_select.angehoerige_zustand") {
		t.Fatalf("want 502 naming the failure, got %d %v", code, out)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("want one try, Home Assistant was asked %d times", n)
	}
	if !strings.Contains(logs.String(), "GET /v1/ha/states") || !strings.Contains(logs.String(), "input_select.angehoerige_zustand") {
		t.Fatalf("log does not name the route and the entity:\n%s", logs.String())
	}
	// An action without a connection: 502 at once, no wait.
	code, out = do(t, srv, "POST", "/v1/ha/services/input_select/select_option", "secret", `{}`)
	if code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "cannot be reached") {
		t.Fatalf("want 502 for the action, got %d %v", code, out)
	}
	// Mira without Home Assistant: 503.
	none := httptest.NewServer(New(Options{Token: "secret"}))
	t.Cleanup(none.Close)
	if code, out := do(t, none, "GET", "/v1/ha/states/a.b", "secret", ""); code != http.StatusServiceUnavailable ||
		out["error"] != "Home Assistant is not connected" {
		t.Fatalf("want 503, got %d %v", code, out)
	}
}

func TestHE07_WithoutAToken(t *testing.T) {
	srv, ha, _ := haSetup(t)
	for _, tok := range []string{"", "wrong"} {
		if code, _ := do(t, srv, "GET", "/v1/ha/states/x.y", tok, ""); code != http.StatusUnauthorized {
			t.Fatalf("token %q: want 401, got %d", tok, code)
		}
		if code, _ := do(t, srv, "POST", "/v1/ha/services/input_select/select_option", tok, `{}`); code != http.StatusUnauthorized {
			t.Fatalf("token %q: want 401 for the action, got %d", tok, code)
		}
	}
	if n := ha.RESTCalls() + len(ha.ServiceCalls()); n != 0 {
		t.Fatalf("Home Assistant was asked %d times without a token", n)
	}
}

func TestHE08_APartnersCallIsRefusedAndShown(t *testing.T) {
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	client := haClient(t, ha.URL, ha.Token)
	waitConnected(t, client)
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	keeper, err := access.New(ctx, st, "secret", quiet)
	if err != nil {
		t.Fatal(err)
	}
	token, info, err := keeper.Make(ctx, "Elektro Meier")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(keeper.Check(New(Options{Token: "secret", HA: client, Logger: quiet})))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest("POST", srv.URL+"/v1/ha/services/input_select/select_option",
		strings.NewReader(`{"entity_id": "input_select.angehoerige_zustand", "option": "pause"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(access.HeaderPartnerUser, "anna@partner")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// A partner token reads only who it is and the state (access-tokens.md, decision 23).
	if resp.StatusCode != http.StatusForbidden || len(ha.ServiceCalls()) != 0 {
		t.Fatalf("status %d, calls %d", resp.StatusCode, len(ha.ServiceCalls()))
	}
	calls, err := keeper.Activity(ctx, info.ID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].TokenName != "Elektro Meier" || calls[0].PartnerUser != "anna@partner" ||
		calls[0].Path != "/v1/ha/services/input_select/select_option" || calls[0].Status != http.StatusForbidden {
		t.Fatalf("partner activity: %+v", calls)
	}
}

func TestHE09_ARefusedTokenRaisesTheRepair(t *testing.T) {
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	ha.SetStates([]haws.State{{EntityID: "input_boolean.x", State: "on"}})
	client := haClient(t, ha.URL, ha.Token)
	waitConnected(t, client)
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var logs bytes.Buffer
	srv := httptest.NewServer(New(Options{Token: "secret", HATokenRepair: true, HA: client, Store: st,
		Logger: slog.New(slog.NewTextHandler(&logs, nil))}))
	t.Cleanup(srv.Close)
	refused := func() (bool, bool) {
		t.Helper()
		code, out := do(t, srv, "GET", "/v1/status", "secret", "")
		if code != http.StatusOK {
			t.Fatalf("status: %d %v", code, out)
		}
		v, present := out["ha_token_refused"]
		return v == true, present
	}
	if _, present := refused(); present {
		t.Fatal("ha_token_refused is there before any refusal; an older integration must see no new key")
	}
	good := ha.Token
	ha.SetToken("revoked")
	code, out := do(t, srv, "GET", "/v1/ha/states/input_boolean.x", "secret", "")
	if code != http.StatusBadGateway || out["error"] != "Home Assistant refused Mira's token" {
		t.Fatalf("want 502 for the refused token, got %d %v", code, out)
	}
	if on, _ := refused(); !on {
		t.Fatal("/v1/status does not report the refused token")
	}
	if !strings.Contains(logs.String(), "refused Mira's token") {
		t.Fatalf("no log line for the refusal:\n%s", logs.String())
	}
	// A call that does not reach Home Assistant changes nothing.
	if code, _ := do(t, srv, "GET", "/v1/ha/states/NOT-AN-ID", "secret", ""); code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
	if on, _ := refused(); !on {
		t.Fatal("a request Home Assistant did not see took the repair away")
	}
	// The next call Home Assistant accepts takes it away.
	ha.SetToken(good)
	if code, out := do(t, srv, "GET", "/v1/ha/states/input_boolean.x", "secret", ""); code != http.StatusOK {
		t.Fatalf("want 200, got %d %v", code, out)
	}
	if _, present := refused(); present {
		t.Fatal("ha_token_refused stays after a call Home Assistant accepted")
	}
}

// HE-10: a Home Assistant token from mira.yaml (not from the integration's
// setup) is not the integration's to replace: a refusal answers 502 and is
// logged, but /v1/status does not raise the repair (docs/spec/ha-api.md,
// decision 11).
func TestHE10_ATokenFromTheFileRaisesNoRepair(t *testing.T) {
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	ha.SetStates([]haws.State{{EntityID: "input_boolean.x", State: "on"}})
	client := haClient(t, ha.URL, ha.Token)
	waitConnected(t, client)
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var logs bytes.Buffer
	srv := httptest.NewServer(New(Options{Token: "secret", HA: client, Store: st,
		Logger: slog.New(slog.NewTextHandler(&logs, nil))}))
	t.Cleanup(srv.Close)
	ha.SetToken("revoked")
	if code, out := do(t, srv, "GET", "/v1/ha/states/input_boolean.x", "secret", ""); code != http.StatusBadGateway {
		t.Fatalf("want 502 for the refused token, got %d %v", code, out)
	}
	if !strings.Contains(logs.String(), "refused Mira's token") {
		t.Fatalf("no log line for the refusal:\n%s", logs.String())
	}
	code, out := do(t, srv, "GET", "/v1/status", "secret", "")
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, out)
	}
	if _, present := out["ha_token_refused"]; present {
		t.Fatalf("a token from mira.yaml raised the repair: %v", out)
	}
}
