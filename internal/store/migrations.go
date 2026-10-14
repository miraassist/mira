package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migration is one forward-only schema step. Never edit a shipped statement;
// append a new migration instead.
type migration struct {
	name string
	stmt string
}

var migrations = []migration{
	{
		name: "0001_sessions",
		stmt: `
CREATE TABLE sessions (
    key             TEXT PRIMARY KEY,
    channel         TEXT NOT NULL,
    conversation_id TEXT NOT NULL DEFAULT '',
    device_id       TEXT NOT NULL DEFAULT '',
    area_id         TEXT NOT NULL DEFAULT '',
    language        TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL
);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL REFERENCES sessions(key) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    tool_calls  TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    UNIQUE(session_key, seq)
);
CREATE INDEX idx_messages_session ON messages(session_key, seq);
`,
	},
	{
		name: "0002_memory",
		stmt: `
CREATE TABLE memory_facts (
    id         TEXT PRIMARY KEY,
    text       TEXT NOT NULL,
    tags       TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE memory_facts_fts USING fts5(
    text, tags, content='memory_facts', content_rowid='rowid'
);
CREATE TRIGGER memory_facts_ai AFTER INSERT ON memory_facts BEGIN
    INSERT INTO memory_facts_fts(rowid, text, tags) VALUES (new.rowid, new.text, new.tags);
END;
CREATE TRIGGER memory_facts_ad AFTER DELETE ON memory_facts BEGIN
    INSERT INTO memory_facts_fts(memory_facts_fts, rowid, text, tags)
        VALUES ('delete', old.rowid, old.text, old.tags);
END;
CREATE TRIGGER memory_facts_au AFTER UPDATE ON memory_facts BEGIN
    INSERT INTO memory_facts_fts(memory_facts_fts, rowid, text, tags)
        VALUES ('delete', old.rowid, old.text, old.tags);
    INSERT INTO memory_facts_fts(rowid, text, tags) VALUES (new.rowid, new.text, new.tags);
END;

CREATE TABLE memory_profile (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    text       TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
`,
	},
	{
		name: "0003_notices",
		stmt: `
CREATE TABLE notices (
    id            TEXT PRIMARY KEY,
    source        TEXT NOT NULL,
    dedupe_key    TEXT NOT NULL DEFAULT '',
    importance    INTEGER NOT NULL DEFAULT 0,
    audience      TEXT NOT NULL DEFAULT 'household',
    spoken        TEXT NOT NULL,
    details       TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'pending',
    delivered_via TEXT NOT NULL DEFAULT '',
    delivered_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_notices_state ON notices(state, importance DESC, created_at);
CREATE UNIQUE INDEX idx_notices_open_dedupe
    ON notices(dedupe_key) WHERE dedupe_key <> '' AND state IN ('pending', 'delivered');

CREATE TABLE announcements (
    id         TEXT PRIMARY KEY,
    device_id  TEXT NOT NULL DEFAULT '',
    area_id    TEXT NOT NULL DEFAULT '',
    channel    TEXT NOT NULL,
    notice_ids TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_announcements_device ON announcements(device_id, created_at DESC);
`,
	},
	{
		name: "0004_kv",
		stmt: `
CREATE TABLE kv (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`,
	},
	{
		name: "0005_confirmations",
		stmt: `
CREATE TABLE confirmations (
    id          TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    arguments   TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL
);
CREATE INDEX idx_confirmations_session ON confirmations(session_key, created_at DESC);
`,
	},
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.name] {
			continue
		}
		err := s.withTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.stmt); err != nil {
				return fmt.Errorf("apply migration %s: %w", m.name, err)
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`,
				m.name, s.now().Format(timeFormat))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// AppliedMigrations lists the migrations recorded in the database, in order.
func (s *Store) AppliedMigrations(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
