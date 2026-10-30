package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

// The integration computes the same fingerprint (tests/ha/test_reconcile.py
// has this value): sha256 of the sorted ids joined by newlines.
func TestExposedHashMatchesTheIntegration(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &app{store: st, haSent: true}
	ctx := context.Background()
	if got := a.exposedHash(ctx); got != "" {
		t.Errorf("before any list: %q", got)
	}
	if err := st.SetKV(ctx, kvExposed, `["b","a"]`); err != nil {
		t.Fatal(err)
	}
	if got, want := a.exposedHash(ctx), "7e18f737311b2dc3b2f269dd78396b0351f14fb66efa879f768cb23181883c78"; got != want {
		t.Errorf("hash = %s, want %s", got, want)
	}
}
