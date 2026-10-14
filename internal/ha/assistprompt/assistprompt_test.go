package assistprompt

import (
	"context"
	"errors"
	"testing"
)

type source struct {
	text string
	err  error
}

func (s *source) Prompt(context.Context, string, string) (string, error) { return s.text, s.err }

func TestRefreshReportsOnlyRealChanges(t *testing.T) {
	src := &source{text: "Static Context: Küche\n"}
	changes := 0
	c := New(Options{Source: src, Server: "ha", OnChange: func() { changes++ }})
	ctx := context.Background()

	if c.Text() != "" {
		t.Fatal("text before the first fetch")
	}
	if changed, err := c.Refresh(ctx); err != nil || !changed {
		t.Fatalf("first refresh = %v, %v", changed, err)
	}
	if c.Text() != "Static Context: Küche" || changes != 0 {
		t.Fatalf("text = %q, changes = %d (the first fetch is no change)", c.Text(), changes)
	}
	if changed, _ := c.Refresh(ctx); changed || changes != 0 {
		t.Fatal("same text counted as a change")
	}
	src.text = "Static Context: Küche, Bad"
	if changed, _ := c.Refresh(ctx); !changed || changes != 1 {
		t.Fatalf("changed text not reported, changes = %d", changes)
	}
	src.err = errors.New("mcp down")
	if _, err := c.Refresh(ctx); err == nil {
		t.Fatal("error swallowed")
	}
	if c.Text() != "Static Context: Küche, Bad" {
		t.Error("a failed fetch must keep the last text")
	}
}
