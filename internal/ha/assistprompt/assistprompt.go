// Package assistprompt keeps Home Assistant's own Assist prompt: the tool
// instructions and the overview of exposed entities (names, aliases, domain,
// area; no states) that HA gives its built-in LLM agents. Home Assistant's
// MCP server serves it as the prompt "Assist". It becomes layer 4, so the
// model sees the house exactly the way the Assist tools expect it.
//
// The text has no states and no time in it, so it is stable between requests.
// It is fetched again when the structure changes, when the MCP server comes
// back, and on a timer — exposing an entity changes the text without any
// registry event — and replaced only when the text really differs.
package assistprompt

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Source fetches a prompt from an MCP server.
type Source interface {
	Prompt(ctx context.Context, server, name string) (string, error)
}

type Options struct {
	Source Source
	Server string
	Name   string // "Assist"
	// Interval is how often Run fetches the prompt to notice changes that
	// come without any event.
	Interval time.Duration
	// OnChange runs after the text changed (not on the first fetch).
	OnChange func()
	Logger   *slog.Logger
}

type Cache struct {
	opts Options
	log  *slog.Logger

	mu      sync.RWMutex
	text    string
	fetched bool
}

func New(opts Options) *Cache {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Name == "" {
		opts.Name = "Assist"
	}
	if opts.Interval == 0 {
		opts.Interval = 5 * time.Minute
	}
	return &Cache{opts: opts, log: opts.Logger.With("component", "assist-prompt")}
}

// Text is the current prompt, or "" if it was never fetched. The caller falls
// back to its own structure block then.
func (c *Cache) Text() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.text
}

// Refresh fetches the prompt and reports whether the text changed. A failed
// fetch keeps the last text: a short MCP outage must not empty layer 4 and
// break every slot's cache.
func (c *Cache) Refresh(ctx context.Context) (bool, error) {
	text, err := c.opts.Source.Prompt(ctx, c.opts.Server, c.opts.Name)
	if err != nil {
		return false, err
	}
	text = strings.TrimSpace(text)
	c.mu.Lock()
	changed := text != c.text
	first := !c.fetched
	c.text, c.fetched = text, true
	c.mu.Unlock()
	if changed {
		c.log.Info("assist prompt updated", "chars", len(text), "first", first)
		if !first && c.opts.OnChange != nil {
			c.opts.OnChange()
		}
	}
	return changed, nil
}

// Trigger refreshes in the background, for event handlers that must not block.
// A failure is only logged at debug level: at startup the registry is ready
// before the MCP server, and the MCP connect triggers another refresh.
func (c *Cache) Trigger(ctx context.Context) {
	go func() {
		if _, err := c.Refresh(ctx); err != nil {
			c.log.Debug("could not refresh the assist prompt", "error", err)
		}
	}()
}

// Run refreshes on the interval until ctx ends.
func (c *Cache) Run(ctx context.Context) error {
	t := time.NewTicker(c.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if _, err := c.Refresh(ctx); err != nil {
				c.log.Warn("could not refresh the assist prompt", "error", err)
			}
		}
	}
}
