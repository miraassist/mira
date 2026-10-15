package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// StatRow is one period of the recorder's long-term statistics. Change is
// what a counter (state_class total/total_increasing) rose by in the period;
// Mean, Min and Max are set for measurements.
type StatRow struct {
	Start  time.Time
	Change *float64
	Mean   *float64
	Min    *float64
	Max    *float64
}

// Statistics reads hourly ("hour") or five-minute ("5minute") statistics
// since start. A non-admin user may read them, so baselines for watching a
// sensor come from Home Assistant's own history.
func (c *Client) Statistics(ctx context.Context, ids []string, start time.Time, period string) (map[string][]StatRow, error) {
	raw, err := c.Call(ctx, map[string]any{
		"type": "recorder/statistics_during_period", "start_time": start.UTC().Format(time.RFC3339),
		"statistic_ids": ids, "period": period, "types": []string{"change", "mean", "min", "max"},
	})
	if err != nil {
		return nil, err
	}
	var payload map[string][]struct {
		Start  float64  `json:"start"` // milliseconds since the epoch
		Change *float64 `json:"change"`
		Mean   *float64 `json:"mean"`
		Min    *float64 `json:"min"`
		Max    *float64 `json:"max"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode statistics: %w", err)
	}
	out := make(map[string][]StatRow, len(payload))
	for id, rows := range payload {
		for _, r := range rows {
			out[id] = append(out[id], StatRow{Start: time.UnixMilli(int64(r.Start)), Change: r.Change, Mean: r.Mean, Min: r.Min, Max: r.Max})
		}
	}
	return out, nil
}
