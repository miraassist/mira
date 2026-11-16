package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/miraassist/mira/internal/excerpt"
)

// State reads one entity's state as Home Assistant's REST interface gives
// it (GET /api/states/<entity_id>): entity_id, state, attributes,
// last_changed, last_updated, context. ErrNotFound when Home Assistant has
// no such entity, ErrNotAdmin when it refuses Mira's token
// (docs/spec/ha-api.md). One request, no retry.
func (c *Client) State(ctx context.Context, entityID string) (json.RawMessage, error) {
	raw, err := c.rest(ctx, http.MethodGet, "/api/states/"+url.PathEscape(entityID), nil)
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("home assistant's state of %s is not JSON: %q", entityID, excerpt.Bytes(raw))
	}
	return raw, nil
}
