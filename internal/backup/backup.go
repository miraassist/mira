// Package backup makes Mira's snapshot for Home Assistant's backups and
// restores one (docs/spec/backup.md): the database as one consistent copy,
// mira.yaml, the secrets decrypted, and one's own skill folders, in one
// tar.gz with a manifest.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

// The files of the archive.
const (
	manifestFile = "manifest.json"
	dbFile       = "mira.db"
	configFile   = "mira.yaml"
	secretsFile  = "secrets.json"
	skillsDir    = "skills"
	// RestoredConfig is the backup's mira.yaml beside the data when the
	// live one cannot be written.
	RestoredConfig = "mira.yaml.restored"
	// setupPrefix are the keys of this Mira's setup and pairing: a restore
	// keeps them, or the integration would be cut off.
	setupPrefix = "setup."
	// maxArchive bounds what a restore reads.
	maxArchive = 1 << 30
)

// Manifest says what the archive holds.
type Manifest struct {
	Version string    `json:"version"`
	Created time.Time `json:"created"`
	// Migration is the last migration of the database.
	Migration string `json:"migration"`
	// Skills are one's own skills, by name: the folder they came from.
	Skills map[string]string `json:"skills,omitempty"`
}

// Mira is what a snapshot is made of and a restore goes to.
type Mira struct {
	Store      *store.Store
	DBPath     string
	ConfigPath string // "" when Mira has none
	Secrets    config.Secrets
	// SkillDirs are the folders of one's own skills (skills.dirs without
	// Mira's bundled one).
	SkillDirs []string
	// DataDir takes the temporary copy and mira.yaml.restored.
	DataDir string
	Version string
}

// Make writes the snapshot to w.
func Make(ctx context.Context, m Mira, w io.Writer) error {
	tmp, err := os.MkdirTemp(m.DataDir, "backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	db := filepath.Join(tmp, dbFile)
	if err := m.Store.VacuumInto(ctx, db); err != nil {
		return fmt.Errorf("copy the database: %w", err)
	}
	migrations := store.Migrations()
	man := Manifest{Version: m.Version, Created: time.Now().UTC(), Migration: migrations[len(migrations)-1],
		Skills: map[string]string{}}
	secrets, err := m.Secrets.Load()
	if err != nil {
		return fmt.Errorf("read the secrets: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: man.Created}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	if err := addFile(tw, dbFile, db); err != nil {
		return err
	}
	if m.ConfigPath != "" {
		raw, err := os.ReadFile(m.ConfigPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read mira.yaml: %w", err)
		}
		if err == nil {
			if err := add(configFile, raw); err != nil {
				return err
			}
		}
	}
	raw, _ := json.MarshalIndent(secrets, "", "  ")
	if err := add(secretsFile, raw); err != nil {
		return err
	}
	for _, dir := range m.SkillDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a folder that is not there has no skills
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || man.Skills[name] != "" {
				continue // the first folder wins, as when Mira loads them
			}
			if _, err := os.Stat(filepath.Join(dir, name, "SKILL.md")); err != nil {
				continue
			}
			man.Skills[name] = dir
			if err := addTree(tw, path.Join(skillsDir, name), filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	raw, _ = json.MarshalIndent(man, "", "  ")
	if err := add(manifestFile, raw); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// addTree adds the regular files under src as prefix/….
func addTree(tw *tar.Writer, prefix, src string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		return addFile(tw, path.Join(prefix, filepath.ToSlash(rel)), p)
	})
}

// Result is what a restore did.
type Result struct {
	// Config: "written", "read_only" (left as it was, the backup's copy is
	// mira.yaml.restored), or "none" (the snapshot had none).
	Config  string   `json:"config"`
	Skills  []string `json:"skills,omitempty"`
	Created string   `json:"created"`
	Version string   `json:"version"`
}

// Refused is a snapshot that is not restored, with the reason; nothing was
// changed.
type Refused struct{ Reason string }

func (r Refused) Error() string { return r.Reason }

// Refused marks it for the API (a 400, not a 500).
func (r Refused) Refused() bool { return true }

// Restore checks the snapshot in r and, if it is good, puts it in place;
// the database counts once Mira opens it next (at her restart). A Refused
// error changed nothing.
func Restore(ctx context.Context, m Mira, r io.Reader) (Result, error) {
	tmp, err := os.MkdirTemp(m.DataDir, "restore-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmp)
	if err := unpack(io.LimitReader(r, maxArchive), tmp); err != nil {
		return Result{}, Refused{"the archive cannot be read: " + err.Error()}
	}
	var man Manifest
	raw, err := os.ReadFile(filepath.Join(tmp, manifestFile))
	if err != nil || json.Unmarshal(raw, &man) != nil {
		return Result{}, Refused{"the archive has no manifest: it is not a snapshot of Mira"}
	}
	db := filepath.Join(tmp, dbFile)
	if err := checkDB(ctx, db, m.Store); err != nil {
		return Result{}, err
	}
	var secrets map[string]string
	if raw, err := os.ReadFile(filepath.Join(tmp, secretsFile)); err == nil {
		if err := json.Unmarshal(raw, &secrets); err != nil {
			return Result{}, Refused{"the secrets in the archive cannot be read"}
		}
	}
	// Checked: now it is written.
	res := Result{Created: man.Created.Format(time.RFC3339), Version: man.Version, Config: "none"}
	if err := copyFile(db, m.DBPath+store.RestoreSuffix); err != nil {
		return Result{}, err
	}
	if secrets != nil {
		if err := m.Secrets.Replace(secrets); err != nil {
			return Result{}, fmt.Errorf("write the secrets: %w", err)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(tmp, configFile)); err == nil {
		switch {
		case m.ConfigPath != "" && (config.File{Path: m.ConfigPath}).Writable() == nil:
			if err := config.WriteAtomic(m.ConfigPath, raw, 0o600); err != nil {
				return Result{}, fmt.Errorf("write mira.yaml: %w", err)
			}
			res.Config = "written"
		default:
			if err := config.WriteAtomic(filepath.Join(m.DataDir, RestoredConfig), raw, 0o600); err != nil {
				return Result{}, fmt.Errorf("write %s: %w", RestoredConfig, err)
			}
			res.Config = "read_only"
		}
	}
	for name, from := range man.Skills {
		src := filepath.Join(tmp, skillsDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dir := from
		if !slices.Contains(m.SkillDirs, from) {
			if len(m.SkillDirs) == 0 {
				continue // this Mira has no folder for one's own skills
			}
			dir = m.SkillDirs[0]
		}
		dst := filepath.Join(dir, name)
		if err := os.RemoveAll(dst); err != nil {
			return Result{}, err
		}
		if err := copyTree(src, dst); err != nil {
			return Result{}, fmt.Errorf("write the skill %s: %w", name, err)
		}
		res.Skills = append(res.Skills, name)
	}
	slices.Sort(res.Skills)
	return res, nil
}

// checkDB opens the snapshot's database: it must pass SQLite's check, have
// only migrations this Mira knows, and gets this Mira's setup keys.
func checkDB(ctx context.Context, path string, live *store.Store) error {
	if _, err := os.Stat(path); err != nil {
		return Refused{"the archive has no database"}
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return Refused{"the database in the archive does not open: " + err.Error()}
	}
	defer db.Close()
	var ok string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&ok); err != nil || ok != "ok" {
		return Refused{"the database in the archive is damaged"}
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return Refused{"the database in the archive is not Mira's"}
	}
	known := store.Migrations()
	var unknown []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && !slices.Contains(known, name) {
			unknown = append(unknown, name)
		}
	}
	rows.Close()
	if len(unknown) > 0 {
		return Refused{"the snapshot is of a newer Mira: update Mira first, then restore it"}
	}
	setup, err := live.KVWithPrefix(ctx, setupPrefix)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM kv WHERE key >= ? AND key < ?`, setupPrefix, setupPrefix+"\xff"); err != nil {
		return err
	}
	for k, v := range setup {
		if _, err := db.ExecContext(ctx, `INSERT INTO kv(key, value, updated_at) VALUES (?, ?, ?)`,
			k, v, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}

// unpack writes the archive's regular files under dir; a name that leaves
// dir is refused.
func unpack(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(h.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("a file outside the archive: %s", h.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return config.WriteAtomic(dst, raw, 0o600)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
}
