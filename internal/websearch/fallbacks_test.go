package websearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// A hit's page cannot be read: the next hit is taken, and the log names the
// address and why.
func TestFB28_a_hits_page_that_cannot_be_read_is_in_the_log(t *testing.T) {
	page := fixture(t, "page.html")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		w.Write(page)
	}))
	t.Cleanup(site.Close)
	results := `<div id="links" class="results">` +
		`<div class="result"><h2 class="result__title"><a class="result__a" href="` + site.URL + `/gone">Treffer A</a></h2><a class="result__snippet" href="x">Kurztext A</a></div>` +
		`<div class="result"><h2 class="result__title"><a class="result__a" href="` + site.URL + `/page">Treffer B</a></h2><a class="result__snippet" href="x">Kurztext B</a></div></div>`
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(results)) }))
	t.Cleanup(ddg.Close)
	log, lines := logcap.Logger()
	s := New(Options{Lang: i18n.DE, BingURL: noBing(t), DuckDuckGoURL: ddg.URL, HTTP: http.DefaultClient, AllowPrivate: true, Logger: log})
	if out := run(t, s, ToolSearch, `{"query":"x"}`); !strings.Contains(out, "Anfang von Treffer 2") {
		t.Errorf("the next hit was not taken:\n%s", out)
	}
	if lines.Line("a hit's page cannot be read", "url="+site.URL+"/gone", "HTTP 404") == "" {
		t.Errorf("no line for the page:\n%s", lines)
	}
}
