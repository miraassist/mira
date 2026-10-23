package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The catalog is for admins; the page's scripts are files without data.
func TestCatalogIsForAdminsAndTheScriptsAreServed(t *testing.T) {
	srv, _, _ := setup(t)
	code, body := asHA(t, srv, "GET", "/v1/ui/catalog", "u-admin", true, "")
	var c Catalog
	if code != 200 || json.Unmarshal([]byte(body), &c) != nil || c.Skills == nil || c.Agents == nil {
		t.Errorf("admin: %d %s", code, body)
	}
	if code, _ := asHA(t, srv, "GET", "/v1/ui/catalog", "u-anna", false, ""); code != http.StatusForbidden {
		t.Errorf("not an admin: %d", code)
	}
	for _, name := range []string{"lit-core.min.js", "app.js"} {
		resp, err := http.Get(srv.URL + "/ui/js/" + name)
		if err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
			t.Errorf("%s: %v %v", name, resp, err)
		}
	}
	if resp, _ := http.Get(srv.URL + "/ui/js/nothing.js"); resp.StatusCode != 404 {
		t.Errorf("unknown script: %d", resp.StatusCode)
	}
}

// "Check now" asks a model and says whether it answered; for admins only.
func TestCheckModel(t *testing.T) {
	srv, _, st := setup(t)
	_ = st
	h := New(Options{Token: "secret", Store: st, CheckModel: func(_ context.Context, name string) CheckResult {
		if name == "main" {
			return CheckResult{OK: true, MS: 800, Answer: "OK"}
		}
		return CheckResult{MS: 3, Error: "llm server returned 401: API key not valid"}
	}})
	s2 := httptest.NewServer(h)
	t.Cleanup(s2.Close)
	if code, body := asHA(t, s2, "POST", "/v1/ui/models/main/check", "u-admin", true, ""); code != 200 || !strings.Contains(body, `"ok":true`) || !strings.Contains(body, `"answer":"OK"`) {
		t.Errorf("main: %d %s", code, body)
	}
	if code, body := asHA(t, s2, "POST", "/v1/ui/models/lokal/check", "u-admin", true, ""); code != 200 || !strings.Contains(body, "401: API key not valid") {
		t.Errorf("lokal: %d %s", code, body)
	}
	if code, _ := asHA(t, s2, "POST", "/v1/ui/models/main/check", "u-anna", false, ""); code != http.StatusForbidden {
		t.Errorf("not an admin: %d", code)
	}
	_ = srv
}
