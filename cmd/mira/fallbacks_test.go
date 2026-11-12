package main

import (
	"context"
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The secrets file cannot be read while a model is renamed: the model keeps
// its token under the old name, and the log names both names.
func TestFB24_a_secrets_file_that_cannot_be_read_while_editing_is_in_the_log(t *testing.T) {
	a, e, _ := editApp(t)
	log, lines := logcap.Logger()
	a.log = log
	if err := os.WriteFile(e.file.Secrets.Path, []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("token: ${MIRA_MODEL_ALT_TOKEN}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc.Content[0]
	e.moveSecret("MIRA_MODEL_ALT_TOKEN", "MIRA_MODEL_NEU_TOKEN", entry)
	if tok := entry.Content[1].Value; tok != "${MIRA_MODEL_ALT_TOKEN}" {
		t.Errorf("token now %q", tok)
	}
	if lines.Line("level=WARN", "secrets file", "from=MIRA_MODEL_ALT_TOKEN", "to=MIRA_MODEL_NEU_TOKEN", "error=") == "" {
		t.Errorf("no line for the secrets file:\n%s", lines)
	}
}

// The kept list of what Assist sees is no JSON: it counts as not sent, and
// the log says so.
func TestFB25_a_kept_list_that_cannot_be_read_is_in_the_log(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log, lines := logcap.Logger()
	a := &app{store: st, haSent: true, log: log}
	ctx := context.Background()
	if err := st.SetKV(ctx, kvExposed, `["a",`); err != nil {
		t.Fatal(err)
	}
	if _, known := a.exposed(ctx); known {
		t.Error("a broken list counts as known")
	}
	if a.exposedHash(ctx) != "" {
		t.Error("a broken list has a fingerprint")
	}
	if lines.Line("level=WARN", "what Assist sees cannot be read", "counts as not sent") == "" ||
		lines.Line("level=WARN", "what Assist sees cannot be read", "sends it again") == "" {
		t.Errorf("no lines for the broken list:\n%s", lines)
	}
}
