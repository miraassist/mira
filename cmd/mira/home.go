package main

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// homeCache keeps where the house is, as Home Assistant's configuration says
// (Settings → System → General), for an hour.
type homeCache struct {
	mu   sync.Mutex
	at   time.Time
	vals map[string]string
	// internalURL is Home Assistant's own address for the home network.
	internalURL string
}

// home is the house's location for skills: the placeholders home.latitude,
// home.longitude and home.name (the guide, "Eigenen Skill erstellen"). Without
// an answer from Home Assistant it is empty; the skill then reads "-".
func (a *app) home(ctx context.Context) map[string]string {
	a.homeCache.mu.Lock()
	defer a.homeCache.mu.Unlock()
	if a.homeCache.vals != nil && time.Since(a.homeCache.at) < time.Hour {
		return a.homeCache.vals
	}
	if a.ha == nil {
		return nil
	}
	raw, err := a.ha.Call(ctx, map[string]any{"type": "get_config"})
	if err != nil {
		a.log.Warn("could not read where the house is", "error", err)
		return a.homeCache.vals
	}
	var cfg struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"location_name"`
		Internal  string  `json:"internal_url"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return a.homeCache.vals
	}
	a.homeCache.vals = map[string]string{
		"home.latitude":  strconv.FormatFloat(cfg.Latitude, 'f', 4, 64),
		"home.longitude": strconv.FormatFloat(cfg.Longitude, 'f', 4, 64),
		"home.name":      cfg.Name,
	}
	a.homeCache.internalURL = cfg.Internal
	a.homeCache.at = time.Now()
	return a.homeCache.vals
}

// haAddress is Home Assistant's address as satellites reach it
// (docs/spec/satellit.md): its internal URL, else the one Mira uses.
func (a *app) haAddress(ctx context.Context) string {
	a.home(ctx)
	a.homeCache.mu.Lock()
	defer a.homeCache.mu.Unlock()
	if a.homeCache.internalURL != "" {
		return a.homeCache.internalURL
	}
	return a.cfg.HA.URL
}
