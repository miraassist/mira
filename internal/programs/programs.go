// Package programs gets the programs skills bring (docs/spec/skill-programs.md):
// downloaded once per version from the address the skill names, checked
// against the skill's SHA-256 sum, kept under the config folder's programs/
// and started as the skill's MCP server. Mira never takes a newer version
// by herself: the sum in the skill is the trust.
package programs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/skills"
)

const (
	// maxSize bounds a download.
	maxSize = 100 << 20
	// timeout bounds one download.
	timeout = 2 * time.Minute
)

// Retry is how long Mira waits after a failed download before trying again.
const Retry = 10 * time.Minute

// Manager keeps the programs of the switched-on skills.
type Manager struct {
	dir    string
	client *http.Client
	log    *slog.Logger

	// retry is Retry; tests shorten it. clock is what the wait before the
	// next download waits on; tests set a fake one. A download's own time
	// limit bounds a connection and stays real time.
	retry time.Duration
	clock clock.Clock

	mu       sync.Mutex
	problems map[string]skills.Problem // by skill
	loading  map[string]bool
}

// New keeps programs in dir (<config>/programs).
func New(dir string, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{dir: dir, client: &http.Client{Timeout: timeout}, log: log.With("component", "programs"), retry: Retry, clock: clock.Real(),
		problems: map[string]skills.Problem{}, loading: map[string]bool{}}
}

// Path is where a skill's program is kept.
func (m *Manager) Path(p skills.Program) string { return filepath.Join(m.dir, p.FileName()) }

// Start makes sure every skill's program is there: one that is there with
// the right sum is used at once; the others are downloaded in the
// background, again after Retry when that fails, until ctx ends. ready is
// called with the skill's name once its program is there after a download.
// Programs no skill names any more are deleted.
func (m *Manager) Start(ctx context.Context, list []skills.Program, ready func(skill string)) {
	m.cleanup(list)
	for _, p := range list {
		if m.present(p) {
			continue
		}
		m.mu.Lock()
		m.loading[p.Skill] = true
		m.mu.Unlock()
		go func() {
			for {
				err := m.fetch(ctx, p)
				m.mu.Lock()
				if err == nil {
					delete(m.problems, p.Skill)
					delete(m.loading, p.Skill)
				}
				m.mu.Unlock()
				if err == nil {
					m.log.Info("program downloaded", "skill", p.Skill, "program", p.FileName())
					if ready != nil {
						ready(p.Skill)
					}
					return
				}
				m.log.Warn("program not downloaded", "skill", p.Skill, "url", p.URL, "error", err, "retry", m.retry)
				select {
				case <-ctx.Done():
					return
				case <-m.clock.After(m.retry):
				}
			}
		}()
	}
}

// Loading says whether a skill's program is still being got.
func (m *Manager) Loading(skill string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loading[skill]
}

// Problems are the skills whose program could not be got, as of now.
func (m *Manager) Problems() []skills.Problem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]skills.Problem, 0, len(m.problems))
	for _, p := range m.problems {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b skills.Problem) int { return compare(a.Skill, b.Skill) })
	return out
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// present: the program is there with the right sum (one with a wrong sum,
// changed on disk, is removed and got anew).
func (m *Manager) present(p skills.Program) bool {
	f, err := os.Open(m.Path(p))
	if err != nil {
		return false
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err == nil && hex.EncodeToString(h.Sum(nil)) == p.SHA256 {
		return true
	}
	m.log.Warn("program changed on disk, getting it anew", "program", m.Path(p))
	_ = os.Remove(m.Path(p))
	return false
}

// fetch downloads a program, checks its sum and puts it in place; nothing
// is written under its name unless the sum matches.
func (m *Manager) fetch(ctx context.Context, p skills.Program) error {
	fail := func(kind string, args ...any) error {
		m.mu.Lock()
		m.problems[p.Skill] = skills.NewProblem(p.Skill, kind, args...)
		m.mu.Unlock()
		return fmt.Errorf("%s %v", kind, args)
	}
	if err := allowed(p.URL); err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(skills.KindProgramDownload, p.URL, fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	tmp, err := os.CreateTemp(m.dir, "."+p.FileName()+".*")
	if err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	if n > maxSize {
		return fail(skills.KindProgramDownload, p.URL, errors.New("larger than 100 MB"))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.SHA256 {
		return fail(skills.KindProgramSum, p.URL, p.SHA256, got)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	if err := os.Rename(tmp.Name(), m.Path(p)); err != nil {
		return fail(skills.KindProgramDownload, p.URL, err)
	}
	return nil
}

// allowed: https, or http on this machine (tests).
func allowed(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")) {
		return nil
	}
	return errors.New("only https")
}

// cleanup deletes the programs no skill names (a skill's new version
// leaves the old one behind until the next start).
func (m *Manager) cleanup(keep []skills.Program) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || slices.ContainsFunc(keep, func(p skills.Program) bool { return p.FileName() == e.Name() }) {
			continue
		}
		if err := os.Remove(filepath.Join(m.dir, e.Name())); err == nil {
			m.log.Info("program removed, no skill names it", "file", e.Name())
		}
	}
}
