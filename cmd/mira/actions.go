package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/trace"
)

// haActions lets a skill finish with a Home Assistant action
// (docs/spec/skill-aktionen.md): whether the action exists, calling it, and
// the placeholders of a satellite's area.
type haActions struct{ a *app }

// core is a building block of the core a skill's action may name where Home
// Assistant has no action (docs/spec/skill-aktionen.md): the undo of termin.
func (h haActions) core(action string) func(context.Context, map[string]any) error {
	switch action {
	case "calendar.delete_created":
		if h.a.calendar != nil {
			return h.a.calendar.DeleteCreated
		}
	}
	return nil
}

func (h haActions) Has(ctx context.Context, action string) (bool, error) {
	if h.core(action) != nil {
		return true, nil
	}
	domain, service, ok := strings.Cut(action, ".")
	if !ok {
		return false, nil
	}
	services, err := h.a.ha.Services(ctx)
	if err != nil {
		return false, err
	}
	_, ok = services[domain][service]
	return ok, nil
}

func (h haActions) Call(ctx context.Context, action string, data map[string]any) error {
	if f := h.core(action); f != nil {
		trace.Add(ctx, trace.Action, action, "")
		return f(ctx, data)
	}
	domain, service, ok := strings.Cut(action, ".")
	if !ok {
		return fmt.Errorf("%q is no action", action)
	}
	trace.Add(ctx, trace.Action, action, "")
	return h.a.ha.CallService(ctx, domain, service, data, nil)
}

// Place is the area of a satellite as placeholders: area, area_name,
// satellite and satellite.device, the ESPHome node its actions are named
// after (esphome.<node>_alarm_set).
func (h haActions) Place(ctx context.Context, area string) map[string]string {
	vars := map[string]string{"area": area, "area_name": area}
	if name := h.a.structure.Snapshot().AreaNames[area]; name != "" {
		vars["area_name"] = name
	}
	sat := h.a.liveConfig().Satellites[area]
	if sat == "" {
		return vars
	}
	vars["satellite"] = sat
	if node := h.a.nodeOf(ctx, sat); node != "" {
		vars["satellite.device"] = node
	}
	return vars
}

// nodeOf is the ESPHome node name of an entity's device, as its actions
// spell it; "" when unknown.
func (a *app) nodeOf(ctx context.Context, entity string) string {
	entities, err := a.ha.Entities(ctx)
	if err != nil {
		return ""
	}
	devices, err := a.ha.Devices(ctx)
	if err != nil {
		return ""
	}
	for _, e := range entities {
		if e.EntityID != entity {
			continue
		}
		for _, d := range devices {
			if d.ID == e.DeviceID {
				return slug(d.Name)
			}
		}
	}
	return ""
}
