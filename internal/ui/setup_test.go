package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeSetup struct {
	who, skill, tool string
	args             map[string]any
}

func (f *fakeSetup) Call(_ context.Context, who, skill, tool string, args json.RawMessage) (SetupResult, error) {
	if skill != "rewe" || tool != "store_search" {
		return SetupResult{}, fmt.Errorf("%w: %s %s", ErrNoSetupTool, skill, tool)
	}
	f.who, f.skill, f.tool = who, skill, tool
	_ = json.Unmarshal(args, &f.args)
	return SetupResult{Text: `[{"id":"2000001","name":"REWE Lieferservice"}]`}, nil
}

// PG-11 Calling a setup tool: the page's values reach the skill's tool, the
// answer comes back; only admins; an unknown tool is 404.
func TestPG11_CallingASetupTool(t *testing.T) {
	f := &fakeSetup{}
	srv := httptest.NewServer(New(Options{Token: "secret", Setup: f}))
	t.Cleanup(srv.Close)
	admin := map[string]string{HeaderHAUser: "u1", HeaderHAAdmin: "1"}
	code, body := callAs(t, srv, "POST", "/v1/ui/skills/rewe/setup/store_search", `{"args":{"zip":"12345"}}`, admin)
	var res SetupResult
	if err := json.Unmarshal([]byte(body), &res); err != nil || code != http.StatusOK || res.IsError || res.Text == "" {
		t.Fatalf("%d %s", code, body)
	}
	if f.who != "u1" || f.args["zip"] != "12345" {
		t.Errorf("called as %q with %v", f.who, f.args)
	}
	if code, _ := callAs(t, srv, "POST", "/v1/ui/skills/rewe/setup/store_search", `{"args":{}}`,
		map[string]string{HeaderHAUser: "u2", HeaderHAAdmin: "0"}); code != http.StatusForbidden {
		t.Errorf("someone who is no admin: %d", code)
	}
	if code, _ := callAs(t, srv, "POST", "/v1/ui/skills/rewe/setup/order", `{"args":{}}`, admin); code != http.StatusNotFound {
		t.Errorf("not a setup tool: %d", code)
	}
}
