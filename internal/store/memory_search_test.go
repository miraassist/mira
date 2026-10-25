package store

import (
	"context"
	"testing"
)

// German questions find the facts they mean: inflected words ("Kalenders",
// "Pilze"), a word inside a compound ("Kalender" in "Standardkalender"),
// umlauts written without them, and a fact that has none of the words is
// not found (docs/todo C6).
func TestMemorySearchFindsGermanForms(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	facts := map[string]string{
		"kal":    "Der Standard-Kalender des Hauses ist Familie.",
		"comp":   "Bens Standardkalender heißt Training.",
		"pilz":   "Anna mag keinen Pilz.",
		"keller": "Der Wechselrichter steht im Keller.",
		"schorn": "Der Schornsteinfeger kommt im März.",
		"fuss":   "Ben ist Fan vom Karlsruher SC.",
	}
	for id, text := range facts {
		if _, err := s.SaveFact(ctx, Fact{ID: id, Text: text, Owner: OwnerHouse}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query string
		want  string
	}{
		{"Kalender", "kal"},
		{"Kalenders", "kal"},
		{"Kalender", "comp"},
		{"Pilze", "pilz"},
		{"Schornsteinfegers", "schorn"},
		{"Maerz", ""},
		{"März", "schorn"},
		{"Wechselrichters", "keller"},
		{"Karlsruhe", "fuss"},
	}
	hits := 0
	for _, c := range cases {
		got, err := s.SearchFacts(ctx, c.query, nil, 10)
		if err != nil {
			t.Fatalf("%s: %v", c.query, err)
		}
		found := c.want == ""
		for _, f := range got {
			if f.ID == c.want {
				found = true
			}
		}
		if found {
			hits++
		} else {
			t.Errorf("%q does not find %q", c.query, facts[c.want])
		}
	}
	t.Logf("%d of %d found", hits, len(cases))
	// a question with none of the words finds nothing
	if got, _ := s.SearchFacts(ctx, "Waschmaschine", nil, 10); len(got) != 0 {
		t.Errorf("Waschmaschine found %v", got)
	}
}
