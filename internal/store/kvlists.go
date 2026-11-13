package store

import (
	"context"
	"database/sql"
	"errors"
)

// The lists that were keys of kv before (docs/spec/kv-cleanup.md, step 2):
// the languages, the found devices said, the mails the inbox read and the
// Paperless owners it learned.

// Scopes of a remembered language (table languages).
const (
	LanguageLast   = "last"
	LanguageArea   = "area"
	LanguagePerson = "person"
)

// Language is the language last spoken in a scope: at an area, by a person,
// or at all (LanguageLast, id ""); ok is false when none was.
func (s *Store) Language(ctx context.Context, scope, id string) (lang string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT language FROM languages WHERE scope = ? AND id = ?`, scope, id).Scan(&lang)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return lang, err == nil, err
}

// SetLanguage remembers the language last spoken in a scope.
func (s *Store) SetLanguage(ctx context.Context, scope, id, lang string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO languages(scope, id, language, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(scope, id) DO UPDATE SET language = excluded.language, updated_at = excluded.updated_at`,
		scope, id, lang, formatTime(s.now()))
	return err
}

// FoundSaid are the found devices and discoveries already said, by key
// (device:<id>, flow:<handler>:<unique id>) with their names.
func (s *Store) FoundSaid(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, name FROM found_said`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, n string
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// SetFoundSaid makes the table what said is: new keys are added with now,
// keys no longer in said are deleted.
func (s *Store) SetFoundSaid(ctx context.Context, said map[string]string) error {
	now := formatTime(s.now())
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS found_keep(key TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM found_keep`); err != nil {
			return err
		}
		for k, n := range said {
			if _, err := tx.ExecContext(ctx, `INSERT INTO found_keep(key) VALUES (?)`, k); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO found_said(key, name, said_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET name = excluded.name`, k, n, now); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM found_said WHERE key NOT IN (SELECT key FROM found_keep)`)
		return err
	})
}

// InboxRead tells whether the inbox read the mail with hash from an IMAP
// entry.
func (s *Store) InboxRead(ctx context.Context, entry, hash string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM inbox_read WHERE entry = ? AND hash = ?`, entry, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// KeepInboxRead marks a mail read; of an entry only the last keep are kept.
func (s *Store) KeepInboxRead(ctx context.Context, entry, hash string, keep int) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		// INSERT OR REPLACE gives a mail read again a new rowid: it is the
		// newest then.
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO inbox_read(entry, hash, read_at) VALUES (?, ?, ?)`,
			entry, hash, formatTime(s.now())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM inbox_read WHERE entry = ? AND rowid NOT IN
(SELECT rowid FROM inbox_read WHERE entry = ? ORDER BY rowid DESC LIMIT ?)`, entry, entry, keep)
		return err
	})
}

// InboxOwner is the user name learned for a Paperless owner number.
func (s *Store) InboxOwner(ctx context.Context, owner string) (user string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT user FROM inbox_owners WHERE owner = ?`, owner).Scan(&user)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return user, err == nil, err
}

// SetInboxOwner keeps the user name of a Paperless owner number.
func (s *Store) SetInboxOwner(ctx context.Context, owner, user string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO inbox_owners(owner, user) VALUES (?, ?)
ON CONFLICT(owner) DO UPDATE SET user = excluded.user`, owner, user)
	return err
}
