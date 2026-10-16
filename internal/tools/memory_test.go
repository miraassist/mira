package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

func TestMemoryHasOwners(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tl := map[string]InternalTool{}
	for _, it := range Internal(i18n.DE, st, nil) {
		tl[it.Name] = it
	}
	anna := Invocation{Channel: config.ChannelTelegram, Person: "anna"}
	sat := Invocation{Channel: config.ChannelVoice, Area: "kueche"}

	// A known speaker remembers for herself, unless she says it is for the house.
	if out, err := run(t, tl[ToolMemorySave], anna, `{"text":"Anna mag keinen Koriander."}`); err != nil || !strings.Contains(out, "anna") {
		t.Fatalf("personal: %q %v", out, err)
	}
	if _, err := run(t, tl[ToolMemorySave], anna, `{"text":"Der Koriander steht im Kühlschrank.","house":true}`); err != nil {
		t.Fatal(err)
	}
	// Someone unknown at a satellite writes into the house's memory.
	if _, err := run(t, tl[ToolMemorySave], sat, `{"text":"Koriander kaufen wir beim Türken."}`); err != nil {
		t.Fatal(err)
	}
	house, _ := st.AllFacts(ctx, store.OwnerHouse)
	mine, _ := st.AllFacts(ctx, "anna")
	if len(house) != 2 || len(mine) != 1 || mine[0].Author != "anna" || house[1].Author != "area:kueche" {
		t.Fatalf("house %+v, anna %+v", house, mine)
	}

	// Anna finds her own and the house's; the satellite only the house's.
	out, _ := run(t, tl[ToolMemorySearch], anna, `{"query":"Koriander"}`)
	if !strings.Contains(out, "keinen Koriander") || !strings.Contains(out, "(persönlich)") || !strings.Contains(out, "Kühlschrank") {
		t.Errorf("anna searches: %s", out)
	}
	out, _ = run(t, tl[ToolMemorySearch], sat, `{"query":"Koriander"}`)
	if strings.Contains(out, "keinen Koriander") || !strings.Contains(out, "Kühlschrank") {
		t.Errorf("satellite searches: %s", out)
	}
	// Nobody forgets someone else's facts.
	if _, err := run(t, tl[ToolMemoryForget], sat, `{"id":"`+mine[0].ID+`"}`); err == nil {
		t.Error("the satellite forgot Anna's fact")
	}
	if _, err := run(t, tl[ToolMemoryForget], anna, `{"id":"`+mine[0].ID+`"}`); err != nil {
		t.Errorf("Anna cannot forget her own fact: %v", err)
	}
}
