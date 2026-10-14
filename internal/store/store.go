// Package store is the agent's SQLite persistence: sessions and their
// transcripts, long-term memory, the notice queue and a small key/value area.
//
// One file, migrations in code. Every write goes through this package so the
// schema stays in one place.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo
)

// ErrNotFound is returned by the Get-style methods when a row is absent.
var ErrNotFound = errors.New("not found")

// Clock lets tests drive time instead of waiting for it.
type Clock func() time.Time

type Store struct {
	db    *sql.DB
	clock Clock
}

type Options struct {
	// Path is the database file. ":memory:" is accepted for tests.
	Path  string
	Clock Clock
}

// Open connects to the database and brings the schema up to date.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	dsn := opts.Path
	if dsn == ":memory:" {
		// A shared cache keeps the in-memory DB alive across pooled connections.
		dsn = "file::memory:?cache=shared"
	} else {
		dsn = "file:" + dsn + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite tolerates one writer. Serialising here is simpler and faster than
	// retrying SQLITE_BUSY all over the agent.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	s := &Store{db: db, clock: opts.Clock}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests and for the rare ad-hoc query.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) now() time.Time { return s.clock().UTC().Truncate(time.Millisecond) }

// withTx runs fn in a transaction and rolls back on error.
func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
