// Package skillsource loads the skills of a subscription from a skill source
// at Mira's start (docs/spec/skill-source.md, B): one request, its retries
// and errors, and the skills as one more skill directory that lives in
// memory. The interface is the server's (mira-skills: REQUIREMENTS.md,
// Interface to Mira). Nothing of it is written to disk (decision 8).
package skillsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"testing/fstest"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
)

// Policy is the spec's retry (Errors): three tries, waiting 2 s, then 4 s; a
// Retry-After is kept up to 30 s, a longer one ends at once.
var Policy = retry.Policy{Attempts: 3, First: 2 * time.Second, Max: 30 * time.Second, Growth: retry.Exponential}

// Timeout bounds one try.
const Timeout = 10 * time.Second

// maxAnswer bounds the answer read: a subscription's skills are tens of
// kilobytes.
const maxAnswer = 16 << 20

type Options struct {
	Source  config.SkillSource
	Version string
	Client  *http.Client
	Logger  *slog.Logger
	// Policy replaces the package's (tests: no waits).
	Policy *retry.Policy
}

// Load asks the source once (with its retries) and gives its skills as a
// root after the skill directories; nil when there are none to load. The
// problems are the source's (a repair each) and those of single skills of
// the answer; each is logged here.
func Load(ctx context.Context, o Options) (*skills.Root, []skills.Problem) {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "skillsource")
	addr := strings.TrimRight(o.Source.URL, "/")
	name := host(addr)
	if !allowed(addr) {
		log.Error("the skill source's address is not https and not on this machine; it is not asked", "url", addr)
		return nil, []skills.Problem{skills.NewProblem(name, skills.KindSourceURL, addr)}
	}
	p := Policy
	if o.Policy != nil {
		p = *o.Policy
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	var raw []byte
	err := retry.Do(ctx, p, func(ctx context.Context) error {
		var err error
		raw, err = fetch(ctx, client, addr, o.Source.Key, o.Version)
		return err
	}, retry.Warn(log, "loading the skills of the subscription", p, "url", addr))
	if err != nil {
		var st *statusError
		switch {
		case errors.As(err, &st) && (st.code == http.StatusUnauthorized || st.code == http.StatusForbidden):
			log.Error("the skill source refused the subscription key; mira starts without its skills", "url", addr, "status", st.code)
			return nil, []skills.Problem{skills.NewProblem(name, skills.KindSourceKey, addr, st.code)}
		case errors.As(err, &st) && !retry.Retryable(st):
			log.Error("the address is not a skill source; mira starts without its skills", "url", addr, "status", st.code, "answer", st.answer)
			return nil, []skills.Problem{skills.NewProblem(name, skills.KindSourceNot, addr, st.code)}
		}
		log.Error("could not load the skills of the subscription; mira starts without them and tries again at the next start", "url", addr, "error", err)
		return nil, []skills.Problem{skills.NewProblem(name, skills.KindSourceDown, addr, err)}
	}
	var answer struct {
		Skills []struct {
			Name    string            `json:"name"`
			Version string            `json:"version"`
			Files   map[string]string `json:"files"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		log.Error("the skill source's answer is not the expected JSON; mira starts without its skills", "url", addr, "error", err, "answer", excerpt.Bytes(raw))
		return nil, []skills.Problem{skills.NewProblem(name, skills.KindSourceNot, addr, "the answer is not the expected JSON")}
	}
	mem := fstest.MapFS{}
	var problems []skills.Problem
	loaded := 0
	for _, sk := range answer.Skills {
		if reason := check(sk.Name, sk.Files); reason != "" {
			log.Warn("a skill of the source is left out", "url", addr, "skill", sk.Name, "reason", reason)
			problems = append(problems, skills.NewProblem(orName(sk.Name, name), skills.KindSourceSkill, addr, sk.Name, reason))
			continue
		}
		for p, text := range sk.Files {
			mem[path.Join(sk.Name, p)] = &fstest.MapFile{Data: []byte(text), Mode: 0o444}
		}
		loaded++
		log.Info("skill loaded from the source", "url", addr, "skill", sk.Name, "version", sk.Version, "files", len(sk.Files))
	}
	log.Info("skills of the subscription loaded", "url", addr, "skills", loaded)
	return &skills.Root{Name: addr, FS: mem, Source: true}, problems
}

// check is why a skill of the answer is not taken; "" when it is.
func check(name string, files map[string]string) string {
	switch {
	case name == "" || !fs.ValidPath(name) || strings.Contains(name, "/"):
		return fmt.Sprintf("its name %q is not a folder's name", name)
	case files["SKILL.md"] == "":
		return "it has no SKILL.md"
	}
	for p := range files {
		if !fs.ValidPath(p) || p == "." {
			return fmt.Sprintf("the path %q leaves the skill's folder", p)
		}
	}
	return ""
}

func orName(skill, source string) string {
	if skill != "" && fs.ValidPath(skill) && !strings.Contains(skill, "/") {
		return skill
	}
	return source
}

// statusError is an answer other than 200: 429 and 5xx are tried again
// (a Retry-After kept), anything else is fatal.
type statusError struct {
	code   int
	answer string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("answered %d %s", e.code, e.answer)
}

func (e *statusError) Retryable() bool {
	return e.code == http.StatusTooManyRequests || e.code >= 500
}

func fetch(ctx context.Context, client *http.Client, addr, key, version string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/v1/skills", nil)
	if err != nil {
		return nil, retry.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Mira-Version", version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err // a lost connection or a timeout: tried again
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	se := &statusError{code: resp.StatusCode, answer: excerpt.Bytes(body)}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 && se.Retryable() {
		return nil, retry.After(se, time.Duration(secs)*time.Second)
	}
	return nil, se
}

// allowed: https, or http to this machine itself (decision 14).
func allowed(addr string) bool {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// host is what a problem of the whole source is named by.
func host(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		return u.Host
	}
	return addr
}
