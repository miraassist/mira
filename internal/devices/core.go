// Package devices takes a device into the house by voice: the part every way
// of adding a device shares (docs/spec/device-onboarding.md,
// docs/spec/found-devices.md). A thread of the skill geraet-einbinden holds
// one device; device_assign puts it into a room, device_finish exposes it to
// Assist and names it. How the device came (Zigbee2MQTT, ZHA, a device Home
// Assistant found, a Wi-Fi device) only decides how it is renamed.
package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Skill is the skill whose tools these are.
const Skill = "geraet-einbinden"

// Tool names.
const (
	ToolAssign = "device_assign"
	ToolFinish = "device_finish"
)

// Steps every plan of the skill has, ticked by the tools (matched by text).
var stepNames = map[i18n.Lang]struct{ Room, Name string }{
	i18n.DE: {"Raum", "Name"},
	i18n.EN: {"Room", "Name"},
}

// HA is what the core needs of Home Assistant's websocket.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
}

// Device is the device a thread takes in. Rename names it where it lives:
// nil is Home Assistant's name of the device (name_by_user).
type Device struct {
	ID     string
	Rename func(ctx context.Context, name string) error
	// RenameEntities renames the entity ids along with the name, as Home
	// Assistant's own rename dialog offers (decided for ZHA, decision 12).
	RenameEntities bool
}

type CoreOptions struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	HA     HA
	// AreaKeys are every lower-cased area id, name and alias to the area id,
	// AreaName an area's name (the structure Mira keeps).
	AreaKeys func() map[string]string
	AreaName func(id string) string
	// DeviceName is a device's name as Home Assistant shows it now (its
	// user's name, else its own), "" for a device it does not know.
	DeviceName func(id string) string
	// Thread finds the turn's thread of the skill; Tick ticks a step of its
	// plan; End ends it as done.
	Thread func(ctx context.Context, inv tools.Invocation) string
	Tick   func(ctx context.Context, thread, match string) error
	End    func(ctx context.Context, thread string)
}

// Core holds each thread's device and has the tools for its room and name.
type Core struct {
	o   CoreOptions
	t   coreTexts
	log *slog.Logger

	mu       sync.Mutex
	byThread map[string]*held
}

type held struct {
	d    Device
	area string
}

func NewCore(o CoreOptions) *Core {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Core{o: o, t: i18n.Pick(coreCatalog, o.Lang), log: o.Logger.With("component", "devices"), byThread: map[string]*held{}}
}

// Hold makes d the thread's device: the room and the name are next.
func (c *Core) Hold(thread string, d Device) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byThread[thread] = &held{d: d}
}

// Release forgets a thread's device (the thread ended).
func (c *Core) Release(thread string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byThread, thread)
}

// Holds reports whether a thread is taking in the device.
func (c *Core) Holds(deviceID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range c.byThread {
		if h.d.ID == deviceID {
			return true
		}
	}
	return false
}

// Tools are device_assign and device_finish.
func (c *Core) Tools() []tools.InternalTool {
	t := c.t
	return []tools.InternalTool{
		{Name: ToolAssign, Description: t.assignDesc, Skill: Skill,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"room":{"type":"string","description":%q}},"required":["room"]}`, t.argRoom),
			Run:    c.assign},
		{Name: ToolFinish, Description: t.finishDesc, Skill: Skill,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string","description":%q}},"required":["name"]}`, t.argName),
			Run:    c.finish},
	}
}

func (c *Core) current(ctx context.Context, inv tools.Invocation) (string, *held) {
	thread := c.o.Thread(ctx, inv)
	c.mu.Lock()
	defer c.mu.Unlock()
	return thread, c.byThread[thread]
}

func (c *Core) tick(ctx context.Context, thread, step string) {
	if err := c.o.Tick(ctx, thread, step); err != nil {
		c.log.Warn("could not tick the plan of the device's thread", "thread", thread, "error", err)
	}
}

func (c *Core) steps() struct{ Room, Name string } {
	if s, ok := stepNames[c.o.Lang]; ok {
		return s
	}
	return stepNames[i18n.DE]
}

func (c *Core) assign(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct{ Room string }
	if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Room) == "" {
		return "", errors.New("room is required")
	}
	t := c.t
	thread, h := c.current(ctx, inv)
	if h == nil {
		return t.noDevice, nil
	}
	keys := c.o.AreaKeys()
	area := keys[strings.ToLower(strings.TrimSpace(args.Room))]
	if area == "" {
		names := map[string]bool{}
		for _, id := range keys {
			names[c.o.AreaName(id)] = true
		}
		list := make([]string, 0, len(names))
		for n := range names {
			list = append(list, n)
		}
		sort.Strings(list)
		// The room comes again: the satellite keeps listening
		// (docs/spec/keep-listening.md).
		tools.TurnOf(ctx).WaitForUser()
		return fmt.Sprintf(t.unknownRoom, args.Room, strings.Join(list, ", ")), nil
	}
	if _, err := c.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/update", "device_id": h.d.ID, "area_id": area}); err != nil {
		c.log.Error("could not put the device into its room", "device", h.d.ID, "area", area, "error", err)
		return fmt.Sprintf(t.assignFailed, err), nil
	}
	c.mu.Lock()
	h.area = area
	c.mu.Unlock()
	c.tick(ctx, thread, c.steps().Room)
	return fmt.Sprintf(t.assigned, c.o.AreaName(area)), nil
}

func (c *Core) finish(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct{ Name string }
	if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Name) == "" {
		return "", errors.New("name is required")
	}
	name := strings.TrimSpace(args.Name)
	t := c.t
	thread, h := c.current(ctx, inv)
	if h == nil {
		return t.noDevice, nil
	}
	// Exposed first: Home Assistant keeps it when a rename of Zigbee2MQTT
	// makes it remove and create the entities again (seen on the test HA
	// 2026-11-09).
	exposed, err := c.Expose(ctx, h.d.ID)
	if err != nil {
		c.log.Error("could not expose the device's entities", "device", h.d.ID, "error", err)
		return fmt.Sprintf(t.finishFailed, err), nil
	}
	// The entity ids begin with the device's name before the rename.
	old := ""
	if h.d.RenameEntities && c.o.DeviceName != nil {
		if old = c.o.DeviceName(h.d.ID); old == "" {
			c.log.Warn("the device's entity ids are not renamed: its name before is not known", "device", h.d.ID)
		}
	}
	if err := c.rename(ctx, h.d, name); err != nil {
		c.log.Error("could not rename the device", "device", h.d.ID, "name", name, "error", err)
		return fmt.Sprintf(t.renameFailed, exposed, err), nil
	}
	if h.d.RenameEntities {
		if err := c.renameEntities(ctx, h.d.ID, old, name); err != nil {
			c.log.Error("could not rename the device's entity ids", "device", h.d.ID, "name", name, "error", err)
			return fmt.Sprintf(t.renameFailed, exposed, err), nil
		}
	}
	c.mu.Lock()
	area := h.area
	c.mu.Unlock()
	c.log.Info("device added", "thread", thread, "device", h.d.ID, "name", name, "area", area, "exposed", exposed)
	c.tick(ctx, thread, c.steps().Name)
	c.Release(thread)
	c.o.End(ctx, thread)
	tools.TurnOf(ctx).DialogEnded() // the device is in: nothing is awaited
	return fmt.Sprintf(t.finished, name, c.o.AreaName(area), exposed), nil
}

func (c *Core) rename(ctx context.Context, d Device, name string) error {
	if d.Rename != nil {
		return d.Rename(ctx, name)
	}
	if _, err := c.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/update", "device_id": d.ID, "name_by_user": name}); err != nil {
		return err
	}
	return nil
}

// renameEntities gives the device's entities ids after its new name, as Home
// Assistant's rename dialog does: an entity id whose object part begins with
// the old name's slug gets the new name's slug there instead.
func (c *Core) renameEntities(ctx context.Context, deviceID, oldName, name string) error {
	oldSlug, newSlug := Slug(oldName), Slug(name)
	if oldSlug == "" || oldSlug == newSlug {
		return nil
	}
	entries, err := c.entries(ctx, deviceID)
	if err != nil {
		return err
	}
	for _, e := range entries {
		domain, object, ok := strings.Cut(e.EntityID, ".")
		if !ok || !strings.HasPrefix(object, oldSlug) {
			continue
		}
		target := domain + "." + newSlug + strings.TrimPrefix(object, oldSlug)
		if _, err := c.o.HA.Call(ctx, map[string]any{"type": "config/entity_registry/update", "entity_id": e.EntityID, "new_entity_id": target}); err != nil {
			return fmt.Errorf("entity %s: %w", e.EntityID, err)
		}
	}
	return nil
}

// entry is one entity of a device in the entity registry.
type entry struct {
	EntityID string `json:"entity_id"`
	DeviceID string `json:"device_id"`
	UniqueID string `json:"unique_id"`
	Category string `json:"entity_category"`
}

// entries reads the entities of a device: search/related, then the entity
// registry of them.
func (c *Core) entries(ctx context.Context, deviceID string) ([]entry, error) {
	raw, err := c.o.HA.Call(ctx, map[string]any{"type": "search/related", "item_type": "device", "item_id": deviceID})
	if err != nil {
		return nil, err
	}
	var related struct {
		Entity []string `json:"entity"`
	}
	if err := json.Unmarshal(raw, &related); err != nil {
		return nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	if len(related.Entity) == 0 {
		return nil, nil
	}
	raw, err = c.o.HA.Call(ctx, map[string]any{"type": "config/entity_registry/get_entries", "entity_ids": related.Entity})
	if err != nil {
		return nil, err
	}
	var out map[string]*entry
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	var list []entry
	for id, e := range out {
		if e != nil {
			e.EntityID = id
			list = append(list, *e)
		}
	}
	slices.SortFunc(list, func(a, b entry) int { return strings.Compare(a.EntityID, b.EntityID) })
	return list, nil
}

// Slug is a name as Home Assistant makes it part of an entity id.
func Slug(name string) string {
	r := strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "Ä", "a", "Ö", "o", "Ü", "u")
	s := strings.ToLower(r.Replace(name))
	var b strings.Builder
	under := false
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			under = false
		} else if !under && b.Len() > 0 {
			b.WriteByte('_')
			under = true
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

// Expose exposes the device's entities to Assist, but those Home Assistant
// marks as configuration or diagnostic; it returns how many.
func (c *Core) Expose(ctx context.Context, deviceID string) (int, error) {
	entries, err := c.entries(ctx, deviceID)
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, e := range entries {
		if e.Category == "" {
			ids = append(ids, e.EntityID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := c.o.HA.Call(ctx, map[string]any{"type": "homeassistant/expose_entity", "assistants": []string{"conversation"},
		"entity_ids": ids, "should_expose": true}); err != nil {
		return 0, err
	}
	return len(ids), nil
}
