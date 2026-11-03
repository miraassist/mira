package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Change is one state of an entity's history and when it began.
type Change struct {
	State string
	At    time.Time
}

// History reads one entity's state changes between start and end from the
// recorder, the first being the state at start if the recorder still has
// it. A non-admin user may read it (tried 2026-11-03).
func (c *Client) History(ctx context.Context, id string, start, end time.Time) ([]Change, error) {
	raw, err := c.Call(ctx, map[string]any{
		"type": "history/history_during_period", "start_time": start.UTC().Format(time.RFC3339),
		"end_time": end.UTC().Format(time.RFC3339), "entity_ids": []string{id},
		"minimal_response": true, "no_attributes": true, "significant_changes_only": false,
	})
	if err != nil {
		return nil, err
	}
	var payload map[string][]struct {
		S  string  `json:"s"`
		LU float64 `json:"lu"` // seconds since the epoch
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	out := make([]Change, 0, len(payload[id]))
	for _, r := range payload[id] {
		out = append(out, Change{State: r.S, At: time.UnixMilli(int64(r.LU * 1000))})
	}
	return out, nil
}
