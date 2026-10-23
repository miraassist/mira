package ui

import (
	"encoding/json"
	"net/http"
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
