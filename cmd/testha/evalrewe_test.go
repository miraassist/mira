package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The skill rewe in an eval run gets the run's korb and settings; the test
// config's own entry (the real account) never reaches the run.
func TestEvalRewePointsTheSkillAtTheFake(t *testing.T) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(`
skills:
  dirs: [skills]
  rewe: {programm: /config/rewe/korb, konto: /config/rewe/konto, limit: 300}
`), &cfg); err != nil {
		t.Fatal(err)
	}
	withRewe(cfg, &reweEnv{err: exec.ErrNotFound})
	if _, ok := cfg["skills"].(map[string]any)["rewe"]; ok {
		t.Fatal("the test config's rewe stays in a run without korb")
	}

	e := &reweEnv{korb: "/opt/korb's/korb", dir: "/tmp/run/rewe", konto: "/tmp/run/rewe/konto"}
	withRewe(cfg, e)
	got := cfg["skills"].(map[string]any)["rewe"].(map[string]any)
	if got["programm"] != "/tmp/run/rewe/korb" || got["konto"] != "/tmp/run/rewe/konto" || got["liste"] != reweListName {
		t.Errorf("settings %v", got)
	}
	script := e.wrapperScript()
	for _, want := range []string{"KORB_API_URL='http://127.0.0.1:8792/api'", "KORB_AUTH_URL='http://127.0.0.1:8792'", `exec '/opt/korb'\''s/korb' "$@"`} {
		if !strings.Contains(script, want) {
			t.Errorf("wrapper lacks %s:\n%s", want, script)
		}
	}
}

// The runner's own korb calls never take the caller's KORB_* variables.
func TestEvalReweKorbNeverSeesTheRealAccount(t *testing.T) {
	t.Setenv("KORB_CONFIG_DIR", "/home/someone/.config/korb")
	t.Setenv("KORB_API_URL", "https://mobile-clients-api.rewe.de/api")
	t.Setenv("KORB_CERT_DIR", "/home/someone/certs")
	e := &reweEnv{konto: "/tmp/run/rewe/konto"}
	env := strings.Join(e.korbEnv(), "\n")
	for _, bad := range []string{"/home/someone", "rewe.de"} {
		if strings.Contains(env, bad) {
			t.Errorf("korb's environment has %s:\n%s", bad, env)
		}
	}
	if !strings.Contains(env, "KORB_CONFIG_DIR=/tmp/run/rewe/konto") || !strings.Contains(env, "KORB_API_URL=http://127.0.0.1:8792/api") {
		t.Errorf("korb's environment:\n%s", env)
	}
}

// Against korb-fake (when rewe-cli's binaries are in PATH): a fresh fake per
// run, the login, an open order, and the checks see basket and orders.
func TestEvalReweSetsUpTheFake(t *testing.T) {
	e := newReweEnv(t.TempDir())
	if e.err != nil {
		t.Skip(e.err)
	}
	t.Cleanup(e.stop)
	ctx := context.Background()
	one := 1
	if err := e.start(ctx, &SetupRewe{OpenOrder: []ReweItem{{Search: "Kaffee", Qty: 7}}}); err != nil {
		t.Fatal(err)
	}
	if f := e.checkOrders(ctx, ReweOrderCheck{Count: &one, Contains: "Kaffee"}); f != "" {
		t.Error(f)
	}
	if f := e.checkOrders(ctx, ReweOrderCheck{Contains: "Butter"}); !strings.Contains(f, "7× Tchibo") {
		t.Errorf("a missing product: %q", f)
	}
	if f := e.checkBasket(ctx, ReweBasketCheck{Empty: true}); f != "" {
		t.Error(f)
	}
	if err := e.add(ctx, ReweItem{Search: "Butter"}); err != nil {
		t.Fatal(err)
	}
	if f := e.checkBasket(ctx, ReweBasketCheck{Contains: "Kerrygold"}); f != "" {
		t.Error(f)
	}
	if f := e.checkBasket(ctx, ReweBasketCheck{Contains: "Kerrygold", Not: true}); f == "" {
		t.Error("Not passed with the product in the basket")
	}

	// The next run starts fresh: no order, no basket; without a login.
	no := false
	zero := 0
	if err := e.start(ctx, &SetupRewe{LoggedIn: &no}); err != nil {
		t.Fatal(err)
	}
	if out, err := e.run(ctx, "login", "status"); err != nil || !strings.Contains(string(out), `"loggedIn":false`) {
		t.Errorf("login status %s %v", out, err)
	}
	if err := e.start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f := e.checkOrders(ctx, ReweOrderCheck{Count: &zero}); f != "" {
		t.Error(f)
	}
	if f := e.checkBasket(ctx, ReweBasketCheck{Empty: true}); f != "" {
		t.Error(f)
	}
}
