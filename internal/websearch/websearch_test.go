package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The results page gives the hits in order, without ads, with DuckDuckGo's
// redirect undone and the snippet's markup gone; the query and region go
// along.
func TestQuery(t *testing.T) {
	page := fixture(t, "results.html")
	var got sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = sent{q: r.PostForm.Get("q"), kl: r.PostForm.Get("kl")}
		w.Write(page)
	}))
	t.Cleanup(srv.Close)
	s := New(Options{Lang: i18n.DE, SearchURL: srv.URL, HTTP: srv.Client()})
	results, err := s.Query(context.Background(), "Baumarkt Sonntag")
	if err != nil {
		t.Fatal(err)
	}
	if got.q != "Baumarkt Sonntag" || got.kl != "de-de" {
		t.Errorf("sent %+v", got)
	}
	want := []Result{
		{Title: "Öffnungszeiten im Baumarkt", URL: "https://example.org/baumarkt/oeffnungszeiten", Snippet: "Montag bis Samstag 8 bis 20 Uhr, sonntags geschlossen."},
		{Title: "Verkaufsoffene Sonntage", URL: "https://example.net/sonntag", Snippet: "Termine für verkaufsoffene Sonntage."},
		{Title: "Ohne Kurztext", URL: "https://example.com/ohne-text"},
	}
	if len(results) != len(want) {
		t.Fatalf("results: %+v", results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, results[i], want[i])
		}
	}
}

type sent struct{ q, kl string }

// DuckDuckGo's bot check (here as HTTP 202, as it sends it) makes the
// search ask Bing, whose links are unpacked; when both fail, the tool says
// plainly that it could not look it up, so the model does not guess.
func TestFallback(t *testing.T) {
	blocked := fixture(t, "blocked.html")
	bing := fixture(t, "bing.html")
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write(blocked)
	}))
	t.Cleanup(ddg.Close)
	var asked string
	fb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("q") + "|" + r.URL.Query().Get("setlang")
		w.Write(bing)
	}))
	t.Cleanup(fb.Close)
	s := New(Options{Lang: i18n.DE, SearchURL: ddg.URL, FallbackURL: fb.URL, HTTP: ddg.Client()})
	results, err := s.Query(context.Background(), "Baumarkt Sonntag")
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Title: "Öffnungszeiten im Baumarkt", URL: "https://example.org/baumarkt/oeffnungszeiten", Snippet: "Montag bis Samstag 8 bis 20 Uhr, sonntags geschlossen."},
		{Title: "Direkter Link", URL: "https://example.net/direkt", Snippet: "Ohne Umweg."},
	}
	if len(results) != len(want) || results[0] != want[0] || results[1] != want[1] {
		t.Errorf("results: %+v", results)
	}
	if asked != "Baumarkt Sonntag|de" {
		t.Errorf("Bing was asked %q", asked)
	}

	fb.Close()
	en := New(Options{Lang: i18n.EN, SearchURL: ddg.URL, FallbackURL: fb.URL, HTTP: ddg.Client()})
	if _, err := en.Query(context.Background(), "x"); err != ErrBlocked {
		t.Fatalf("both failed, got %v", err)
	}
	for _, tool := range en.Tools() {
		if tool.Name != ToolSearch {
			continue
		}
		_, err := tool.Run(context.Background(), tools.Invocation{Name: ToolSearch, Args: json.RawMessage(`{"query":"x"}`)})
		if err == nil || !strings.Contains(err.Error(), "do not guess") {
			t.Errorf("tool error %v", err)
		}
	}
}

// A search brings the start of the first readable hit with it: a first hit
// that cannot be read is passed over, and only the first two are tried.
func TestSearchExcerpt(t *testing.T) {
	page := fixture(t, "page.html")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			http.NotFound(w, r)
		case "/empty":
			w.Write([]byte("<html><body><script>build()</script></body></html>"))
		default:
			w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
			w.Write(page)
		}
	}))
	t.Cleanup(site.Close)
	search := func(paths ...string) string {
		var b strings.Builder
		b.WriteString(`<div id="links" class="results">`)
		for i, p := range paths {
			c := string(rune('A' + i))
			b.WriteString(`<div class="result"><h2 class="result__title"><a class="result__a" href="` + site.URL + p + `">Treffer ` + c + `</a></h2><a class="result__snippet" href="x">Kurztext ` + c + `</a></div>`)
		}
		b.WriteString(`</div>`)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(b.String())) }))
		t.Cleanup(srv.Close)
		// The search page and the site are both on the loopback interface.
		return run(t, New(Options{Lang: i18n.DE, SearchURL: srv.URL, HTTP: http.DefaultClient, AllowPrivate: true}), ToolSearch, `{"query":"x"}`)
	}

	out := search("/gone", "/page")
	if !strings.Contains(out, "1. Treffer A") || !strings.Contains(out, "2. Treffer B") {
		t.Errorf("hits missing:\n%s", out)
	}
	if !strings.Contains(out, "Anfang von Treffer 2 ("+site.URL+"/page):") || !strings.Contains(out, "Montag bis Samstag von 8 bis 20 Uhr") {
		t.Errorf("no excerpt of the second hit:\n%s", out)
	}
	if strings.Contains(out, "Impressum") || strings.Contains(out, "Newsletter") {
		t.Errorf("excerpt holds page furniture:\n%s", out)
	}
	out = search("/gone", "/empty", "/page")
	if strings.Contains(out, "Anfang von Treffer") || !strings.Contains(out, "3. Treffer C") {
		t.Errorf("tried more than two hits, or lost hits:\n%s", out)
	}
}

// A page gives its title and main text: Latin-1 decoded, without scripts,
// navigation, header, footer, aside, form and hidden parts.
func TestRead(t *testing.T) {
	page := fixture(t, "page.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		w.Write(page)
	}))
	t.Cleanup(srv.Close)
	s := New(Options{Lang: i18n.DE, AllowPrivate: true})
	p, err := s.Read(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Öffnungszeiten - Baumarkt am See" {
		t.Errorf("title %q", p.Title)
	}
	for _, want := range []string{"Öffnungszeiten\n", "Montag bis Samstag von 8 bis 20 Uhr", "Gartencenter: ab 7 Uhr\nHolzzuschnitt", "Parkplätze"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("missing %q in\n%s", want, p.Text)
		}
	}
	for _, not := range []string{"tracking", "color", "Angebote", "Newsletter", "Suchen", "Impressum", "Versteckter"} {
		if strings.Contains(p.Text, not) {
			t.Errorf("%q should be left out:\n%s", not, p.Text)
		}
	}
}

// Pages in the own network, other schemes and non-HTML are refused.
func TestReadRefuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF"))
	}))
	t.Cleanup(srv.Close)
	s := New(Options{Lang: i18n.DE})
	if _, err := s.Read(context.Background(), srv.URL); err != errPrivate {
		t.Errorf("loopback: %v", err)
	}
	if out := run(t, s, ToolPage, `{"url":"http://192.168.1.1/"}`); !strings.Contains(out, "eigenen Netz") {
		t.Errorf("private address: %q", out)
	}
	if _, err := s.Read(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("file: was read")
	}
	open := New(Options{Lang: i18n.DE, AllowPrivate: true})
	if _, err := open.Read(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "application/pdf") {
		t.Errorf("pdf: %v", err)
	}
}

// Both tools only read and carry web content; switched off, the registry
// neither offers nor runs them.
func TestToolsSwitch(t *testing.T) {
	on := true
	s := New(Options{Lang: i18n.DE, Enabled: func() bool { return on }})
	reg := tools.NewRegistry(nil, s.Tools()...)
	for _, name := range []string{ToolSearch, ToolPage} {
		spec, ok := reg.Lookup(name)
		if !ok || !spec.ReadOnly || !spec.WebContent {
			t.Errorf("%s: %+v %v", name, spec, ok)
		}
	}
	if n := len(reg.Specs(true)); n != 2 {
		t.Errorf("background jobs get %d tools", n)
	}
	on = false
	if _, ok := reg.Lookup(ToolSearch); ok || len(reg.Specs(false)) != 0 {
		t.Error("switched off, still offered")
	}
	if _, _, err := reg.Execute(context.Background(), tools.Invocation{Name: ToolSearch, Args: json.RawMessage(`{"query":"x"}`)}); err == nil {
		t.Error("switched off, still run")
	}
}

func run(t *testing.T, s *Search, name, args string) string {
	t.Helper()
	for _, tool := range s.Tools() {
		if tool.Name == name {
			out, err := tool.Run(context.Background(), tools.Invocation{Name: name, Args: json.RawMessage(args)})
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

// TestLive asks the real DuckDuckGo and Bing and reads the first hit, to
// notice when a results page changed. Only with MIRA_LIVE_SEARCH=1.
func TestLive(t *testing.T) {
	if os.Getenv("MIRA_LIVE_SEARCH") != "1" {
		t.Skip("MIRA_LIVE_SEARCH=1 runs it against the real DuckDuckGo")
	}
	s := New(Options{Lang: i18n.DE})
	if r, err := s.bing(context.Background(), "Wikipedia Eiffelturm"); err != nil || len(r) == 0 {
		t.Fatalf("bing: %v %+v", err, r)
	} else {
		t.Logf("bing: %d hits, first %s", len(r), r[0].URL)
	}
	results, err := s.Query(context.Background(), "Wikipedia Eiffelturm")
	if err != nil || len(results) == 0 {
		t.Fatalf("search: %v %+v", err, results)
	}
	for _, r := range results {
		t.Logf("%s | %s | %.80s", r.Title, r.URL, r.Snippet)
	}
	p, err := s.Read(context.Background(), results[0].URL)
	if err != nil || len(p.Text) < 200 {
		t.Fatalf("page %s: %v (%d chars)", results[0].URL, err, len(p.Text))
	}
	t.Logf("page %q: %d chars, cut %v\n%.300s", p.Title, len(p.Text), p.Cut, p.Text)
}
