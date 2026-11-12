package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// docs/spec/inbox.md, IN-03: without the skill paperless's inbox_token the
// inbox takes no webhooks of Paperless, and the start says so.
func TestIN03_InboxOffIsLogged(t *testing.T) {
	var log bytes.Buffer
	a := &app{log: slog.New(slog.NewTextHandler(&log, nil))}
	cfg := config.Default()
	cfg.Skills.Enabled = map[string]map[string]any{"paperless": {"url": "http://mcp", "token": "t"}}
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"postfach": nil})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	in := a.newInbox(cfg, loaded)
	if in == nil || in.PaperlessOn() {
		t.Fatal("the mail inbox should be on, its Paperless part off")
	}
	if strings.Count(log.String(), "has no inbox_token") != 1 {
		t.Fatalf("log:\n%s", log.String())
	}
}

// docs/spec/inbox.md, IN-06: the owner's Paperless user name is the person by
// the convention of the calendars: login, name or key.
func TestIN06_OwnerBecomesPersonByConvention(t *testing.T) {
	persons := map[string]config.Person{
		"anna": {Entity: "person.anna", Name: "Anna Muster"},
		"ben":  {Entity: "person.ben", Name: "Ben"},
	}
	logins := map[string]string{"person.anna": "anna.m"}
	for owner, want := range map[string]string{"anna.m": "anna", "Anna Muster": "anna", "BEN": "ben", "anna": "anna"} {
		if got, ok := personByOwner(owner, persons, logins); !ok || got != want {
			t.Errorf("%q: %q %v", owner, got, ok)
		}
	}
	if _, ok := personByOwner("scanner", persons, logins); ok {
		t.Error("an unknown owner became a person")
	}
}

// docs/spec/inbox.md, IN-38: Mira keeps the hashes of the last 100 mails
// read per IMAP entry, in her database, across a restart.
func TestIN38_TheLast100MailsPerEntryAreKept(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mira.db")
	st, err := store.Open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{store: st}
	for i := range 101 {
		if err := a.inboxKeep(ctx, "E1", fmt.Sprintf("h%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.inboxKeep(ctx, "E2", "h000"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = store.Open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a = &app{store: st}
	for _, c := range []struct {
		entry, hash string
		want        bool
	}{{"E1", "h000", false}, {"E1", "h001", true}, {"E1", "h100", true}, {"E2", "h000", true}, {"E2", "h001", false}} {
		got, err := a.inboxKnown(ctx, c.entry, c.hash)
		if err != nil || got != c.want {
			t.Errorf("known(%s, %s) = %v, %v; want %v", c.entry, c.hash, got, err, c.want)
		}
	}
}
