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
	{
		name: "0006_notice_confidentiality",
		stmt: `
ALTER TABLE notices ADD COLUMN confidentiality TEXT NOT NULL DEFAULT 'normal';
`,
	},
	{
		name: "0007_reminders",
		stmt: `
CREATE TABLE reminders (
	id          TEXT PRIMARY KEY,
	due_at      TEXT NOT NULL,
	text        TEXT NOT NULL,
	audience    TEXT NOT NULL DEFAULT 'household',
	area_id     TEXT NOT NULL DEFAULT '',
	session_key TEXT NOT NULL DEFAULT '',
	state       TEXT NOT NULL DEFAULT 'pending',
	attempts    INTEGER NOT NULL DEFAULT 0,
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);
CREATE INDEX reminders_due ON reminders(state, due_at);
`,
	},
	{
		name: "0008_tasks",
		stmt: `
CREATE TABLE tasks (
	id          TEXT PRIMARY KEY,
	goal        TEXT NOT NULL,
	title       TEXT NOT NULL,
	summary     TEXT NOT NULL DEFAULT '',
	plan        TEXT NOT NULL,
	state       TEXT NOT NULL DEFAULT 'proposed',
	session_key TEXT NOT NULL DEFAULT '',
	last_alert  TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);
`,
	},
	{
		name: "0009_task_origin",
		stmt: `ALTER TABLE tasks ADD COLUMN origin TEXT NOT NULL DEFAULT '';`,
	},
	{
		// Tasks are Home Assistant automations now: the old plans go (test
		// data only), and every judgement Mira makes for them is logged.
		name: "0010_assessments",
		stmt: `
DELETE FROM tasks;
CREATE TABLE assessments (
	id          TEXT PRIMARY KEY,
	task        TEXT NOT NULL,
	kind        TEXT NOT NULL,
	goal        TEXT NOT NULL,
	input       TEXT NOT NULL DEFAULT '',
	importance  INTEGER NOT NULL DEFAULT 0,
	answer      INTEGER NOT NULL DEFAULT 0,
	spoken      TEXT NOT NULL DEFAULT '',
	notice_id   TEXT NOT NULL DEFAULT '',
	dry_run     INTEGER NOT NULL DEFAULT 0,
	created_at  TEXT NOT NULL
);
CREATE INDEX assessments_task ON assessments(task, created_at);
`,
	},
	{
		// Threads ("Vorgänge") hold a concern across several turns (task
		// planning with questions and a yes); turns and notices point to
		// theirs. Task proposals now live in their thread.
		name: "0011_threads",
		stmt: `
CREATE TABLE threads (
	id          TEXT PRIMARY KEY,
	target      TEXT NOT NULL,
	place       TEXT NOT NULL,
	state       TEXT NOT NULL,
	request     TEXT NOT NULL DEFAULT '',
	fields      TEXT NOT NULL DEFAULT '{}',
	log         TEXT NOT NULL DEFAULT '[]',
	content     TEXT NOT NULL DEFAULT '{}',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	expires_at  TEXT NOT NULL
);
CREATE INDEX threads_place ON threads(place, state);
ALTER TABLE sessions ADD COLUMN thread_id TEXT NOT NULL DEFAULT '';
ALTER TABLE notices ADD COLUMN thread_id TEXT NOT NULL DEFAULT '';
DROP TABLE tasks;
`,
	},
	{
		// Memory has an owner: the house or a person. Who wrote a fact is
		// its author. There is one profile per owner; threads know whose
		// they are.
		name: "0012_memory_owner",
		stmt: `
ALTER TABLE memory_facts ADD COLUMN owner TEXT NOT NULL DEFAULT 'house';
ALTER TABLE memory_facts ADD COLUMN author TEXT NOT NULL DEFAULT '';
CREATE INDEX memory_facts_owner ON memory_facts(owner);
CREATE TABLE memory_profiles (
    owner      TEXT PRIMARY KEY,
    text       TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
INSERT INTO memory_profiles(owner, text, updated_at) SELECT 'house', text, updated_at FROM memory_profile;
DROP TABLE memory_profile;
ALTER TABLE threads ADD COLUMN person TEXT NOT NULL DEFAULT '';
`,
	},
	{
		// Notices become messages (docs/spec/nachrichten.md): a sender and a
		// receiver ("house" or a person). The session transcripts, which
		// had the name, are chat messages now.
		name: "0013_messages",
		stmt: `
ALTER TABLE messages RENAME TO chat_messages;
ALTER TABLE notices RENAME TO messages;
ALTER TABLE messages RENAME COLUMN audience TO receiver;
ALTER TABLE messages ADD COLUMN sender TEXT NOT NULL DEFAULT '';
UPDATE messages SET receiver = 'house' WHERE receiver = 'household';
ALTER TABLE announcements RENAME COLUMN notice_ids TO message_ids;
ALTER TABLE assessments RENAME COLUMN notice_id TO message_id;
ALTER TABLE reminders RENAME COLUMN audience TO receiver;
UPDATE reminders SET receiver = 'house' WHERE receiver = 'household';
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
