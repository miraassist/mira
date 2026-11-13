package store

import (
	"context"
	"strings"
	"testing"
)

// docs/spec/kv-cleanup.md: the dead keys go, the lists of kv move to their
// tables.

// beforeCleanup is a database as it was before the cleanup (migration 0028)
// with these keys in kv; opening it runs the cleanup's migrations.
func beforeCleanup(t *testing.T, kv map[string]string) string {
	t.Helper()
	ctx := context.Background()
	path := templatePath(t)
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP TABLE schedule_runs`, `DROP TABLE languages`, `DROP TABLE found_said`, `DROP TABLE inbox_read`,
		`DROP TABLE inbox_owners`,
		`DELETE FROM schema_migrations WHERE name IN ('0029_kv_dead_keys', '0030_kv_tables')`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	for k, v := range kv {
		if err := s.SetKV(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	return path
}

func openAfter(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func keys(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT key FROM kv ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		out = append(out, k)
	}
	return out
}

func TestKV01_DeadKeysAreDeleted(t *testing.T) {
	s := openAfter(t, beforeCleanup(t, map[string]string{
		"ui_config:persona": "Du duzt alle.", "ui_config:persons": "anna: {}", "ui_managed_by_ha": "1",
		"telegram.offset": "4711", "feed_seen:modelle": "[]", "instance_id": "01abc", "quiet_mode": "true",
	}))
	got := strings.Join(keys(t, s), " ")
	if got != "instance_id quiet_mode" {
		t.Fatalf("kv = %s", got)
	}
}

func TestKV03_TheSchedulesLastRunsMoveToTheirTable(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, beforeCleanup(t, map[string]string{
		"schedule:briefing:morgen":           `{"started":"2026-11-13T05:30:00.001+01:00","ended":"2026-11-13T05:30:06.05+01:00","status":"done","text":"Guten Morgen"}`,
		"schedule:briefing:persoenlich:anna": `{"started":"2026-11-13T05:35:01+01:00","ended":"2026-11-13T05:35:06+01:00","status":"nothing"}`,
		"schedule:mira:profile":              `{"started":"2026-11-13T03:00:00Z","ended":"2026-11-13T03:00:09Z","status":"failed","error":"model down"}`,
	}))
	run, ok, err := s.LastRun(ctx, RunKey{Skill: "briefing", Schedule: "morgen"})
	if err != nil || !ok || run.Status != RunDone || run.Text != "Guten Morgen" || run.Ended.Sub(run.Started).Seconds() < 6 {
		t.Fatalf("house's run = %+v %v %v", run, ok, err)
	}
	runs, err := s.LastRuns(ctx, "briefing", "persoenlich")
	if err != nil || len(runs) != 1 || runs["anna"].Status != RunNothing {
		t.Fatalf("anna's runs = %+v %v", runs, err)
	}
	if run, ok, _ := s.LastRun(ctx, RunKey{Skill: "mira", Schedule: "profile"}); !ok || run.Error != "model down" {
		t.Fatalf("mira's run = %+v", run)
	}
	if k := keys(t, s); len(k) != 0 {
		t.Fatalf("kv = %v", k)
	}
}

func TestKV03_APersonGoneLosesHerLastRuns(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, templatePath(t))
	for _, p := range []string{"anna", "ben"} {
		if err := s.SetLastRun(ctx, RunKey{Skill: "briefing", Schedule: "persoenlich", Person: p}, LastRun{Status: RunDone}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetLastRun(ctx, RunKey{Skill: "briefing", Schedule: "morgen"}, LastRun{Status: RunDone}); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneLastRuns(ctx, "briefing", "persoenlich", []string{"anna"})
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if runs, _ := s.LastRuns(ctx, "briefing", "persoenlich"); len(runs) != 1 || runs["anna"].Status != RunDone {
		t.Fatalf("runs = %+v", runs)
	}
	if _, ok, _ := s.LastRun(ctx, RunKey{Skill: "briefing", Schedule: "morgen"}); !ok {
		t.Fatal("the house's run went")
	}
}

func TestKV04_AMailReadStaysRead(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, beforeCleanup(t, map[string]string{
		"inbox_mails":  `{"01ENTRY":["aaa","bbb"]}`,
		"inbox_owners": `{"3":"anna"}`,
	}))
	for _, h := range []string{"aaa", "bbb"} {
		if read, err := s.InboxRead(ctx, "01ENTRY", h); err != nil || !read {
			t.Fatalf("%s read = %v %v", h, read, err)
		}
	}
	if read, _ := s.InboxRead(ctx, "01ENTRY", "ccc"); read {
		t.Fatal("a new mail counts as read")
	}
	if user, ok, _ := s.InboxOwner(ctx, "3"); !ok || user != "anna" {
		t.Fatalf("owner = %q %v", user, ok)
	}
	if k := keys(t, s); len(k) != 0 {
		t.Fatalf("kv = %v", k)
	}
}

func TestKV04_OnlyTheLastMailsOfAnEntryAreKept(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, templatePath(t))
	for _, h := range []string{"a", "b", "c", "a"} { // a again: the newest then
		if err := s.KeepInboxRead(ctx, "e", h, 2); err != nil {
			t.Fatal(err)
		}
	}
	for h, want := range map[string]bool{"a": true, "c": true, "b": false} {
		if read, _ := s.InboxRead(ctx, "e", h); read != want {
			t.Errorf("%s read = %v", h, read)
		}
	}
}

func TestKV05_ADeviceSaidStaysSaid(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, beforeCleanup(t, map[string]string{
		"found.said":    `{"device:d1":"Hue Lampe","flow:hue:u-f1":"Hue Bridge"}`,
		"found.started": "2026-11-10T00:06:46Z",
	}))
	said, err := s.FoundSaid(ctx)
	if err != nil || len(said) != 2 || said["device:d1"] != "Hue Lampe" || said["flow:hue:u-f1"] != "Hue Bridge" {
		t.Fatalf("said = %v %v", said, err)
	}
	if got := strings.Join(keys(t, s), " "); got != "found.started" {
		t.Fatalf("kv = %s", got)
	}
	delete(said, "flow:hue:u-f1")
	said["device:d2"] = "Shelly"
	if err := s.SetFoundSaid(ctx, said); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.FoundSaid(ctx); len(again) != 2 || again["device:d2"] != "Shelly" || again["flow:hue:u-f1"] != "" {
		t.Fatalf("said = %v", again)
	}
}

func TestKV06_TheLanguagesMoveToTheirTable(t *testing.T) {
	ctx := context.Background()
	s := openAfter(t, beforeCleanup(t, map[string]string{
		"language.area.kuche": "de", "language.person.anna": "en", "language.last": "en",
	}))
	for _, c := range []struct{ scope, id, want string }{
		{LanguageArea, "kuche", "de"}, {LanguagePerson, "anna", "en"}, {LanguageLast, "", "en"},
	} {
		if got, ok, err := s.Language(ctx, c.scope, c.id); err != nil || !ok || got != c.want {
			t.Errorf("%s %s = %q %v %v", c.scope, c.id, got, ok, err)
		}
	}
	if k := keys(t, s); len(k) != 0 {
		t.Fatalf("kv = %v", k)
	}
}

func TestKV07_AValueThatDoesNotParseStopsTheMigration(t *testing.T) {
	path := beforeCleanup(t, map[string]string{"found.said": "device:d1=Hue Lampe"})
	s, err := Open(context.Background(), Options{Path: path})
	if err == nil {
		s.Close()
		t.Fatal("the store opened")
	}
	for _, want := range []string{"0030_kv_tables", "found.said", "device:d1=Hue Lampe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
