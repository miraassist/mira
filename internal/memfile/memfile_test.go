package memfile

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

var (
	house = Owner{Key: store.OwnerHouse}
	anna  = Owner{Key: "anna", Name: "Anna"}
	ben   = Owner{Key: "ben", Name: "Ben"}
	all   = []Owner{house, anna, ben}
)

func open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func save(t *testing.T, st *store.Store, id, owner, text string, tags ...string) {
	t.Helper()
	if _, err := st.SaveFact(context.Background(), store.Fact{ID: id, Owner: owner, Text: text, Tags: tags, Author: "anna"}); err != nil {
		t.Fatal(err)
	}
}

func texts(t *testing.T, st *store.Store, owner string) []string {
	t.Helper()
	facts, err := st.AllFacts(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range facts {
		out = append(out, f.Text+" "+strings.Join(f.Tags, ","))
	}
	return out
}

func TestWriteAndParse(t *testing.T) {
	st := open(t)
	save(t, st, "1", store.OwnerHouse, "Der Sicherungskasten ist im Keller.", "technik", "keller")
	save(t, st, "2", "anna", "Anna ist allergisch\ngegen Birke.", "allergie")
	text, err := Write(context.Background(), st, []Owner{house, anna, ben})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Haus\n\n- Der Sicherungskasten ist im Keller. #technik #keller\n\n# Anna\n\n- Anna ist allergisch gegen Birke. #allergie\n\n# Ben\n\n"
	if text != want {
		t.Fatalf("written:\n%q\nwant\n%q", text, want)
	}
	sections, err := Parse("Notizen oben.\n" + text + "\n**fett** und ein Absatz\n* noch einer #a #b")
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 3 || sections[0].Heading != "Haus" || len(sections[2].Lines) != 1 ||
		sections[2].Lines[0].Text != "noch einer" || strings.Join(sections[2].Lines[0].Tags, ",") != "a,b" {
		t.Errorf("parsed %+v", sections)
	}
	if _, err := Parse("- vor der Überschrift\n# Haus"); !errors.As(err, new(LineError)) {
		t.Errorf("a fact before a heading: %v", err)
	}
}

// Importing is loading a file into an editor: an owner with a section has
// exactly its facts afterwards; unchanged facts keep author and time.
func TestImportReplacesTheSectionsOwners(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	save(t, st, "1", store.OwnerHouse, "Der Sicherungskasten ist im Keller.", "technik")
	save(t, st, "2", store.OwnerHouse, "Der Schornsteinfeger kommt im November.")
	save(t, st, "3", "ben", "Ben trinkt Kaffee.")
	sections, _ := Parse("# haus\n- der Sicherungskasten ist im  Keller #keller\n- Die Spülmaschine piept zweimal.\n- Die Spülmaschine piept zweimal.\n# Anna\n- Anna mag Tee.")
	changes, err := Plan(ctx, st, sections, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || len(changes[0].Add) != 1 || len(changes[0].Remove) != 1 || len(changes[0].Keep) != 1 ||
		changes[0].Remove[0].ID != "2" || len(changes[1].Add) != 1 {
		t.Fatalf("plan %+v", changes)
	}
	if got := texts(t, st, store.OwnerHouse); len(got) != 2 {
		t.Fatalf("Plan changed something: %v", got)
	}
	if err := Apply(ctx, st, changes, "stefan"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(texts(t, st, store.OwnerHouse), "|"); got != "Der Sicherungskasten ist im Keller. keller|Die Spülmaschine piept zweimal. " {
		t.Errorf("house: %s", got)
	}
	kept, _ := st.GetFact(ctx, "1")
	if kept.Author != "anna" || kept.Text != "Der Sicherungskasten ist im Keller." {
		t.Errorf("the unchanged fact lost its author or wording: %+v", kept)
	}
	if got := texts(t, st, "ben"); len(got) != 1 {
		t.Errorf("Ben had no section and must stay: %v", got)
	}
	added, _ := st.AllFacts(ctx, "anna")
	if len(added) != 1 || added[0].Author != "stefan" || added[0].Source != Source {
		t.Errorf("anna: %+v", added)
	}
	// An empty section empties that memory.
	sections, _ = Parse("# Ben\n")
	changes, _ = Plan(ctx, st, sections, all, all)
	if err := Apply(ctx, st, changes, ""); err != nil {
		t.Fatal(err)
	}
	if got := texts(t, st, "ben"); len(got) != 0 {
		t.Errorf("Ben's empty section: %v", got)
	}
}

func TestHeadingsMustBeKnownAndAllowed(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	sections, _ := Parse("# Haus\n- a\n# Oma\n- b")
	var he HeadingError
	if _, err := Plan(ctx, st, sections, all, all); !errors.As(err, &he) || !he.Unknown || he.Heading != "Oma" {
		t.Errorf("unknown heading: %v", err)
	}
	sections, _ = Parse("# House\n- a\n# ben\n- b")
	if _, err := Plan(ctx, st, sections, all, []Owner{house, anna}); !errors.As(err, &he) || he.Unknown || he.Heading != "ben" {
		t.Errorf("not allowed: %v", err)
	}
}
