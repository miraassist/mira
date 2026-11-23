package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// PA-07 A mira.yaml that cannot be read starts Mira limited
// (docs/spec/parsing.md): its status carries the problem with the file, the
// line and the error, and a turn gets the sentence.
func TestPA07_AMiraYAMLThatCannotBeReadStartsMiraLimited(t *testing.T) {
	file := filepath.Join(t.TempDir(), "mira.yaml")
	if err := os.WriteFile(file, []byte("server:\n  token: geheim\nmodels:\n  main: [kaputt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, cause := config.LoadOptional(file)
	if cause == nil {
		t.Fatal("the broken file reads")
	}
	cfg := config.Limited(file)
	if cfg.Server.Token != "" {
		t.Errorf("a file whose YAML does not parse gave a token %q", cfg.Server.Token)
	}
	// A file that parses but does not fit (a field Mira does not know): its
	// token and listen address are taken for the limited start.
	fits := filepath.Join(t.TempDir(), "mira.yaml")
	if err := os.WriteFile(fits, []byte("server:\n  token: geheim\n  listen: 127.0.0.1:9999\nunbekannt: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.LoadOptional(fits); err == nil {
		t.Fatal("a file with an unknown field reads")
	}
	if c := config.Limited(fits); c.Server.Token != "geheim" || c.Server.Listen != "127.0.0.1:9999" {
		t.Errorf("limited from a file that parses: %+v", c.Server)
	}
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p := prepared{cfg: config.Default()}
	p.cfg.Server.Token = "tok"
	srv := httptest.NewServer(limitedMux(st, p, "id-1", file, cause))
	defer srv.Close()

	get := func(method, path, body string) (*http.Response, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp, string(raw)
	}
	resp, body := get(http.MethodGet, "/v1/status", "")
	var status struct {
		Problems []struct{ Source, Name, Text string } `json:"problems"`
	}
	if err := json.Unmarshal([]byte(body), &status); err != nil || resp.StatusCode != 200 {
		t.Fatalf("status %d %q: %v", resp.StatusCode, body, err)
	}
	if len(status.Problems) != 1 || status.Problems[0].Source != "config" ||
		!strings.Contains(status.Problems[0].Text, file) || !strings.Contains(status.Problems[0].Text, "line 3") {
		t.Errorf("problems %+v", status.Problems)
	}
	_, said := get(http.MethodPost, "/v1/conversation", `{"text":"Licht an","conversation_id":"c1","language":"de"}`)
	if !strings.Contains(said, "Einstellungen gerade nicht lesen") || !strings.Contains(said, `"conversation_id":"c1"`) {
		t.Errorf("turn %s", said)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/status", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without the token: %v %v", resp, err)
	}
}

// PA-08 A corrected mira.yaml starts Mira anew: a change that still does not
// read is logged, one that reads starts anew.
func TestPA08_ACorrectedMiraYAMLStartsMiraAnew(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "mira.yaml")
	if err := os.WriteFile(file, []byte("models:\n  main: [kaputt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	log, lines := logcap.Logger()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchConfig(ctx, clk, file, log, func() { close(started) })
	<-clk.AwaitWaiters(1)

	later := time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC) // a file time ahead of any the test made
	if err := os.WriteFile(file, []byte("models:\n  main: [immer noch kaputt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(file, later, later) // a change the stamp sees, whatever the file system's resolution
	clk.Advance(configWatch)
	<-lines.Await("still cannot be read")

	if err := os.WriteFile(file, []byte("server:\n  name: Mira\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	evenLater := later.Add(time.Hour)
	_ = os.Chtimes(file, evenLater, evenLater)
	<-clk.AwaitWaiters(1)
	clk.Advance(configWatch)
	<-started
}
