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

// Threshold is the score a voice needs when mira.yaml sets none: well above
// a stranger's (0.5 to 0.6 is common between different voices with the
// first model) and below the 0.7 to 0.85 an enrolled person reaches.
const Threshold = 0.7

// Margin is how far the best person has to be ahead of the second: two
// profiles nearly alike are nobody for sure.
const Margin = 0.05

// Utterance is what the service heard one satellite say: the best and the
// second-best person with their scores.
type Utterance struct {
	Person      string  `json:"person"`
	Score       float64 `json:"score"`
	Second      string  `json:"second"`
	SecondScore float64 `json:"second_score"`
}

// Match is every utterance of text in the last seconds, one per satellite
// that heard it; none when the service has nothing or there is no service.
func (c *Client) Match(ctx context.Context, text string) ([]Utterance, error) {
	if c == nil || strings.TrimSpace(text) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	q := url.Values{"text": {text}, "within": {fmt.Sprint(within)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/match?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speaker-id: HTTP %d", resp.StatusCode)
	}
	var found []Utterance
	if err := json.NewDecoder(resp.Body).Decode(&found); err != nil {
		return nil, fmt.Errorf("speaker-id: %w", err)
	}
	return found, nil
}

// Decide is the person who said it, or "": every satellite that heard it
// names the same person, at or above that person's threshold and clearly
// ahead of the second.
func Decide(found []Utterance, threshold func(person string) float64) string {
	person := ""
	for _, u := range found {
		if u.Person == "" || (person != "" && u.Person != person) {
			return ""
		}
		if u.Score < threshold(u.Person) || (u.Second != "" && u.Score-u.SecondScore < Margin) {
			return ""
		}
		person = u.Person
	}
	return person
}
