package ui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// The running plans, with the room's name instead of its id; anyone with
// the token may read them.
func TestPlans(t *testing.T) {
	h := New(Options{Token: "secret",
		AreaName: func(id string) string { return map[string]string{"kueche": "Küche"}[id] },
		Plans: func(context.Context) ([]Plan, error) {
			return []Plan{{ID: "1", Target: "kochen", Request: "Pfannkuchen", Place: "area:kueche", Plan: "- [x] a\n- [ ] b", Done: 1, Steps: 2},
				{ID: "2", Target: "kochen", Place: "telegram:42"}}, nil
		}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, body := call(t, srv, "GET", "/v1/ui/plans", "secret", "")
	if code != 200 || !strings.Contains(body, `"place":"Küche"`) || !strings.Contains(body, `"done":1`) || strings.Contains(body, "telegram") {
		t.Errorf("plans: %d %s", code, body)
	}
	if code, _ := call(t, srv, "GET", "/v1/ui/plans", "", ""); code != 401 {
		t.Errorf("without token: %d", code)
	}
	empty := httptest.NewServer(New(Options{Token: "secret"}))
	defer empty.Close()
	if code, body := call(t, empty, "GET", "/v1/ui/plans", "secret", ""); code != 200 || !strings.Contains(body, `"plans":[]`) {
		t.Errorf("no plans: %d %s", code, body)
	}
}
