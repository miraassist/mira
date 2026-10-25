package speakerid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// serve answers /api/match with body and records the text asked for.
func serve(t *testing.T, body string, delay time.Duration) (*Client, *string) {
	t.Helper()
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/match" {
			http.NotFound(w, r)
			return
		}
		asked = r.URL.Query().Get("text")
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", ""), &asked
}

func TestMatch(t *testing.T) {
	client, asked := serve(t, `[{"person": "anna", "score": 0.81, "second": "ben", "second_score": 0.4, "seconds": 2.1}]`, 0)
	found, err := client.Match(context.Background(), "Mach das Licht an")
	if err != nil || len(found) != 1 || found[0] != (Utterance{"anna", 0.81, "ben", 0.4}) {
		t.Fatalf("got %+v, %v", found, err)
	}
	if *asked != "Mach das Licht an" {
		t.Errorf("asked for %q", *asked)
	}
}

func TestDecide(t *testing.T) {
	at := func(p string) float64 {
		if p == "ben" {
			return 0.6 // a voice of his own threshold
		}
		return Threshold
	}
	anna := Utterance{Person: "anna", Score: 0.8, Second: "ben", SecondScore: 0.3}
	for _, c := range []struct {
		name  string
		found []Utterance
		want  string
	}{
		{"one satellite", []Utterance{anna}, "anna"},
		{"nothing heard", nil, ""},
		{"below the threshold", []Utterance{{Person: "anna", Score: 0.65}}, ""},
		{"a person's own threshold", []Utterance{{Person: "ben", Score: 0.65}}, "ben"},
		{"too close to the second", []Utterance{{Person: "anna", Score: 0.8, Second: "ben", SecondScore: 0.77}}, ""},
		{"two satellites, same person", []Utterance{anna, {Person: "anna", Score: 0.75}}, "anna"},
		{"two satellites disagree", []Utterance{anna, {Person: "ben", Score: 0.9}}, ""},
		{"one of two too low", []Utterance{anna, {Person: "anna", Score: 0.5}}, ""},
		{"no profile at all", []Utterance{{Person: "", Score: 0}}, ""},
	} {
		if got := Decide(c.found, at); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A service that does not answer in time costs the turn the timeout and no
// more.
func TestMatchTimesOut(t *testing.T) {
	client, _ := serve(t, `[]`, 2*time.Second)
	start := time.Now()
	if _, err := client.Match(context.Background(), "Hallo"); err == nil {
		t.Fatal("no error")
	}
	if took := time.Since(start); took > Timeout+200*time.Millisecond {
		t.Errorf("took %v", took)
	}
}

// Without a URL there is no client, and asking it finds nothing.
func TestNoService(t *testing.T) {
	c := New("  ", "")
	if c != nil {
		t.Fatal("a client without a URL")
	}
	if found, err := c.Match(context.Background(), "Hallo"); found != nil || err != nil {
		t.Errorf("got %v, %v", found, err)
	}
}
