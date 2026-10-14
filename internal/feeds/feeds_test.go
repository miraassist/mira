package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestParseRSS(t *testing.T) {
	body, _ := os.ReadFile("testdata/rss.xml")
	items, err := Parse(body)
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if items[0].ID != "zenmux:notification:2642SNAiRE0q14614094" || items[0].Title != "New Model Available: Gemini 4 Argon" || items[0].Published.IsZero() {
		t.Errorf("first item = %+v", items[0])
	}
	if items[1].ID != "https://example.org/qwen" || items[1].Title != "Qwen & Co" || items[1].Summary != "Ein neues Modell." {
		t.Errorf("second item = %+v", items[1])
	}
}

func TestParseAtom(t *testing.T) {
	body, _ := os.ReadFile("testdata/atom.xml")
	items, err := Parse(body)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if it := items[0]; it.ID != "urn:uuid:1" || it.Link != "https://example.org/llama6" || it.Summary != "Kurzfassung" || it.Published.IsZero() {
		t.Errorf("item = %+v", it)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "testdata/rss.xml")
	}))
	defer srv.Close()
	items, err := Fetch(context.Background(), nil, srv.URL+"/feed")
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %d, err = %v", len(items), err)
	}
	if _, err := Fetch(context.Background(), nil, srv.URL+"/nope"); err == nil {
		t.Error("404 accepted")
	}
	if _, err := Parse([]byte("<html></html>")); err == nil {
		t.Error("html accepted as a feed")
	}
}
