package companions

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

const integration = "the-integrations-token"

type fixture struct {
	t         *testing.T
	h         *Handler
	k         *access.Keeper
	srv       http.Handler
	logs      *bytes.Buffer
	restarted chan struct{}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, logs: &bytes.Buffer{}, restarted: make(chan struct{}, 1)}
	log := slog.New(slog.NewTextHandler(f.logs, nil))
	f.k, err = access.New(ctx, st, integration, log)
	if err != nil {
		t.Fatal(err)
	}
	f.h = &Handler{Store: st, Log: log, Restart: func() { f.restarted <- struct{}{} }}
	mux := api.NewMux()
	f.h.Register(mux)
	// The page's routes as internal/ui mounts them (its admin check is the page's own).
	mux.Hidden("GET /ui/companion/{name}/{path...}", "test", http.HandlerFunc(f.h.PassPage))
	mux.Hidden("GET /v1/ui/companion/{name}/{path...}", "test", http.HandlerFunc(f.h.PassData))
	mux.Hidden("GET /v1/ui/companions", "test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list, _ := f.h.List(r.Context())
		writeJSON(w, http.StatusOK, list)
	}))
	f.srv = f.k.Check(mux)
	return f
}

func (f *fixture) partner(name string) string {
	f.t.Helper()
	tok, _, err := f.k.Make(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

func (f *fixture) call(method, path, bearer string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func TestBS01_ACompanionRegistersWithItsPartnerToken(t *testing.T) {
	f := newFixture(t)
	tok := f.partner("Pro")
	rec := f.call("PUT", "/v1/companions/pro", tok, Registration{Title: "Pro", URL: "http://mira-pro:8792", Token: "companion-secret"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("answer %d %s", rec.Code, rec.Body)
	}
	list, err := f.h.List(context.Background())
	if err != nil || len(list) != 1 || list[0] != (Companion{Name: "pro", Title: "Pro"}) {
		t.Errorf("list %+v %v", list, err)
	}
	if !strings.Contains(f.logs.String(), "companion registered") || !strings.Contains(f.logs.String(), "http://mira-pro:8792") ||
		strings.Contains(f.logs.String(), "companion-secret") {
		t.Errorf("log %s", f.logs)
	}
}

func TestBS02_AWrongRegistrationIsRefusedPerField(t *testing.T) {
	f := newFixture(t)
	rec := f.call("PUT", "/v1/companions/Pro!", f.partner("Pro"), Registration{Title: "Pro", Token: "x"})
	var e Errors
	json.NewDecoder(rec.Body).Decode(&e)
	if rec.Code != http.StatusBadRequest || e.Errors["name"] == "" || e.Errors["url"] == "" {
		t.Errorf("answer %d %+v", rec.Code, e)
	}
}

func TestBS03_AnotherTokenMayNotTakeAName(t *testing.T) {
	f := newFixture(t)
	reg := Registration{Title: "Pro", URL: "http://a:1", Token: "x"}
	if rec := f.call("PUT", "/v1/companions/pro", f.partner("One"), reg); rec.Code != http.StatusNoContent {
		t.Fatalf("first %d", rec.Code)
	}
	reg.URL = "http://b:2"
	if rec := f.call("PUT", "/v1/companions/pro", f.partner("Two"), reg); rec.Code != http.StatusConflict {
		t.Errorf("second %d %s", rec.Code, rec.Body)
	}
	all, _ := f.h.Store.Companions(context.Background())
	if len(all) != 1 || all[0].URL != "http://a:1" {
		t.Errorf("registration changed %+v", all)
	}
}

func TestBS04_APartnerTokenStillReachesNothingElse(t *testing.T) {
	f := newFixture(t)
	tok := f.partner("Pro")
	for _, c := range [][2]string{{"GET", "/v1/ui/companions"}, {"POST", "/v1/conversation"}, {"GET", "/v1/ui/companion/pro/state"}} {
		if rec := f.call(c[0], c[1], tok, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", c[0], c[1], rec.Code)
		}
	}
}

func TestBS05_TheCompanionsPageAndDataArePassedOn(t *testing.T) {
	f := newFixture(t)
	var gotAuth string
	comp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ui/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<title>Pro</title>"))
		case "/api/state":
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tier":"pro"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(comp.Close)
	f.call("PUT", "/v1/companions/pro", f.partner("Pro"), Registration{Title: "Pro", URL: comp.URL, Token: "companion-secret"})
	if rec := f.call("GET", "/ui/companion/pro/", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Pro</title>") {
		t.Errorf("page %d %s", rec.Code, rec.Body)
	}
	rec := f.call("GET", "/v1/ui/companion/pro/state", integration, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"tier":"pro"`) {
		t.Errorf("data %d %s", rec.Code, rec.Body)
	}
	if gotAuth != "Bearer companion-secret" {
		t.Errorf("the companion got %q, not its own token", gotAuth)
	}
	if rec := f.call("GET", "/ui/companion/other/", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown companion: %d", rec.Code)
	}
}

func TestBS06_AnUnreachableCompanionIsSaid(t *testing.T) {
	f := newFixture(t)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	f.call("PUT", "/v1/companions/pro", f.partner("Pro"), Registration{Title: "Pro", URL: gone.URL, Token: "x"})
	rec := f.call("GET", "/v1/ui/companion/pro/state", integration, nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "companion pro cannot be reached") {
		t.Errorf("answer %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(f.logs.String(), "a companion cannot be reached") {
		t.Errorf("no log line")
	}
}

func TestBS08_AReloadRestartsMira(t *testing.T) {
	f := newFixture(t)
	if rec := f.call("POST", "/v1/skills/reload", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d", rec.Code)
	}
	if rec := f.call("POST", "/v1/skills/reload", f.partner("Pro"), nil); rec.Code != http.StatusAccepted {
		t.Fatalf("answer %d %s", rec.Code, rec.Body)
	}
	<-f.restarted
}
