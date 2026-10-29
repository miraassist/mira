package main

import (
	"context"
	"slices"
	"sync"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// What a satellite's tools need of its device (its speaker, its node name,
// its actions) comes from what Mira keeps current by Home Assistant's
// events: the devices and entities from the structure snapshot (registry
// events), the actions from a cache the service events clear. Nothing is
// read from a registry per tool call (docs/spec/ha-anbindung.md, Watching
// states).

// deviceEntity is the first entity of a domain on a device, "" for none.
func (a *app) deviceEntity(deviceID, domain string) string {
	if deviceID == "" {
		return ""
	}
	return a.structure.Snapshot().DeviceEntity(deviceID, domain)
}

// deviceNode is a device's ESPHome node name, as its actions are named
// (esphome.<node>_notify); "" for an unknown device.
func (a *app) deviceNode(deviceID string) string {
	d, ok := a.structure.Snapshot().Devices[deviceID]
	if !ok {
		return ""
	}
	return slug(d.Name)
}

// entityDevice is the device of an entity, "" for none.
func (a *app) entityDevice(entity string) string {
	return a.structure.Snapshot().Entities[entity].DeviceID
}

// serviceCache keeps Home Assistant's actions until one is registered or
// removed: read once when asked, cleared by the event.
type serviceCache struct {
	mu   sync.Mutex
	list map[string]map[string][]string
}

// services are Home Assistant's actions: domain -> action -> its fields.
func (a *app) services(ctx context.Context) (map[string]map[string][]string, error) {
	a.serviceCache.mu.Lock()
	list := a.serviceCache.list
	a.serviceCache.mu.Unlock()
	if list != nil {
		return list, nil
	}
	list, err := a.ha.Services(ctx)
	if err != nil {
		return nil, err
	}
	a.serviceCache.mu.Lock()
	a.serviceCache.list = list
	a.serviceCache.mu.Unlock()
	return list, nil
}

// hasAction says whether Home Assistant offers domain.action with all of
// fields.
func (a *app) hasAction(ctx context.Context, domain, action string, fields ...string) bool {
	list, err := a.services(ctx)
	if err != nil {
		a.log.Warn("could not read Home Assistant's actions", "error", err)
		return false
	}
	have, ok := list[domain][action]
	if !ok {
		return false
	}
	for _, f := range fields {
		if !slices.Contains(have, f) {
			return false
		}
	}
	return true
}

// watchServices clears the action cache whenever Home Assistant registers or
// removes one (an ESPHome device connecting brings its actions). The handler
// runs on the connection's read loop and only clears.
func (a *app) watchServices(ctx context.Context) error {
	clear := func(haws.Event) {
		a.serviceCache.mu.Lock()
		a.serviceCache.list = nil
		a.serviceCache.mu.Unlock()
	}
	for _, event := range []string{"service_registered", "service_removed"} {
		cancel, err := a.ha.Subscribe(ctx, event, clear)
		if err != nil {
			return err
		}
		defer cancel()
	}
	<-ctx.Done()
	return ctx.Err()
}
