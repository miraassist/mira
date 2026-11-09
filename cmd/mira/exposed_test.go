package main

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
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

// registryReads is a structure source that counts its reads of the areas.
type registryReads struct{ n atomic.Int32 }

func (r *registryReads) Areas(context.Context) ([]haws.Area, error) {
	r.n.Add(1)
	return []haws.Area{{AreaID: "kueche", Name: "Küche"}}, nil
}
func (r *registryReads) Devices(context.Context) ([]haws.Device, error) { return nil, nil }
func (r *registryReads) Entities(context.Context) ([]haws.Entity, error) {
	return []haws.Entity{{EntityID: "todo.einkaufsliste", AreaID: "kueche", Name: "Einkaufsliste"}}, nil
}

func TestHU01_AListOfExposedEntitiesFromTheIntegrationRebuildsTheHouseAtOnce(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src := &registryReads{}
	a := &app{store: st}
	a.structure = structure.NewCache(structure.Options{Source: src, Exposed: a.exposed, Debounce: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.structure.Run(ctx)

	if err := a.setExposed(ctx, []string{"todo.einkaufsliste"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !a.structure.Snapshot().ExposedEntities["todo.einkaufsliste"] {
		if time.Now().After(deadline) {
			t.Fatalf("the house was not rebuilt at once (%d reads)", src.n.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := src.n.Load(); n != 1 {
		t.Errorf("the registries were read %d times, want 1", n)
	}
}
