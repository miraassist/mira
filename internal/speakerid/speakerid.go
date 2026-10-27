// Package speakerid asks the speaker-id service who said a satellite turn's
// text (docs/spec/sprecher.md). The service recognizes the voice beside
// speech recognition; Mira only reads its result and keeps nothing of it.
package speakerid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	base, token string
	http        *http.Client
}

// New returns a client for the service's API (e.g. http://host:10311) with
// its token, or nil when url is empty, which is speaker recognition switched
// off.
func New(base, token string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	return &Client{base: base, token: token, http: &http.Client{}}
}

// call sends one request and decodes the answer into out (nil: none).
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("speaker-id: HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("speaker-id: %w", err)
	}
	return nil
}

// ErrNotFound: the utterance or the person is not (or no longer) there.
var ErrNotFound = errors.New("speaker-id: not found")

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
// It never takes longer than Timeout.
func (c *Client) Match(ctx context.Context, text string) ([]Utterance, error) {
	if c == nil || strings.TrimSpace(text) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	q := url.Values{"text": {text}, "within": {fmt.Sprint(within)}}
	var found []Utterance
	err := c.call(ctx, http.MethodGet, "/api/match?"+q.Encode(), nil, &found)
	return found, err
}

// Heard is an utterance as enrolling shows it.
type Heard struct {
	ID         string  `json:"id"`
	At         float64 `json:"at"`
	Seconds    float64 `json:"seconds"`
	Transcript string  `json:"transcript"`
	Assigned   string  `json:"assigned,omitempty"`
	// Person is who the service thinks it is, with the score; empty from
	// a service before 0.11.0.
	Person string  `json:"person,omitempty"`
	Score  float64 `json:"score,omitempty"`
}

// HeardSince is what the service heard since since (unix seconds; it looks
// back five minutes at most), oldest first, and the service's clock now.
func (c *Client) HeardSince(ctx context.Context, since float64) ([]Heard, float64, error) {
	var r struct {
		Now   float64 `json:"now"`
		Heard []Heard `json:"heard"`
	}
	err := c.call(ctx, http.MethodGet, "/api/heard?since="+strconv.FormatFloat(since, 'f', 3, 64), nil, &r)
	return r.Heard, r.Now, err
}

// Now is the service's clock, from which enrolling looks for what is said.
func (c *Client) Now(ctx context.Context) (float64, error) {
	var r struct {
		Now float64 `json:"now"`
	}
	err := c.call(ctx, http.MethodGet, "/api/now", nil, &r)
	return r.Now, err
}

// Profiles are the enrolled persons with their number of samples.
func (c *Client) Profiles(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	err := c.call(ctx, http.MethodGet, "/api/profiles", nil, &out)
	return out, err
}

// Assign enrolls the utterance id for person.
func (c *Client) Assign(ctx context.Context, id, person string) error {
	return c.call(ctx, http.MethodPost, "/api/assign", map[string]string{"id": id, "name": person}, nil)
}

// Delete deletes person's voice; a person without one is no error.
func (c *Client) Delete(ctx context.Context, person string) error {
	err := c.call(ctx, http.MethodPost, "/api/delete", map[string]string{"name": person}, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
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
