package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// REWE in the evals (docs/spec/evals.md, REWE shopping): the shipped skill
// rewe with korb against korb-fake, a fresh fake per scenario run. korb and
// korb-fake come from rewe-cli, which is not part of this repository; the
// run takes them from PATH. korb never sees the real account: every call
// the runner makes, and the skill's korb through a wrapper, points at the
// fake on this machine, with a config folder of the run's own.

const (
	// reweFakeAddr is where korb-fake listens during an eval run.
	reweFakeAddr = "127.0.0.1:8792"
	// reweList is the shopping list of the REWE scenarios, a list of its
	// own in the test HA (cmd/testha/lists.go), so the scenarios do not
	// meet the examples on Einkauf.
	reweList     = "todo.wocheneinkauf"
	reweListName = "Wocheneinkauf"
	reweLimit    = 150
	reweZIP      = "12345"
)

// SetupRewe is what a REWE scenario starts with, besides its shopping list
// (setup.todo).
type SetupRewe struct {
	// LoggedIn false: korb has no login (RW-18). Default true.
	LoggedIn *bool `yaml:"logged_in"`
	// OpenOrder is an order placed before the scenario, open and still
	// changeable (RW-16, RW-17); the products must reach the minimum order
	// value.
	OpenOrder []ReweItem `yaml:"open_order"`
}

// ReweItem is a product by a search word (the first hit) and a quantity.
type ReweItem struct {
	Search string `yaml:"search"`
	Qty    int    `yaml:"qty"`
}

// ReweBasketCheck checks korb's basket (a running modification's basket
// while there is one): a product whose data contains Contains, or none
// with Not; Empty wants no product at all.
type ReweBasketCheck struct {
	Contains string `yaml:"contains"`
	Not      bool   `yaml:"not"`
	Empty    bool   `yaml:"empty"`
}

// ReweOrderCheck checks the open orders: how many there are, and that one
// of them has a product whose data contains Contains.
type ReweOrderCheck struct {
	Count    *int   `yaml:"count"`
	Contains string `yaml:"contains"`
}

// usesRewe: the scenario needs the skill rewe.
func (sc Scenario) usesRewe() bool {
	return slices.Contains(sc.Skills, "rewe")
}

// reweEnv is the run's korb setup: the binaries, the folder of the config
// (the skill's konto), the fake's process. err says why REWE scenarios
// cannot run (a binary missing); they fail their setup with it.
type reweEnv struct {
	korb, fake string
	dir        string // the run's folder: wrapper, konto
	konto      string
	marketID   string
	proc       *exec.Cmd
	err        error
}

// newReweEnv finds korb and korb-fake and writes the wrapper the skill
// starts as its programm.
func newReweEnv(tmp string) *reweEnv {
	e := &reweEnv{dir: filepath.Join(tmp, "rewe")}
	var missing []string
	for _, b := range []struct {
		name string
		to   *string
	}{{"korb", &e.korb}, {"korb-fake", &e.fake}} {
		p, err := exec.LookPath(b.name)
		if err != nil {
			missing = append(missing, b.name)
			continue
		}
		*b.to, _ = filepath.Abs(p)
	}
	if len(missing) > 0 {
		e.err = fmt.Errorf("%s not in PATH (rewe-cli, github.com/miraassist/rewe-cli: its releases, or go install ./cmd/korb ./cmd/korb-fake)", strings.Join(missing, " and "))
		return e
	}
	e.konto = filepath.Join(e.dir, "konto")
	if err := os.MkdirAll(e.konto, 0o700); err != nil {
		e.err = err
		return e
	}
	if err := os.WriteFile(e.wrapper(), []byte(e.wrapperScript()), 0o700); err != nil {
		e.err = err
	}
	return e
}

func (e *reweEnv) wrapper() string { return filepath.Join(e.dir, "korb") }

// wrapperScript points the skill's korb at the fake: Mira gives a stdio
// server only a few variables (StdioEnv), and the skill's own env has no
// address, so the address goes in here.
func (e *reweEnv) wrapperScript() string {
	return fmt.Sprintf("#!/bin/sh\nexport KORB_API_URL=%s KORB_AUTH_URL=%s\nexec %s \"$@\"\n",
		shellQuote("http://"+reweFakeAddr+"/api"), shellQuote("http://"+reweFakeAddr), shellQuote(e.korb))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// settings are the skill's settings in the eval service's config.
func (e *reweEnv) settings() map[string]any {
	return map[string]any{"programm": e.wrapper(), "konto": e.konto, "limit": reweLimit, "liste": reweListName}
}

// withRewe switches the skill rewe on with the run's korb, when a scenario
// needs it and korb is there; a test config's own entry for rewe (the real
// account) never reaches an eval run.
func withRewe(cfg map[string]any, e *reweEnv) {
	skills, _ := cfg["skills"].(map[string]any)
	if skills != nil {
		delete(skills, "rewe")
	}
	if e == nil || e.err != nil {
		return
	}
	if skills == nil {
		skills = map[string]any{}
		cfg["skills"] = skills
	}
	skills["rewe"] = e.settings()
}

// start runs a fresh korb-fake (the one before ends) and makes korb's
// config: the delivery service and, unless the scenario says otherwise,
// the login; then the open order the scenario starts with.
func (e *reweEnv) start(ctx context.Context, s *SetupRewe) error {
	if e.err != nil {
		return e.err
	}
	e.stop()
	entries, _ := os.ReadDir(e.konto)
	for _, en := range entries {
		if err := os.RemoveAll(filepath.Join(e.konto, en.Name())); err != nil {
			return err
		}
	}
	cmd := exec.Command(e.fake, "-addr", reweFakeAddr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("korb-fake: %w", err)
	}
	e.proc = cmd
	id, err := fakeMarket(out)
	if err != nil {
		e.stop()
		return fmt.Errorf("korb-fake on %s: %w", reweFakeAddr, err)
	}
	e.marketID = id
	go func() { _, _ = bufio.NewReader(out).WriteTo(discard{}) }()
	if _, err := e.run(ctx, "store", "set", e.marketID, reweZIP); err != nil {
		return err
	}
	if s != nil && s.LoggedIn != nil && !*s.LoggedIn {
		return nil
	}
	if _, err := e.run(ctx, "login"); err != nil {
		return err
	}
	if s == nil || len(s.OpenOrder) == 0 {
		return nil
	}
	for _, it := range s.OpenOrder {
		if err := e.add(ctx, it); err != nil {
			return err
		}
	}
	raw, err := e.run(ctx, "timeslots")
	if err != nil {
		return err
	}
	var slots struct {
		Slots []struct {
			ID string `json:"id"`
		} `json:"getTimeslotsCheckout"`
	}
	if err := json.Unmarshal(raw, &slots); err != nil || len(slots.Slots) == 0 {
		return fmt.Errorf("korb-fake has no slot for the open order")
	}
	// The first slot is tomorrow or later: changeable for 12 hours before.
	if _, err := e.run(ctx, "checkout", "create", slots.Slots[len(slots.Slots)/2].ID); err != nil {
		return err
	}
	_, err = e.run(ctx, "checkout", "order")
	return err
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

var fakeStoreLine = regexp.MustCompile(`korb store set (\S+) `)

// fakeMarket reads korb-fake's banner until it names the delivery service.
func fakeMarket(out interface{ Read([]byte) (int, error) }) (string, error) {
	sc := bufio.NewScanner(out)
	found := make(chan string, 1)
	go func() {
		for sc.Scan() {
			if m := fakeStoreLine.FindStringSubmatch(sc.Text()); m != nil {
				found <- m[1]
				return
			}
		}
		close(found)
	}()
	select {
	case id, ok := <-found:
		if !ok {
			return "", errors.New("it ended without its banner (port taken?)")
		}
		return id, nil
	case <-time.After(10 * time.Second):
		return "", errors.New("no banner within 10 s")
	}
}

func (e *reweEnv) stop() {
	if e.proc == nil {
		return
	}
	_ = e.proc.Process.Kill()
	_ = e.proc.Wait()
	e.proc = nil
}

// add puts the first hit of a search into the basket.
func (e *reweEnv) add(ctx context.Context, it ReweItem) error {
	raw, err := e.run(ctx, "search", it.Search)
	if err != nil {
		return err
	}
	var hits []struct {
		Listing struct {
			ID string `json:"listingId"`
		} `json:"listing"`
	}
	if err := json.Unmarshal(raw, &hits); err != nil || len(hits) == 0 {
		return fmt.Errorf("korb-fake has no product for %q", it.Search)
	}
	qty := it.Qty
	if qty == 0 {
		qty = 1
	}
	_, err = e.run(ctx, "basket", "add", hits[0].Listing.ID, "--qty", fmt.Sprint(qty))
	return err
}

// run calls korb against the fake, with an environment of its own: never
// the caller's KORB_* variables, so never the real account.
func (e *reweEnv) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.korb, args...)
	cmd.Env = e.korbEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("korb %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (e *reweEnv) korbEnv() []string {
	env := []string{
		"KORB_API_URL=http://" + reweFakeAddr + "/api",
		"KORB_AUTH_URL=http://" + reweFakeAddr,
		"KORB_CONFIG_DIR=" + e.konto,
		"KORB_CERT_DIR=",
	}
	for _, k := range []string{"PATH", "HOME", "TZ"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// checkBasket checks korb's basket against the fake.
func (e *reweEnv) checkBasket(ctx context.Context, c ReweBasketCheck) string {
	if e.err != nil {
		return e.err.Error()
	}
	raw, err := e.run(ctx, "basket")
	if err != nil {
		return err.Error()
	}
	var b struct {
		Items []json.RawMessage `json:"lineItems"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return "korb basket: " + err.Error()
	}
	titles := itemTitles(b.Items)
	if c.Empty {
		if len(b.Items) > 0 {
			return fmt.Sprintf("the basket is not empty: %s", strings.Join(titles, ", "))
		}
		return ""
	}
	found := slices.ContainsFunc(b.Items, func(it json.RawMessage) bool { return containsFold(string(it), c.Contains) })
	switch {
	case found && c.Not:
		return fmt.Sprintf("the basket has %q: %s", c.Contains, strings.Join(titles, ", "))
	case !found && !c.Not:
		return fmt.Sprintf("the basket has no %q: %s", c.Contains, strings.Join(titles, ", "))
	}
	return ""
}

// checkOrders checks the open orders at the fake.
func (e *reweEnv) checkOrders(ctx context.Context, c ReweOrderCheck) string {
	if e.err != nil {
		return e.err.Error()
	}
	raw, err := e.run(ctx, "orders")
	if err != nil {
		return err.Error()
	}
	var orders []struct {
		ID string `json:"orderId"`
	}
	if err := json.Unmarshal(raw, &orders); err != nil {
		return "korb orders: " + err.Error()
	}
	if c.Count != nil && len(orders) != *c.Count {
		return fmt.Sprintf("%d open orders, not %d", len(orders), *c.Count)
	}
	if c.Contains == "" {
		return ""
	}
	var titles []string
	for _, o := range orders {
		raw, err := e.run(ctx, "orders", "get", o.ID)
		if err != nil {
			return err.Error()
		}
		var d struct {
			Sub []struct {
				Items []json.RawMessage `json:"lineItems"`
			} `json:"subOrders"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return "korb orders get: " + err.Error()
		}
		for _, s := range d.Sub {
			if slices.ContainsFunc(s.Items, func(it json.RawMessage) bool { return containsFold(string(it), c.Contains) }) {
				return ""
			}
			titles = append(titles, itemTitles(s.Items)...)
		}
	}
	return fmt.Sprintf("no open order has %q (%s)", c.Contains, strings.Join(titles, ", "))
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// itemTitles are the titles of line items, for a failure's message.
func itemTitles(items []json.RawMessage) []string {
	var out []string
	for _, it := range items {
		var v struct {
			Title   string `json:"title"`
			Qty     int    `json:"quantity"`
			Product struct {
				Title string `json:"title"`
			} `json:"product"`
		}
		_ = json.Unmarshal(it, &v)
		t := v.Title
		if t == "" {
			t = v.Product.Title
		}
		out = append(out, fmt.Sprintf("%d× %s", v.Qty, t))
	}
	return out
}
