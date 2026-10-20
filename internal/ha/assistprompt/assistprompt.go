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
//
// Part of the prompt depends on the device that asks: which area it is in,
// and whether it can start timers. Home Assistant puts those lines at the
// end, and so does it keep its own cache intact. Mira fetches the prompt
// once without a device and once as each satellite, takes what all of them
// share as layer 4 (the same for every room and channel, so the cache of
// everything behind it holds), and gives each request the rest of its own
// satellite's version in the turn layer, the last part of the prompt
// (docs/spec/kontext.md).
package assistprompt

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Source fetches a prompt from an MCP server, as a device would get it
// ("" for none).
type Source interface {
	PromptAs(ctx context.Context, server, name, device string) (string, error)
}

type Options struct {
	Source Source
	Server string
	Name   string // "Assist"
	// Devices are the voice satellites' device ids; the prompt is fetched
	// as each of them.
	Devices func() []string
	// Interval is how often Run fetches the prompt to notice changes that
	// come without any event.
	Interval time.Duration
	// OnChange runs after layer 4 changed (not on the first fetch). A change
	// of a device's own lines does not count: they are not in the prefix.
	OnChange func()
	Logger   *slog.Logger
}

// minShared is the share of the prompt that all versions must have in
// common. Less means Home Assistant no longer puts the device lines at the
// end, and the cache gain shrinks (the prompt is still right).
const minShared = 0.9

type Cache struct {
	opts Options
	log  *slog.Logger

	mu      sync.RWMutex
	base    string
	tails   map[string]string // by device; "" is the version without one
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
	return &Cache{opts: opts, log: opts.Logger.With("component", "assist-prompt"), tails: map[string]string{}}
}

// Text is layer 4: what every version of the prompt has in common, or "" if
// it was never fetched. The caller falls back to its own structure block
// then.
func (c *Cache) Text() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base
}

// Tail is what Home Assistant adds for that device, to go into the turn
// layer. For no device, or one that was not fetched, it is the version
// without a device: a text channel gets what a device-less request gets.
func (c *Cache) Tail(device string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if t, ok := c.tails[device]; ok && device != "" {
		return t
	}
	return c.tails[""]
}

// Refresh fetches the prompt and reports whether layer 4 changed. A failed
// fetch keeps the last text: a short MCP outage must not empty layer 4 and
// break every slot's cache. A satellite whose own fetch failed keeps its old
// tail.
func (c *Cache) Refresh(ctx context.Context) (bool, error) {
	plain, err := c.opts.Source.PromptAs(ctx, c.opts.Server, c.opts.Name, "")
	if err != nil {
		return false, err
	}
	plain = strings.TrimSpace(plain)
	versions := map[string]string{"": plain}
	if c.opts.Devices != nil {
		for _, device := range c.opts.Devices() {
			text, err := c.opts.Source.PromptAs(ctx, c.opts.Server, c.opts.Name, device)
			if err != nil {
				c.log.Warn("could not fetch the assist prompt for a satellite", "error", err)
				continue
			}
			versions[device] = strings.TrimSpace(text)
		}
	}

	base := sharedStart(versions)
	if len(plain) > 0 && float64(len(base)) < minShared*float64(len(plain)) {
		c.log.Warn("the assist prompt differs per device well before its end; the cache gain shrinks",
			"shared", len(base), "of", len(plain))
	}
	tails := make(map[string]string, len(versions))
	for device, text := range versions {
		tails[device] = strings.TrimSpace(strings.TrimPrefix(text, base))
	}

	c.mu.Lock()
	changed := base != c.base
	first := !c.fetched
	for device, tail := range c.tails { // keep the tail of a satellite that failed this time
		if _, ok := tails[device]; !ok {
			tails[device] = tail
		}
	}
	c.base, c.tails, c.fetched = base, tails, true
	c.mu.Unlock()
	if changed {
		c.log.Info("assist prompt updated", "chars", len(base), "first", first)
		if !first && c.opts.OnChange != nil {
			c.opts.OnChange()
		}
	}
	return changed, nil
}

// sharedStart is what all versions begin with, cut at a line end so no line
// is split between layer 4 and the turn.
func sharedStart(versions map[string]string) string {
	var first string
	set := false
	for _, v := range versions {
		if !set {
			first, set = v, true
			continue
		}
		n := 0
		for n < len(first) && n < len(v) && first[n] == v[n] {
			n++
		}
		first = first[:n]
	}
	if len(versions) == 1 {
		return first
	}
	if i := strings.LastIndexByte(first, '\n'); i >= 0 {
		return strings.TrimSpace(first[:i])
	}
	return ""
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
