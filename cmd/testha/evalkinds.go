package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The steps that put a waiting message of a kind (docs/spec/evals.md,
// Messages and their kinds): each says nothing and waits for its signal.

// StepMessage is a text for one of Mira's mailboxes, as the integration
// posts it (kind simple).
type StepMessage struct {
	Text     string `yaml:"text"`
	Receiver string `yaml:"receiver"`
}

// StepDevice is a device the fake discovery adds, as a maker's app would.
type StepDevice struct {
	Name         string `yaml:"name"`
	Manufacturer string `yaml:"manufacturer"`
	Model        string `yaml:"model"`
}

// kindWait is how long a step waits for its signal: a safety net only.
const kindWait = 2 * time.Minute

var (
	foundSaid   = regexp.MustCompile(`msg="found devices said".* count=(\d+)`)
	machineKept = regexp.MustCompile(`msg="answering machine thread kept".* recordings=(\d+)`)
)

// postMessage posts a text to the eval service's /v1/messages, as the
// integration does for Mira's mailboxes; its answer says it is kept.
func (r *evalRun) postMessage(ctx context.Context, m StepMessage) error {
	receiver := m.Receiver
	if receiver == "" {
		receiver = "house"
	}
	return r.agent(ctx, "POST", "/v1/messages", map[string]any{"text": m.Text, "receiver": receiver, "importance": 2}, nil)
}

// addDevices adds the devices with the fake discovery and waits until the
// found devices' log lines have counted them all (the gathering takes 60 s
// after the last one). The ids go to removed, for the cleanup.
func (r *evalRun) addDevices(ctx context.Context, devices []StepDevice, added *[]string) error {
	from, err := logSize(r.agentLog)
	if err != nil {
		return err
	}
	for _, d := range devices {
		var out struct {
			Response struct {
				DeviceID string `json:"device_id"`
			} `json:"service_response"`
		}
		body := map[string]any{"name": d.Name}
		if d.Manufacturer != "" {
			body["manufacturer"] = d.Manufacturer
		}
		if d.Model != "" {
			body["model"] = d.Model
		}
		if err := r.ha(ctx, "POST", "/api/services/found_fake/add_device?return_response", body, &out); err != nil {
			return fmt.Errorf("found_fake.add_device %q: %w", d.Name, err)
		}
		*added = append(*added, out.Response.DeviceID)
	}
	seen := 0
	return waitLog(ctx, r.agentLog, from, kindWait, func(line string) (bool, error) {
		if m := foundSaid.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			seen += n
		}
		return seen >= len(devices), nil
	})
}

// addRecordings puts new recordings into the fake answering machine at once
// and waits for the line that the answering machine's thread kept them.
func (r *evalRun) addRecordings(ctx context.Context, names []string) error {
	from, err := logSize(r.agentLog)
	if err != nil {
		return err
	}
	if err := r.ha(ctx, "POST", "/api/services/mira_fritz/fake_add", map[string]any{"recordings": names}, nil); err != nil {
		return fmt.Errorf("mira_fritz.fake_add %s: %w", strings.Join(names, ", "), err)
	}
	return waitLog(ctx, r.agentLog, from, kindWait, func(line string) (bool, error) {
		m := machineKept.FindStringSubmatch(line)
		if m == nil {
			return false, nil
		}
		n, _ := strconv.Atoi(m[1])
		return n >= len(names), nil
	})
}

// cleanKinds empties the fake answering machine and removes the run's found
// devices, so the next run finds them anew.
func (r *evalRun) cleanKinds(ctx context.Context, recordings bool, devices []string) error {
	var errs []string
	if recordings {
		if err := r.ha(ctx, "POST", "/api/services/mira_fritz/fake_reset", map[string]any{}, nil); err != nil {
			errs = append(errs, "mira_fritz.fake_reset: "+err.Error())
		}
	}
	for _, id := range devices {
		if err := r.ha(ctx, "POST", "/api/services/found_fake/remove_device", map[string]any{"device_id": id}, nil); err != nil {
			errs = append(errs, fmt.Sprintf("found_fake.remove_device %s: %v", id, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
