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
	return New(srv.URL + "/"), &asked
}

func TestPerson(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
	}{
		{"accepted", `{"person": "anna", "score": 0.71, "decision": "accepted", "count": 1}`, "anna"},
		{"uncertain", `{"person": null, "score": 0.62, "decision": "uncertain", "count": 2}`, ""},
		{"unknown", `{"person": null, "score": 0.2, "decision": "unknown", "count": 1}`, ""},
		{"not found", `null`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, asked := serve(t, c.body, 0)
			got, _, err := client.Person(context.Background(), "Mach das Licht an")
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
			if *asked != "Mach das Licht an" {
				t.Errorf("asked for %q", *asked)
			}
		})
	}
}

// A service that does not answer in time costs the turn the timeout and no
// more.
func TestPersonTimesOut(t *testing.T) {
	client, _ := serve(t, `{"person": "anna", "decision": "accepted"}`, 2*time.Second)
	start := time.Now()
	got, _, err := client.Person(context.Background(), "Hallo")
	if err == nil || got != "" {
		t.Fatalf("got %q, %v; want an error and nobody", got, err)
	}
	if took := time.Since(start); took > Timeout+200*time.Millisecond {
		t.Errorf("took %v", took)
	}
}

// Without a URL there is no client, and asking it is nobody.
func TestNoService(t *testing.T) {
	var c *Client = New("  ")
	if c != nil {
		t.Fatal("a client without a URL")
	}
	if got, _, err := c.Person(context.Background(), "Hallo"); got != "" || err != nil {
		t.Errorf("got %q, %v", got, err)
	}
}
