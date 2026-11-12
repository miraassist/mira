// Package storetest gives tests a database with every migration applied,
// without applying them for every test: they are applied once per test
// process, and each test gets a copy of the result. Applying them costs about
// 0.1 s (half a second with the race detector), copying a fraction of a
// millisecond.
package storetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

var (
	once     sync.Once
	template []byte
	failed   error
)

// Path is the path of a fresh database in the test's own temporary
// directory, its migrations applied; open it with store.Open.
func Path(t testing.TB) string {
	t.Helper()
	once.Do(func() { template, failed = build() })
	if failed != nil {
		t.Fatalf("the template database: %v", failed)
	}
	path := filepath.Join(t.TempDir(), "mira.db")
	if err := os.WriteFile(path, template, 0o600); err != nil {
		t.Fatalf("copying the template database to %s: %v", path, err)
	}
	return path
}

// build applies the migrations to a database of its own and returns the
// closed file's bytes (closing folds the write-ahead log into it).
func build() ([]byte, error) {
	dir, err := os.MkdirTemp("", "mira-storetest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "mira.db")
	s, err := store.Open(context.Background(), store.Options{Path: path})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := s.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", path, err)
	}
	return os.ReadFile(path)
}
