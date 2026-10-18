package radio

import (
	"context"
	"encoding/json"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// radioSource is the Radio Browser of Home Assistant's media sources.
const radioSource = "media-source://radio_browser"

// HA is the part of the Home Assistant client the radio uses.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
}

// Search looks stations up in the Radio Browser by name.
func Search(ha HA) func(ctx context.Context, query string) ([]Station, error) {
	return func(ctx context.Context, query string) ([]Station, error) {
		raw, err := ha.Call(ctx, map[string]any{
			"type": "media_source/search_media", "media_content_id": radioSource, "search_query": query,
		})
		if err != nil {
			return nil, err
		}
		var res struct {
			Result []struct {
				Title       string `json:"title"`
				ContentID   string `json:"media_content_id"`
				ContentType string `json:"media_content_type"`
				CanPlay     bool   `json:"can_play"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, err
		}
		var out []Station
		for _, r := range res.Result {
			if r.CanPlay && strings.HasPrefix(r.ContentID, radioSource+"/") {
				out = append(out, Station{Title: r.Title, ContentID: r.ContentID, ContentType: r.ContentType})
			}
		}
		return out, nil
	}
}

// Play starts a station on a media player.
func Play(ha HA) func(ctx context.Context, entity string, st Station) error {
	return func(ctx context.Context, entity string, st Station) error {
		return ha.CallService(ctx, "media_player", "play_media", map[string]any{
			"media_content_id": st.ContentID, "media_content_type": st.ContentType,
		}, &haws.Target{EntityID: []string{entity}})
	}
}
