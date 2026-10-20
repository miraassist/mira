package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// The TECHO5 contract is what a TECHO5 device offers Home Assistant: its
// ESPHome actions with their fields, and its entities. It is recorded from a
// real device into testenv/techo5_fake/contract/<kind>.json and is what the
// fake devices (techo5_fake) offer; `testha contract` compares a real device
// with it, so a new firmware that changes the interface is noticed.
//
//	go run ./cmd/testha contract          compare the real devices with the contract
//	go run ./cmd/testha contract record   record it anew from the real devices
type contract struct {
	Model    string                     `json:"model"`
	Services map[string]contractService `json:"services"`
	// Entities are "domain.suffix", the entity id without the node prefix.
	Entities []string `json:"entities"`
}

type contractService struct {
	Fields   []string `json:"fields"`
	Response bool     `json:"response,omitempty"`
}

// techo5Device is a real TECHO5 device found in Home Assistant.
type techo5Device struct {
	node, kind string
	contract   contract
}

// realTECHO5 finds the TECHO5 devices in Home Assistant (not the fakes) and
// reads what they offer. A device is a TECHO5 one when its ESPHome actions
// have <node>_alarms_list; it is a Show when its model says so.
func realTECHO5(ctx context.Context, admin *haws.Client, token string) ([]techo5Device, error) {
	var services []struct {
		Domain   string `json:"domain"`
		Services map[string]struct {
			Fields   map[string]json.RawMessage `json:"fields"`
			Response json.RawMessage            `json:"response"`
		} `json:"services"`
	}
	if err := api(ctx, "GET", "/api/services", token, nil, &services); err != nil {
		return nil, err
	}
	esphome := map[string]contractService{}
	for _, d := range services {
		if d.Domain != "esphome" {
			continue
		}
		for name, s := range d.Services {
			fields := make([]string, 0, len(s.Fields))
			for f := range s.Fields {
				fields = append(fields, f)
			}
			sort.Strings(fields)
			esphome[name] = contractService{Fields: fields, Response: len(s.Response) > 0 && string(s.Response) != "null"}
		}
	}
	devices, err := admin.Devices(ctx)
	if err != nil {
		return nil, err
	}
	entities, err := admin.Entities(ctx)
	if err != nil {
		return nil, err
	}
	var out []techo5Device
	for _, dev := range devices {
		node := slugName(dev.Name)
		if node == "" || strings.HasPrefix(node, "fake_") {
			continue
		}
		if _, ok := esphome[node+"_alarms_list"]; !ok {
			continue
		}
		c := contract{Model: dev.Model, Services: map[string]contractService{}}
		for name, s := range esphome {
			if suffix, ok := strings.CutPrefix(name, node+"_"); ok {
				c.Services[suffix] = s
			}
		}
		// TECHO5 spreads its entities over sub-devices of the same model
		// ("Show microphone", "Show playback"); their ids still start with
		// the node.
		own := map[string]bool{}
		for _, sub := range devices {
			if sub.Model == dev.Model && (slugName(sub.Name) == node || strings.HasPrefix(slugName(sub.Name), node+"_")) {
				own[sub.ID] = true
			}
		}
		for _, e := range entities {
			if !own[e.DeviceID] {
				continue
			}
			domain, object, _ := strings.Cut(e.EntityID, ".")
			if suffix, ok := strings.CutPrefix(object, node+"_"); ok {
				c.Entities = append(c.Entities, domain+"."+suffix)
			}
		}
		sort.Strings(c.Entities)
		kind := "echo"
		if strings.Contains(dev.Model, "Show") {
			kind = "show"
		}
		out = append(out, techo5Device{node: node, kind: kind, contract: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].node < out[j].node })
	return out, nil
}

// slugName is how ESPHome names a node in its actions (cmd/mira's slug).
func slugName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func contractPath(dir, kind string) string {
	return filepath.Join(dir, "techo5_fake", "contract", kind+".json")
}

func readContract(dir, kind string) (contract, error) {
	var c contract
	raw, err := os.ReadFile(contractPath(dir, kind))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

// compareContract lists what a real device has that the contract has not,
// and the other way round.
func compareContract(want, got contract) []string {
	var out []string
	for name, w := range want.Services {
		g, ok := got.Services[name]
		switch {
		case !ok:
			out = append(out, "action gone: "+name)
		case !slices.Equal(w.Fields, g.Fields):
			out = append(out, fmt.Sprintf("action %s: fields %v, contract %v", name, g.Fields, w.Fields))
		case w.Response != g.Response:
			out = append(out, fmt.Sprintf("action %s: response %v, contract %v", name, g.Response, w.Response))
		}
	}
	for name := range got.Services {
		if _, ok := want.Services[name]; !ok {
			out = append(out, "action new: "+name)
		}
	}
	for _, e := range want.Entities {
		if !slices.Contains(got.Entities, e) {
			out = append(out, "entity gone: "+e)
		}
	}
	for _, e := range got.Entities {
		if !slices.Contains(want.Entities, e) {
			out = append(out, "entity new: "+e)
		}
	}
	sort.Strings(out)
	return out
}

func contractCmd(ctx context.Context, dir string, args []string) error {
	e := loadEnv(dir)
	admin, stop, err := connect(ctx, e["TESTHA_ADMIN_TOKEN"])
	if err != nil {
		return fmt.Errorf("no admin token; run testha up first: %w", err)
	}
	defer stop()
	found, err := realTECHO5(ctx, admin, e["TESTHA_ADMIN_TOKEN"])
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("no real TECHO5 device in the test Home Assistant")
	}
	record := len(args) > 0 && args[0] == "record"
	recorded := map[string]bool{}
	differ := false
	for _, d := range found {
		if record {
			if recorded[d.kind] {
				continue // the first device of a kind is the reference
			}
			raw, _ := json.MarshalIndent(d.contract, "", "  ")
			if err := os.MkdirAll(filepath.Dir(contractPath(dir, d.kind)), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(contractPath(dir, d.kind), append(raw, '\n'), 0o644); err != nil {
				return err
			}
			recorded[d.kind] = true
			fmt.Printf("recorded %s from %s (%s): %d actions, %d entities\n", d.kind, d.node, d.contract.Model, len(d.contract.Services), len(d.contract.Entities))
			continue
		}
		want, err := readContract(dir, d.kind)
		if err != nil {
			return fmt.Errorf("no %s contract yet; run testha contract record: %w", d.kind, err)
		}
		diff := compareContract(want, d.contract)
		fmt.Printf("%s (%s, %s): ", d.node, d.kind, d.contract.Model)
		if len(diff) == 0 {
			fmt.Println("matches the contract")
			continue
		}
		differ = true
		fmt.Printf("%d differences to the contract (%s)\n", len(diff), want.Model)
		for _, line := range diff {
			fmt.Println("  " + line)
		}
	}
	if !record {
		if err := checkFakes(ctx, admin, e["TESTHA_ADMIN_TOKEN"], dir); err != nil {
			return err
		}
	}
	if differ {
		return fmt.Errorf("the firmware differs from the fakes; check the differences, then testha contract record")
	}
	return nil
}

// checkFakes says whether the fake devices offer the contract's actions (they
// read it at start: after a record, Home Assistant needs a restart) and how
// many of the real device's entities they have.
func checkFakes(ctx context.Context, admin *haws.Client, token, dir string) error {
	var services []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err := api(ctx, "GET", "/api/services", token, nil, &services); err != nil {
		return err
	}
	entities, err := admin.Entities(ctx)
	if err != nil {
		return err
	}
	for _, f := range []struct{ node, kind string }{{"fake_show", "show"}, {"fake_echo", "echo"}} {
		want, err := readContract(dir, f.kind)
		if err != nil {
			return err
		}
		var missing []string
		for name := range want.Services {
			found := false
			for _, d := range services {
				if _, ok := d.Services[f.node+"_"+name]; ok && d.Domain == "esphome" {
					found = true
				}
			}
			if !found {
				missing = append(missing, name)
			}
		}
		have := 0
		for _, e := range entities {
			domain, object, _ := strings.Cut(e.EntityID, ".")
			if suffix, ok := strings.CutPrefix(object, f.node+"_"); ok && slices.Contains(want.Entities, domain+"."+suffix) {
				have++
			}
		}
		sort.Strings(missing)
		fmt.Printf("%s: %d of %d actions, %d of %d entities (the ones Mira uses)", f.node, len(want.Services)-len(missing), len(want.Services), have, len(want.Entities))
		if len(missing) > 0 {
			fmt.Printf("; missing actions %v (restart the test HA after a record)", missing)
		}
		fmt.Println()
	}
	return nil
}
