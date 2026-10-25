package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/miraassist/mira/internal/speakerid"
)

// fakeVoices is the speaker-id service: what it heard, and the samples.
type fakeVoices struct {
	mu      sync.Mutex
	heard   []speakerid.Heard
	samples map[string]int
	since   float64
}

func (f *fakeVoices) Profiles(context.Context) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.samples {
		out[k] = v
	}
	return out, nil
}
func (f *fakeVoices) Now(context.Context) (float64, error) { return 1000, nil }
func (f *fakeVoices) HeardSince(_ context.Context, since float64) ([]speakerid.Heard, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = since
	var out []speakerid.Heard
	for _, u := range f.heard {
		if u.At >= since {
			out = append(out, u)
		}
	}
	return out, 1001, nil
}
func (f *fakeVoices) Assign(_ context.Context, id, person string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples[person]++
	return nil
}
func (f *fakeVoices) Delete(_ context.Context, person string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.samples, person)
	return nil
}

// voiceServer has anna (HA user u-anna) and ben (no login), and a service
// that heard one sentence before enrolling and one after.
func voiceServer(t *testing.T) (*httptest.Server, *fakeVoices) {
	t.Helper()
	fv := &fakeVoices{samples: map[string]int{"ben": 2}, heard: []speakerid.Heard{
		{ID: "old", At: 900, Transcript: "Said before"},
		{ID: "new", At: 1000.5, Transcript: "Merk dir, ich mag Tee"},
	}}
	h := New(Options{Token: "secret", Voices: fv,
		People: func() []PersonInfo {
			return []PersonInfo{{Key: "anna", Name: "Anna", Entity: "person.anna"}, {Key: "ben", Name: "Ben", Entity: "person.ben"}}
		},
		PersonOfHAUser: func(_ context.Context, user string) string {
			if user == "u-anna" {
				return "anna"
			}
			return ""
		}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, fv
}

func TestMeSaysWhoLooks(t *testing.T) {
	srv, _ := voiceServer(t)
	code, body := asHA(t, srv, "GET", "/v1/ui/me", "u-anna", false, "")
	var me map[string]any
	json.Unmarshal([]byte(body), &me)
	if code != 200 || me["person"] != "anna" || me["name"] != "Anna" || me["admin"] != false || me["voice"] != true {
		t.Errorf("%d %s", code, body)
	}
}

// Anyone manages their own voice; only an admin someone else's, such as a
// child without a login.
func TestVoiceRights(t *testing.T) {
	srv, _ := voiceServer(t)
	for _, c := range []struct {
		user   string
		admin  bool
		path   string
		status int
	}{
		{"u-anna", false, "/v1/ui/voice/anna", 200},
		{"u-anna", false, "/v1/ui/voice/ben", 403},
		{"u-admin", true, "/v1/ui/voice/ben", 200},
		{"u-admin", true, "/v1/ui/voice/zoe", 404},
		{"u-anna", false, "/v1/ui/voice", 403}, // the list of everyone's voices
		{"u-admin", true, "/v1/ui/voice", 200},
	} {
		if code, body := asHA(t, srv, "GET", c.path, c.user, c.admin, ""); code != c.status {
			t.Errorf("%s %s: %d %s, want %d", c.user, c.path, code, body, c.status)
		}
	}
}

// Enrolling shows only what was said after it started, and only that can be
// enrolled.
func TestEnrollShowsOnlyWhatFollows(t *testing.T) {
	srv, fv := voiceServer(t)
	if code, _ := asHA(t, srv, "GET", "/v1/ui/voice/anna/heard", "u-anna", false, ""); code != http.StatusConflict {
		t.Fatalf("heard before enrolling: %d", code)
	}
	if code, body := asHA(t, srv, "POST", "/v1/ui/voice/anna/enroll", "u-anna", false, ""); code != 200 {
		t.Fatalf("enroll: %d %s", code, body)
	}
	_, body := asHA(t, srv, "GET", "/v1/ui/voice/anna/heard", "u-anna", false, "")
	if strings.Contains(body, "Said before") || !strings.Contains(body, "Merk dir") || fv.since != 1000 {
		t.Errorf("heard: %s (since %v)", body, fv.since)
	}
	if code, _ := asHA(t, srv, "POST", "/v1/ui/voice/anna/assign", "u-anna", false, `{"id": "old"}`); code != http.StatusNotFound {
		t.Errorf("assigning what was said before: %d", code)
	}
	if code, _ := asHA(t, srv, "POST", "/v1/ui/voice/anna/assign", "u-anna", false, `{"id": "new"}`); code != 200 || fv.samples["anna"] != 1 {
		t.Errorf("assign: %d, samples %v", code, fv.samples)
	}
	asHA(t, srv, "DELETE", "/v1/ui/voice/anna/enroll", "u-anna", false, "")
	if code, _ := asHA(t, srv, "GET", "/v1/ui/voice/anna/heard", "u-anna", false, ""); code != http.StatusConflict {
		t.Errorf("heard after stopping: %d", code)
	}
}

func TestAdminDeletesAVoice(t *testing.T) {
	srv, fv := voiceServer(t)
	if code, _ := asHA(t, srv, "DELETE", "/v1/ui/voice/ben", "u-anna", false, ""); code != 403 || fv.samples["ben"] != 2 {
		t.Errorf("anna deleted ben's voice: %d", code)
	}
	if code, _ := asHA(t, srv, "DELETE", "/v1/ui/voice/ben", "u-admin", true, ""); code != 200 || fv.samples["ben"] != 0 {
		t.Errorf("admin: %d %v", code, fv.samples)
	}
}
