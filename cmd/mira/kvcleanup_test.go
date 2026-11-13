package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/store"
)

// docs/spec/kv-cleanup.md, decision 9: what the integration set covers
// mira.yaml, and the log says which settings, never their values.

func coveredLog(t *testing.T, yaml, haURL, haToken string) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db := filepath.Join(dir, "mira.db")
	path := filepath.Join(dir, "mira.yaml")
	if err := os.WriteFile(path, []byte(yaml+"store:\n  path: "+db+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, store.Options{Path: db})
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.Save(ctx, st, haURL, haToken, nil); err != nil {
		t.Fatal(err)
	}
	st.Close()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	t.Setenv("MIRA_DATA", "")
	p, err := prepare(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.HA.URL != strings.TrimRight(haURL, "/") || p.cfg.HA.Token != haToken {
		t.Fatalf("ha = %q %q", p.cfg.HA.URL, p.cfg.HA.Token)
	}
	return buf.String()
}

func TestKV12_TheSetupsValueCoversMiraYamlAndTheLogSaysSo(t *testing.T) {
	log := coveredLog(t, "ha:\n  url: http://a.example:8123\n  token: same\n", "http://b.example:8123", "same")
	if !strings.Contains(log, "setup covers mira.yaml") || !strings.Contains(log, "ha.url") ||
		strings.Contains(log, "ha.token") {
		t.Fatalf("log = %s", log)
	}
	if quiet := coveredLog(t, "ha:\n  url: http://b.example:8123/\n  token: t\n", "http://b.example:8123", "t"); strings.Contains(quiet, "covers") {
		t.Fatalf("the same address is said: %s", quiet)
	}
}

func TestKV13_ACoveredTokenIsNamedNeverItsValue(t *testing.T) {
	log := coveredLog(t, "ha:\n  url: http://a.example:8123\n  token: file-secret-1\n", "http://a.example:8123", "setup-secret-2")
	if !strings.Contains(log, "ha.token") || strings.Contains(log, "file-secret-1") || strings.Contains(log, "setup-secret-2") {
		t.Fatalf("log = %s", log)
	}
}
