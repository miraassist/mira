package programs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// publisher serves a program at /korb_<arch> and counts the downloads.
func publisher(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/korb_") {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func program(url, sum, version string) skills.Program {
	return skills.Program{Skill: "rewe", Name: "korb", Version: version, URL: url, SHA256: sum}
}

// waitReady waits for the background download.
func waitReady(t *testing.T, ready chan string) string {
	t.Helper()
	select {
	case s := <-ready:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("the program never became ready")
		return ""
	}
}

// PG-01 Mira gets the skill's program: downloaded once, checked, kept
// executable under programs/, and the skill's server is connected then.
func TestPG01_MiraGetsTheSkillsProgram(t *testing.T) {
	body := []byte("#!/bin/sh\necho korb\n")
	srv, hits := publisher(t, body)
	dir := t.TempDir()
	m := New(dir, nil)
	p := program(srv.URL+"/korb_amd64", sum(body), "0.9.0")
	ready := make(chan string, 1)
	m.Start(context.Background(), []skills.Program{p}, func(s string) { ready <- s })
	if !m.Loading("rewe") && hits.Load() == 0 {
		t.Error("not loading while the download runs")
	}
	if s := waitReady(t, ready); s != "rewe" {
		t.Fatalf("ready for %q", s)
	}
	st, err := os.Stat(m.Path(p))
	if err != nil || st.Mode().Perm() != 0o755 || filepath.Dir(m.Path(p)) != dir {
		t.Fatalf("kept: %v %v", st, err)
	}
	if got, _ := os.ReadFile(m.Path(p)); string(got) != string(body) || m.Loading("rewe") || len(m.Problems()) != 0 {
		t.Fatalf("file %q, loading %v, problems %v", got, m.Loading("rewe"), m.Problems())
	}
}

// PG-03 A program that is there is not downloaded again; one changed on
// disk is got anew.
func TestPG03_AProgramThatIsThereIsNotDownloadedAgain(t *testing.T) {
	body := []byte("korb")
	srv, hits := publisher(t, body)
	dir := t.TempDir()
	p := program(srv.URL+"/korb_amd64", sum(body), "0.9.0")
	ready := make(chan string, 1)
	New(dir, nil).Start(context.Background(), []skills.Program{p}, func(s string) { ready <- s })
	waitReady(t, ready)
	m := New(dir, nil)
	m.Start(context.Background(), []skills.Program{p}, func(string) { t.Error("downloaded again") })
	if m.Loading("rewe") || hits.Load() != 1 {
		t.Fatalf("again: loading %v, %d downloads", m.Loading("rewe"), hits.Load())
	}
	os.WriteFile(m.Path(p), []byte("changed"), 0o755)
	New(dir, nil).Start(context.Background(), []skills.Program{p}, func(s string) { ready <- s })
	waitReady(t, ready)
	if got, _ := os.ReadFile(m.Path(p)); string(got) != "korb" || hits.Load() != 2 {
		t.Fatalf("changed file: %q, %d downloads", got, hits.Load())
	}
}

// PG-02 A wrong sum is never kept or started; the problem names both sums.
func TestPG02_AWrongSumIsNeverStarted(t *testing.T) {
	srv, _ := publisher(t, []byte("evil"))
	dir := t.TempDir()
	m := New(dir, nil)
	want := sum([]byte("good"))
	p := program(srv.URL+"/korb_amd64", want, "0.9.0")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx, []skills.Program{p}, func(string) { t.Error("started") })
	deadline := time.Now().Add(5 * time.Second)
	for len(m.Problems()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	probs := m.Problems()
	if len(probs) != 1 {
		t.Fatalf("problems %v", probs)
	}
	text := probs[0].Text(i18n.DE)
	if probs[0].Skill != "rewe" || !strings.Contains(text, want) || !strings.Contains(text, sum([]byte("evil"))) || !strings.Contains(text, srv.URL) {
		t.Errorf("problem %q", text)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("written: %v", entries)
	}
	if !m.Loading("rewe") {
		t.Error("a failed program is not loading any more")
	}
}

// PG-04 A new version is a new file; the old one goes at the start that
// no longer names it.
func TestPG04_ANewVersionComesWithTheSkillOnly(t *testing.T) {
	old, newer := []byte("v1"), []byte("v2")
	srvOld, _ := publisher(t, old)
	srvNew, _ := publisher(t, newer)
	dir := t.TempDir()
	pOld := program(srvOld.URL+"/korb_amd64", sum(old), "0.8.0")
	ready := make(chan string, 1)
	New(dir, nil).Start(context.Background(), []skills.Program{pOld}, func(s string) { ready <- s })
	waitReady(t, ready)
	os.WriteFile(filepath.Join(dir, "unknown-1-000000000000"), []byte("x"), 0o755)

	pNew := program(srvNew.URL+"/korb_amd64", sum(newer), "0.9.0")
	m := New(dir, nil)
	m.Start(context.Background(), []skills.Program{pNew}, func(s string) { ready <- s })
	waitReady(t, ready)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != pNew.FileName() {
		t.Fatalf("left: %v", entries)
	}
}

// PG-05 A failed download is said and tried again later, not in a loop.
func TestPG05_AFailedDownloadIsTriedAgainLater(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var hits atomic.Int32
	body := []byte("korb")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	m := New(t.TempDir(), nil)
	m.retry = 200 * time.Millisecond
	ready := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx, []skills.Program{program(srv.URL+"/korb_amd64", sum(body), "0.9.0")}, func(s string) { ready <- s })
	time.Sleep(50 * time.Millisecond)
	if probs := m.Problems(); len(probs) != 1 || !strings.Contains(probs[0].Text(i18n.DE), "HTTP 503") {
		t.Fatalf("problems %v", probs)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d tries at once", n)
	}
	fail.Store(false)
	waitReady(t, ready)
	if len(m.Problems()) != 0 {
		t.Errorf("problem stays: %v", m.Problems())
	}
}

// Only https, or http on this machine.
func TestOnlyHTTPS(t *testing.T) {
	for url, ok := range map[string]bool{"https://github.com/x": true, "http://127.0.0.1:8080/x": true, "http://example.com/x": false} {
		if (allowed(url) == nil) != ok {
			t.Errorf("%s: %v", url, allowed(url))
		}
	}
}
