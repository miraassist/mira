// Package logcap catches what a test's code logs, to check that an error
// handler leaves a trace (docs/spec/fallbacks.md, decision 4).
package logcap

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// Lines is what was logged, safe to read while the code still logs.
type Lines struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	waiters []waiter
}

type waiter struct {
	n     int
	parts []string
	done  chan struct{}
}

func (l *Lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.buf.Write(p)
	kept := l.waiters[:0]
	for _, w := range l.waiters {
		if linesWith(l.buf.String(), w.parts) >= w.n {
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	l.waiters = kept
	return n, err
}

// Await is closed once a line holding every part was logged, at once when
// one already was: the signal a test waits for instead of polling the log
// (docs/spec/tests.md).
func (l *Lines) Await(parts ...string) <-chan struct{} { return l.AwaitN(1, parts...) }

// AwaitN is Await for n lines holding every part.
func (l *Lines) AwaitN(n int, parts ...string) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	done := make(chan struct{})
	if linesWith(l.buf.String(), parts) >= n {
		close(done)
		return done
	}
	l.waiters = append(l.waiters, waiter{n: n, parts: parts, done: done})
	return done
}

// String is all lines so far.
func (l *Lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// Line is the first line holding every part; "" without one.
func (l *Lines) Line(parts ...string) string { return lineWith(l.String(), parts) }

// linesWith counts the lines holding every part.
func linesWith(text string, parts []string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		all := line != ""
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all {
			n++
		}
	}
	return n
}

func lineWith(text string, parts []string) string {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all && line != "" {
			return line
		}
	}
	return ""
}

// Logger is a logger whose lines go to the returned Lines.
func Logger() (*slog.Logger, *Lines) {
	l := &Lines{}
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug})), l
}

// Default makes slog's default logger write to the returned Lines until the
// test ends. Code that takes slog.Default() when built must be built after
// it; a test that uses it must not run in parallel.
func Default(t *testing.T) *Lines {
	t.Helper()
	before := slog.Default()
	log, lines := Logger()
	slog.SetDefault(log)
	t.Cleanup(func() { slog.SetDefault(before) })
	return lines
}
