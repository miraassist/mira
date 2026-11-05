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
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *Lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// String is all lines so far.
func (l *Lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// Line is the first line holding every part; "" without one.
func (l *Lines) Line(parts ...string) string {
	for _, line := range strings.Split(l.String(), "\n") {
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
