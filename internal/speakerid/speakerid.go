// Package speakerid asks the speaker-id service who said a satellite turn's
// text (docs/spec/sprecher.md). The service recognizes the voice beside
// speech recognition; Mira only reads its result and keeps nothing of it.
package speakerid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Timeout bounds one question: a service that is down or slow costs a turn
// at most this, and the turn goes on without the voice.
const Timeout = 300 * time.Millisecond

// within is how far back the service looks for the turn's text, in seconds:
// the text reaches Mira within a second or two of being said.
const within = 10

// Client asks one service.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the service's API (e.g. http://host:10311), or
// nil when url is empty, which is speaker recognition switched off.
func New(base string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	return &Client{base: base, http: &http.Client{Timeout: Timeout}}
}

type result struct {
	Person   *string `json:"person"`
	Decision string  `json:"decision"`
	Score    float64 `json:"score"`
}

// Person is the person key the service recognized as having said text just
// now, with its score; "" when nobody was recognized for sure (uncertain,
// unknown, not found) or the service did not answer in time.
func (c *Client) Person(ctx context.Context, text string) (string, float64, error) {
	if c == nil || strings.TrimSpace(text) == "" {
		return "", 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	q := url.Values{"text": {text}, "within": {fmt.Sprint(within)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/match?"+q.Encode(), nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("speaker-id: HTTP %d", resp.StatusCode)
	}
	var r *result
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, fmt.Errorf("speaker-id: %w", err)
	}
	if r == nil || r.Decision != "accepted" || r.Person == nil {
		return "", 0, nil
	}
	return *r.Person, r.Score, nil
}
