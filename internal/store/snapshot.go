package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Backup and restore (docs/spec/backup.md): a consistent copy of the
// database, and a restored one put in place when the store opens next.

// RestoreSuffix marks a restored database beside the live one: Open puts it
// in place, so an open database is never replaced.
const RestoreSuffix = ".restore"

// VacuumInto writes a consistent copy of the database to path (which must
// not exist yet); writing goes on meanwhile.
func (s *Store) VacuumInto(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// Migrations are the names of the migrations this Mira knows, in order.
func Migrations() []string {
	out := make([]string, 0, len(migrations))
	for _, m := range migrations {
		out = append(out, m.name)
	}
	return out
}

// KVWithPrefix are the key-value rows whose key begins with prefix.
func (s *Store) KVWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM kv WHERE key >= ? AND key < ?`, prefix, prefix+"\xff")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// takeRestored puts a restored database (path + RestoreSuffix) in place of
// path, without the old one's -wal and -shm.
func takeRestored(path string) error {
	restored := path + RestoreSuffix
	if _, err := os.Stat(restored); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	for _, f := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restore database: %w", err)
		}
	}
	if err := os.Rename(restored, path); err != nil {
		return fmt.Errorf("restore database: %w", err)
	}
	return nil
}

// isFile says whether the store's path is a file (not :memory:).
func isFile(path string) bool { return path != ":memory:" && !strings.HasPrefix(path, "file::memory:") }
