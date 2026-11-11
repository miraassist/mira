package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
)

// EventStarted is the event Mira fires once, after her start, when she is
// connected to Home Assistant and her HTTP API listens: the integration then
// sends its settings at once instead of at its next poll
// (docs/spec/editing.md, decision 10).
const EventStarted = "mira_started"

// caller is the part of Home Assistant's client the signal needs.
type caller interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
}

// startSignal fires once both parts are up: Home Assistant connected (the
// event goes there) and the HTTP API listening (the integration answers on
// it; fired before, its request found no Mira, seen on the test HA
// 2026-11-11). A reconnect later fires nothing.
type startSignal struct {
	mu                  sync.Mutex
	haUp, httpUp, fired bool
	// fire sends the event; set before either part comes up.
	fire func(ctx context.Context)
}

func (s *startSignal) up(ctx context.Context, ha, http bool) {
	s.mu.Lock()
	s.haUp, s.httpUp = s.haUp || ha, s.httpUp || http
	now := s.haUp && s.httpUp && !s.fired && s.fire != nil
	if now {
		s.fired = true
	}
	s.mu.Unlock()
	if now {
		s.fire(ctx)
	}
}

// fireStarted fires EventStarted with Mira's id and version; a refusal is a
// warning, the integration's poll then sends the settings.
func fireStarted(ctx context.Context, ha caller, id, version string, log *slog.Logger) {
	_, err := ha.Call(ctx, map[string]any{"type": "fire_event", "event_type": EventStarted,
		"event_data": map[string]any{"id": id, "version": version}})
	if err != nil {
		log.Warn("could not tell Home Assistant that Mira started; the integration sends its settings at its next poll",
			"event", EventStarted, "error", err)
		return
	}
	log.Info("told Home Assistant that Mira started", "event", EventStarted)
}
