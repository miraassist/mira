package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
)

// carryOverKV moves the lists kept as keys of kv into their tables
// (docs/spec/kv-cleanup.md, step 2, migration 0030). A value that does not
// parse stops the migration, naming its key and its start (KV-07).
func carryOverKV(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT key, value, updated_at FROM kv
WHERE key GLOB 'schedule:*' OR key GLOB 'language.*' OR key IN ('found.said', 'inbox_mails', 'inbox_owners')`)
	if err != nil {
		return err
	}
	type kvRow struct{ key, value, updated string }
	var all []kvRow
	for rows.Next() {
		var r kvRow
		if err := rows.Scan(&r.key, &r.value, &r.updated); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	notUnderstood := func(r kvRow, err error) error {
		return fmt.Errorf("kv %s: %q not understood: %w", r.key, excerpt.Of(r.value), err)
	}
	for _, r := range all {
		switch {
		case strings.HasPrefix(r.key, "schedule:"):
			k, ok := parseRunKey(r.key)
			if !ok {
				return notUnderstood(r, fmt.Errorf("not schedule:<skill>:<name>[:<person>]"))
			}
			var run LastRun
			if err := json.Unmarshal([]byte(r.value), &run); err != nil {
				return notUnderstood(r, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_runs(skill, schedule, person, started_at, ended_at, status, text, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, k.Skill, k.Schedule, k.Person, formatTime(run.Started), formatTime(run.Ended),
				run.Status, run.Text, run.Error); err != nil {
				return err
			}
		case strings.HasPrefix(r.key, "language."):
			scope, id := LanguageLast, ""
			switch {
			case r.key == "language.last":
			case strings.HasPrefix(r.key, "language.area."):
				scope, id = LanguageArea, strings.TrimPrefix(r.key, "language.area.")
			case strings.HasPrefix(r.key, "language.person."):
				scope, id = LanguagePerson, strings.TrimPrefix(r.key, "language.person.")
			default:
				return notUnderstood(r, fmt.Errorf("not language.last, language.area.<id> or language.person.<id>"))
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO languages(scope, id, language, updated_at) VALUES (?, ?, ?, ?)`,
				scope, id, r.value, r.updated); err != nil {
				return err
			}
		case r.key == "found.said":
			var said map[string]string
			if err := json.Unmarshal([]byte(r.value), &said); err != nil {
				return notUnderstood(r, err)
			}
			for key, name := range said {
				if _, err := tx.ExecContext(ctx, `INSERT INTO found_said(key, name, said_at) VALUES (?, ?, ?)`,
					key, name, r.updated); err != nil {
					return err
				}
			}
		case r.key == "inbox_mails":
			var mails map[string][]string
			if err := json.Unmarshal([]byte(r.value), &mails); err != nil {
				return notUnderstood(r, err)
			}
			for entry, hashes := range mails {
				// Oldest first: the rows keep that order (their rowid).
				for _, h := range hashes {
					if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO inbox_read(entry, hash, read_at) VALUES (?, ?, ?)`,
						entry, h, r.updated); err != nil {
						return err
					}
				}
			}
		case r.key == "inbox_owners":
			var owners map[string]string
			if err := json.Unmarshal([]byte(r.value), &owners); err != nil {
				return notUnderstood(r, err)
			}
			for owner, user := range owners {
				if _, err := tx.ExecContext(ctx, `INSERT INTO inbox_owners(owner, user) VALUES (?, ?)`, owner, user); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, r.key); err != nil {
			return err
		}
	}
	return nil
}

// parseRunKey reads a key of the old form schedule:<skill>:<name>[:<person>].
func parseRunKey(key string) (RunKey, bool) {
	parts := strings.Split(strings.TrimPrefix(key, "schedule:"), ":")
	switch {
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return RunKey{Skill: parts[0], Schedule: parts[1]}, true
	case len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "":
		return RunKey{Skill: parts[0], Schedule: parts[1], Person: parts[2]}, true
	}
	return RunKey{}, false
}
