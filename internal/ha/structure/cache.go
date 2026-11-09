package structure

import (
	"context"
	"log/slog"
	"sync"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Registry events that invalidate the house structure.
var registryEvents = []string{
	"area_registry_updated",
	"device_registry_updated",
	"entity_registry_updated",
}

// Source reads the registries. The real implementation is the WebSocket
// client; tests supply their own.
type Source interface {
	Areas(context.Context) ([]haws.Area, error)
	Devices(context.Context) ([]haws.Device, error)
	Entities(context.Context) ([]haws.Entity, error)
}

// Subscriber is the part of the WebSocket client the cache needs for
// invalidation.
type Subscriber interface {
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
}

type Options struct {
	Source     Source
	Subscriber Subscriber
	// Debounce collapses a burst of registry updates — renaming ten entities
	// in the UI should cost one rebuild, not ten.
	Debounce time.Duration
	Logger   *slog.Logger
	// OnChange runs whenever the house actually changed (Same). A rebuild
	// that knows the same house does not call it.
	OnChange func(Snapshot)
	// Exposed, when it knows, is what Home Assistant exposes to Assist as
	// the Mira integration sent it (docs/spec/ha-integration.md). It comes
	// before asking Home Assistant, which only answers admins.
	Exposed func(ctx context.Context) (map[string]bool, bool)
}

// Cache holds the current structure snapshot and keeps it up to date.
type Cache struct {
	src      Source
	sub      Subscriber
	debounce time.Duration
	log      *slog.Logger
	onChange func(Snapshot)
	exposed  func(ctx context.Context) (map[string]bool, bool)

	mu   sync.RWMutex
	snap Snapshot

	// dirty signals the debounce loop that a rebuild is due; now that it is
	// due at once.
	dirty chan struct{}
	now   chan struct{}
}

func NewCache(opts Options) *Cache {
	if opts.Debounce == 0 {
		opts.Debounce = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Cache{
		src:      opts.Source,
		sub:      opts.Subscriber,
		debounce: opts.Debounce,
		log:      opts.Logger.With("component", "ha-structure"),
		exposed:  opts.Exposed,
		onChange: opts.OnChange,
		dirty:    make(chan struct{}, 1),
		now:      make(chan struct{}, 1),
	}
}

// Snapshot returns the current structure. It is safe to call before the first
// build; the zero snapshot renders as an empty layer 4.
func (c *Cache) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap
}

// Rebuild reads the registries and replaces the snapshot. It reports whether
// the house changed.
func (c *Cache) Rebuild(ctx context.Context) (bool, error) {
	areas, err := c.src.Areas(ctx)
	if err != nil {
		return false, err
	}
	devices, err := c.src.Devices(ctx)
	if err != nil {
		return false, err
	}
	entities, err := c.src.Entities(ctx)
	if err != nil {
		return false, err
	}
	var exposed map[string]bool
	known := false
	if c.exposed != nil {
		exposed, known = c.exposed(ctx)
	}
	if !known {
		exposed = nil
	}

	snap := Build(Registry{Areas: areas, Devices: devices, Entities: entities, Exposed: exposed})

	c.mu.Lock()
	changed := !Same(c.snap, snap)
	c.snap = snap
	c.mu.Unlock()

	if !snap.ExposureKnown {
		c.log.Warn("what Assist may see is not known yet; Mira waits for the integration's list")
	}
	c.log.Info("house structure built",
		"areas", snap.AreaCount, "entities", snap.EntityCount, "changed", changed)

	if changed && c.onChange != nil {
		c.onChange(snap)
	}
	return changed, nil
}

// Run subscribes to registry changes and rebuilds the structure when they
// arrive, debounced. It returns when ctx ends.
func (c *Cache) Run(ctx context.Context) error {
	if c.sub != nil {
		for _, eventType := range registryEvents {
			cancel, err := c.sub.Subscribe(ctx, eventType, func(haws.Event) { c.Invalidate() })
			if err != nil {
				c.log.Error("could not subscribe to registry updates", "event", eventType, "error", err)
				continue
			}
			defer cancel()
		}
	}

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	pending := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.dirty:
			if !pending {
				pending = true
				timer.Reset(c.debounce)
			}
		case <-c.now:
			// It reads the registries too: a rebuild still waiting is
			// dropped (docs/spec/context.md, 1.3).
			if pending && !timer.Stop() {
				<-timer.C
			}
			pending = false
			c.rebuild(ctx)
		case <-timer.C:
			pending = false
			c.rebuild(ctx)
		}
	}
}

// rebuild is Rebuild for the loop: a failure keeps the last structure, and
// the next change tries again.
func (c *Cache) rebuild(ctx context.Context) {
	if _, err := c.Rebuild(ctx); err != nil && ctx.Err() == nil {
		c.log.Error("structure rebuild failed; the last structure stays until the next change", "error", err)
	}
}

// Now schedules a rebuild at once, without the debounce: the integration
// sent a list of exposed entities, already gathered on its side
// (docs/spec/context.md, 1.3). It does not wait for the rebuild.
func (c *Cache) Now() {
	select {
	case c.now <- struct{}{}:
	default: // a rebuild at once is already scheduled
	}
}

// Invalidate schedules a rebuild. Several calls within the debounce window
// collapse into one.
func (c *Cache) Invalidate() {
	select {
	case c.dirty <- struct{}{}:
	default: // a rebuild is already scheduled
	}
}

// AreaForDevice resolves a satellite's device id to its area.
func (c *Cache) AreaForDevice(deviceID string) (areaID, areaName string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	areaID, ok = c.snap.DeviceArea[deviceID]
	if !ok {
		return "", "", false
	}
	return areaID, c.snap.AreaNames[areaID], true
}

// AreaName returns the display name of an area id.
func (c *Cache) AreaName(areaID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if name, ok := c.snap.AreaNames[areaID]; ok {
		return name
	}
	return areaID
}
