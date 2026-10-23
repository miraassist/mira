package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const editSample = `# Mira for the test house
models:
  main:
    endpoint: http://localhost:1234/v1   # LM Studio
    model: qwen
    privacy: local
agents:
  delegate: { model: main }
`

func TestBE01_EditKeepsCommentsAndChangesOnlyTheEntry(t *testing.T) {
	dir := t.TempDir()
	f := File{Path: filepath.Join(dir, "mira.yaml"), Secrets: Secrets{Path: filepath.Join(dir, "secrets.json")}}
	if err := os.WriteFile(f.Path, []byte(editSample), 0o640); err != nil {
		t.Fatal(err)
	}
	err := f.Edit(func(top *yaml.Node) error {
		m, err := NodeOf(map[string]any{"endpoint": "https://example.com/v1", "model": "flash", "privacy": "cloud"})
		if err != nil {
			return err
		}
		MapSet(MapChild(top, "models"), "smart", m)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(f.Path)
	out := string(raw)
	for _, want := range []string{"# Mira for the test house", "# LM Studio", "smart:", "model: flash"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if st, _ := os.Stat(f.Path); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
}

func TestBE02_AnEditMiraWouldRefuseIsNotWritten(t *testing.T) {
	dir := t.TempDir()
	f := File{Path: filepath.Join(dir, "mira.yaml"), Secrets: Secrets{Path: filepath.Join(dir, "secrets.json")}}
	os.WriteFile(f.Path, []byte(editSample), 0o644)
	err := f.Edit(func(top *yaml.Node) error {
		m, _ := NodeOf(map[string]any{"endpoint": "https://example.com/v1", "model": "x"}) // no privacy
		MapSet(MapChild(top, "models"), "smart", m)
		return nil
	})
	if err == nil {
		t.Fatal("a model without privacy was written")
	}
	if raw, _ := os.ReadFile(f.Path); string(raw) != editSample {
		t.Errorf("file changed:\n%s", raw)
	}
}

func TestBE03_SecretsStayOutOfTheFileAndAreEncryptedWithAKey(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"", "correct horse"} {
		s := Secrets{Path: filepath.Join(dir, "s-"+key), Key: key}
		if err := s.Set(map[string]string{SecretName("MODEL", "smart", "TOKEN"): "sk-123"}); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(s.Path)
		if (key != "") == strings.Contains(string(raw), "sk-123") {
			t.Errorf("key %q: file %s", key, raw)
		}
		if st, _ := os.Stat(s.Path); st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", st.Mode().Perm())
		}
		lookup := s.Lookup(func(string) (string, bool) { return "old", true })
		if v, _ := lookup("MIRA_MODEL_SMART_TOKEN"); v != "sk-123" {
			t.Errorf("key %q: lookup %q", key, v)
		}
		if key != "" {
			if _, err := (Secrets{Path: s.Path, Key: "wrong"}).Load(); err != ErrSecretKey {
				t.Errorf("wrong key: %v", err)
			}
		}
	}
	if n := SecretName("SKILL", "paperless", "token"); n != "MIRA_SKILL_PAPERLESS_TOKEN" {
		t.Errorf("name %s", n)
	}
}
