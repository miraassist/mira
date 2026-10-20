package timer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Intents runs one of Home Assistant's intents on a satellite.
type Intents interface {
	Handle(ctx context.Context, name string, data map[string]any, deviceID, language string) (IntentResult, error)
}

// IntentResult is what an intent answered: an error with its text, or, for
// the status of timers, the timers.
type IntentResult struct {
	Error    string
	Timers   []HATimer
	Canceled int
}

// HATimer is one timer of Home Assistant.
type HATimer struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	IsActive         bool   `json:"is_active"`
	TotalSecondsLeft int    `json:"total_seconds_left"`
}

// HTTPIntents calls /api/intent/handle of Home Assistant.
type HTTPIntents struct {
	URL, Token string
	Client     *http.Client
}

func (h HTTPIntents) Handle(ctx context.Context, name string, data map[string]any, deviceID, language string) (IntentResult, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "data": data, "device_id": deviceID, "language": language})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.URL, "/")+"/api/intent/handle", bytes.NewReader(body))
	if err != nil {
		return IntentResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Content-Type", "application/json")
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return IntentResult{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return IntentResult{}, fmt.Errorf("intent %s: HTTP %d", name, resp.StatusCode)
	}
	var out struct {
		ResponseType string `json:"response_type"`
		Speech       struct {
			Plain struct {
				Speech string `json:"speech"`
			} `json:"plain"`
		} `json:"speech"`
		SpeechSlots struct {
			Timers   []HATimer `json:"timers"`
			Canceled int       `json:"canceled"`
		} `json:"speech_slots"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return IntentResult{}, err
	}
	if out.ResponseType == "error" {
		return IntentResult{Error: out.Speech.Plain.Speech}, nil
	}
	return IntentResult{Timers: out.SpeechSlots.Timers, Canceled: out.SpeechSlots.Canceled}, nil
}
