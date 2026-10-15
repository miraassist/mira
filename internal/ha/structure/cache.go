package structure

import (
	"context"
	"github.com/sker65/mira/internal/i18n"
	"log/slog"
	"sync"
	"time"

	haws "github.com/sker65/mira/internal/ha/ws"
)

// Registry events that invalidate the structure block.
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
	ExposedEntities(context.Context) (map[string]bool, error)
}

// Subscriber is the part of the WebSocket client the cache needs for
// invalidation.
type Subscriber interface {
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
}

type Options struct {
	// Lang is the language of the block's labels.
	Lang       i18n.Lang
	Source     Source
	Subscriber Subscriber
	// Debounce collapses a burst of registry updates — renaming ten entities
	// in the UI should cost one rebuild, not ten.
	Debounce time.Duration
	Logger   *slog.Logger
	// OnChange runs whenever the block actually changed. A rebuild that
	// produces identical bytes does not call it: the prompt cache is still
	// valid and there is nothing to warm up.
	OnChange func(Snapshot)
}

// Cache holds the current structure snapshot and keeps it up to date.
type Cache struct {
	lang     i18n.Lang
	src      Source
	sub      Subscriber
	debounce time.Duration
	log      *slog.Logger
	onChange func(Snapshot)

	mu   sync.RWMutex
	snap Snapshot

	// dirty signals the debounce loop that a rebuild is due.
	dirty chan struct{}
}

func NewCache(opts Options) *Cache {
	if opts.Debounce == 0 {
		opts.Debounce = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Cache{
		lang:     opts.Lang,
		src:      opts.Source,
		sub:      opts.Subscriber,
		debounce: opts.Debounce,
		log:      opts.Logger.With("component", "ha-structure"),
		onChange: opts.OnChange,
		dirty:    make(chan struct{}, 1),
	}
}

// Snapshot returns the current block. It is safe to call before the first
// build; the zero snapshot renders as an empty layer 4.
func (c *Cache) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap
}

// Rebuild reads the registries and replaces the snapshot. It reports whether
// the block changed.
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
	exposed, err := c.src.ExposedEntities(ctx)
	if err != nil {
		// Not fatal: Build falls back to the registry options, and the
		// snapshot records that exposure is unknown.
		c.log.Warn("could not read the exposure list, falling back to entity options", "error", err)
		exposed = nil
	}

	snap := Build(Registry{Lang: c.lang, Areas: areas, Devices: devices, Entities: entities, Exposed: exposed})

	c.mu.Lock()
	changed := c.snap.Block != snap.Block
	c.snap = snap
	c.mu.Unlock()

	if !snap.ExposureKnown {
		c.log.Warn("exposure is unknown; the structure block lists every usable entity",
			"entities", snap.EntityCount)
	}
	c.log.Info("structure block built",
		"areas", snap.AreaCount, "entities", snap.EntityCount, "changed", changed)

	if changed && c.onChange != nil {
		c.onChange(snap)
	}
	return changed, nil
}

// Run subscribes to registry changes and rebuilds the block when they
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
		case <-timer.C:
			pending = false
			if _, err := c.Rebuild(ctx); err != nil && ctx.Err() == nil {
				c.log.Error("structure rebuild failed", "error", err)
			}
		}
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

// Block is prompt layer 4.
func (c *Cache) Block() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap.Block
}
