package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

// mira is a Mira in a folder of her own: a database with a fact, a
// mira.yaml, secrets (encrypted with key, if given) and one own skill.
func mira(t *testing.T, key, fact string) Mira {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "mira.db")
	st, err := store.Open(context.Background(), store.Options{Path: db})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.SaveFact(context.Background(), store.Fact{ID: "f1", Text: fact, Owner: store.OwnerHouse}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKV(context.Background(), "setup.token", "token-"+fact); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "mira.yaml")
	if err := os.WriteFile(cfg, []byte("# "+fact+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := config.Secrets{Path: filepath.Join(dir, "secrets.json"), Key: key}
	if err := secrets.Set(map[string]string{"MIRA_MODEL_MAIN_TOKEN": "sk-" + fact}); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dir, "my-skills")
	if err := os.MkdirAll(filepath.Join(own, "blumen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "blumen", "SKILL.md"), []byte("---\nname: blumen\n---\n"+fact+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Mira{Store: st, DBPath: db, ConfigPath: cfg, Secrets: secrets, SkillDirs: []string{own}, DataDir: dir, Version: "1.0.0"}
}

func snapshot(t *testing.T, m Mira) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Make(context.Background(), m, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// files are the archive's files by name.
func files(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(tr)
		out[h.Name] = string(raw)
	}
}

func TestBK01_the_snapshot_holds_the_database_the_settings_and_ones_own_skills(t *testing.T) {
	m := mira(t, "", "alt")
	got := files(t, snapshot(t, m))
	for _, name := range []string{"mira.db", "mira.yaml", "secrets.json", "skills/blumen/SKILL.md", "manifest.json"} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s missing in %v", name, keys(got))
		}
	}
	if !strings.Contains(got["manifest.json"], `"blumen"`) || !strings.Contains(got["manifest.json"], `"version": "1.0.0"`) {
		t.Errorf("manifest: %s", got["manifest.json"])
	}
	for name := range got {
		if strings.HasPrefix(name, "skills/") && !strings.HasPrefix(name, "skills/blumen/") {
			t.Errorf("a skill not of one's own: %s", name)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBK02_the_snapshot_is_consistent_while_mira_writes(t *testing.T) {
	m := mira(t, "", "alt")
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			m.Store.SaveFact(context.Background(), store.Fact{ID: fmt.Sprint("w", i), Text: strings.Repeat("x", 200), Owner: store.OwnerHouse})
		}
	}()
	time.Sleep(20 * time.Millisecond)
	archive := snapshot(t, m)
	cancel()
	wg.Wait()
	db := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(db, []byte(files(t, archive)["mira.db"]), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var ok string
	if err := conn.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity: %q %v", ok, err)
	}
}

// restoreInto restores the archive into m and opens her database anew, as
// her restart does.
func restoreInto(t *testing.T, m Mira, archive []byte) (Result, *store.Store) {
	t.Helper()
	res, err := Restore(context.Background(), m, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	m.Store.Close()
	st, err := store.Open(context.Background(), store.Options{Path: m.DBPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return res, st
}

func TestBK03_a_restore_replaces_the_data_and_keeps_the_pairing(t *testing.T) {
	old := mira(t, "", "alt")
	archive := snapshot(t, old)
	m := mira(t, "", "neu")
	res, st := restoreInto(t, m, archive)
	facts, err := st.AllFacts(context.Background(), store.OwnerHouse)
	if err != nil || len(facts) != 1 || facts[0].Text != "alt" {
		t.Errorf("facts: %+v %v", facts, err)
	}
	if token, _ := st.GetKV(context.Background(), "setup.token"); token != "token-neu" {
		t.Errorf("pairing: %q", token)
	}
	if raw, _ := os.ReadFile(m.ConfigPath); string(raw) != "# alt\n" || res.Config != "written" {
		t.Errorf("mira.yaml: %q (%s)", raw, res.Config)
	}
	if raw, _ := os.ReadFile(filepath.Join(m.SkillDirs[0], "blumen", "SKILL.md")); !strings.Contains(string(raw), "alt") {
		t.Errorf("skill: %q", raw)
	}
	if s, _ := m.Secrets.Load(); s["MIRA_MODEL_MAIN_TOKEN"] != "sk-alt" {
		t.Errorf("secrets: %v", s)
	}
	if _, err := os.Stat(m.DBPath + store.RestoreSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the restored database was not put in place: %v", err)
	}
}

// unchanged says whether m still has her own fact, mira.yaml and secrets.
func unchanged(t *testing.T, m Mira, fact string) {
	t.Helper()
	if _, err := os.Stat(m.DBPath + store.RestoreSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Error("a database was laid down")
	}
	if raw, _ := os.ReadFile(m.ConfigPath); string(raw) != "# "+fact+"\n" {
		t.Errorf("mira.yaml changed: %q", raw)
	}
	if s, _ := m.Secrets.Load(); s["MIRA_MODEL_MAIN_TOKEN"] != "sk-"+fact {
		t.Errorf("secrets changed: %v", s)
	}
}

func TestBK04_a_snapshot_of_a_newer_mira_is_refused(t *testing.T) {
	old := mira(t, "", "alt")
	if err := old.Store.SetKV(context.Background(), "x", "y"); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", "file:"+old.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES ('9999_future', 'x')`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	archive := snapshot(t, old)
	m := mira(t, "", "neu")
	_, err = Restore(context.Background(), m, bytes.NewReader(archive))
	var refused Refused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "newer Mira") {
		t.Fatalf("restore: %v", err)
	}
	unchanged(t, m, "neu")
}

func TestBK05_a_broken_archive_changes_nothing(t *testing.T) {
	m := mira(t, "", "neu")
	broken := func(files map[string]string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))})
			tw.Write([]byte(body))
		}
		tw.Close()
		gz.Close()
		return buf.Bytes()
	}
	for name, archive := range map[string][]byte{
		"not gzip":       []byte("hello"),
		"no manifest":    broken(map[string]string{"mira.db": "x"}),
		"no database":    broken(map[string]string{"manifest.json": "{}"}),
		"bad database":   broken(map[string]string{"manifest.json": "{}", "mira.db": "not a database"}),
		"leaves the dir": broken(map[string]string{"../evil": "x", "manifest.json": "{}"}),
	} {
		_, err := Restore(context.Background(), m, bytes.NewReader(archive))
		var refused Refused
		if !errors.As(err, &refused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	unchanged(t, m, "neu")
}

func TestBK06_the_secrets_go_decrypted_the_key_never(t *testing.T) {
	old := mira(t, "alter-schluessel", "alt")
	got := files(t, snapshot(t, old))
	if !strings.Contains(got["secrets.json"], "sk-alt") {
		t.Errorf("secrets in the archive: %q", got["secrets.json"])
	}
	for name, body := range got {
		if strings.Contains(body, "alter-schluessel") {
			t.Errorf("the key is in %s", name)
		}
	}
	m := mira(t, "neuer-schluessel", "neu")
	restoreInto(t, m, snapshot(t, old))
	raw, _ := os.ReadFile(m.Secrets.Path)
	if strings.Contains(string(raw), "sk-alt") {
		t.Error("the restored secrets are not encrypted")
	}
	if s, err := m.Secrets.Load(); err != nil || s["MIRA_MODEL_MAIN_TOKEN"] != "sk-alt" {
		t.Errorf("with the new key: %v %v", s, err)
	}
}

func TestBK07_a_read_only_mira_yaml_is_left_its_copy_kept(t *testing.T) {
	archive := snapshot(t, mira(t, "", "alt"))
	m := mira(t, "", "neu")
	if err := os.Chmod(m.ConfigPath, 0o400); err != nil {
		t.Fatal(err)
	}
	if (config.File{Path: m.ConfigPath}).Writable() == nil {
		t.Skip("running as a user who can write a read-only file")
	}
	res, st := restoreInto(t, m, archive)
	if res.Config != "read_only" {
		t.Errorf("config: %s", res.Config)
	}
	if raw, _ := os.ReadFile(m.ConfigPath); string(raw) != "# neu\n" {
		t.Errorf("mira.yaml: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(m.DataDir, RestoredConfig)); string(raw) != "# alt\n" {
		t.Errorf("%s: %q", RestoredConfig, raw)
	}
	if facts, _ := st.AllFacts(context.Background(), store.OwnerHouse); len(facts) != 1 || facts[0].Text != "alt" {
		t.Errorf("the rest was not restored: %+v", facts)
	}
}
