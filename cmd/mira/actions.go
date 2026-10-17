package main

import (
	"context"
	"fmt"
	"strings"
)

// haActions lets a skill finish with a Home Assistant action
// (docs/spec/skill-aktionen.md): whether the action exists, calling it, and
// the placeholders of a satellite's area.
type haActions struct{ a *app }

func (h haActions) Has(ctx context.Context, action string) (bool, error) {
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
	domain, service, ok := strings.Cut(action, ".")
	if !ok {
		return fmt.Errorf("%q is no action", action)
	}
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
