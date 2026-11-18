package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// threadNet is one of Home Assistant's Thread networks
// (docs/spec/device-onboarding.md, Matter over Thread, row T1). Its
// credentials are never kept here: they are read just before they go to the
// Matter server (decision 28).
type threadNet struct {
	ID   string `json:"dataset_id"`
	Name string `json:"network_name"`
	// Preferred is Home Assistant's preferred Thread network.
	Preferred bool `json:"preferred"`
}

// networks are what Mira can give the Matter server for one code
// (decisions 23 to 26).
type networks struct {
	// wifi: the skill has the home Wi-Fi's name and password.
	wifi bool
	// thread is the Thread network taken, nil for none.
	thread *threadNet
	// count is the number of Home Assistant's Thread networks; several and
	// none preferred means none is taken (decision 26).
	count int
}

// networks reads the Wi-Fi settings and Home Assistant's Thread networks and
// picks the Thread network: the preferred one, else the only one, else none
// (decisions 25 and 26). Home Assistant without the Thread integration has
// none (row T9). An error is Home Assistant's.
func (m *Onboarding) networks(ctx context.Context) (networks, error) {
	ssid, password := m.wifi()
	n := networks{wifi: ssid != "" && password != ""}
	raw, err := m.o.HA.Call(ctx, map[string]any{"type": "thread/list_datasets"})
	var haErr *haws.Error
	if errors.As(err, &haErr) && haErr.Code == "unknown_command" {
		m.log.Info("home assistant has no thread integration; no thread network", "error", err)
		return n, nil
	}
	if err != nil {
		return n, err
	}
	var answer struct {
		Datasets []threadNet `json:"datasets"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return n, fmt.Errorf("thread/list_datasets: %q not understood: %w", excerpt.Bytes(raw), err)
	}
	n.count = len(answer.Datasets)
	for i, d := range answer.Datasets {
		if d.Preferred {
			n.thread = &answer.Datasets[i]
			return n, nil
		}
	}
	switch {
	case n.count == 1:
		n.thread = &answer.Datasets[0]
	case n.count > 1:
		m.log.Info("no preferred thread network", "networks", n.count)
	}
	return n, nil
}

// giveThread reads the network's credentials and hands them to the Matter
// server (rows T2, T3). The credentials hold the network's key: they go into
// no log line and no answer (decision 28).
func (m *Onboarding) giveThread(ctx context.Context, net threadNet) error {
	raw, err := m.o.HA.Call(ctx, map[string]any{"type": "thread/get_dataset_tlv", "dataset_id": net.ID})
	if err != nil {
		m.log.Error("could not read the thread network's credentials", "network", net.Name, "error", err)
		return err
	}
	var answer struct {
		TLV string `json:"tlv"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || strings.TrimSpace(answer.TLV) == "" {
		// Only the answer's length: it would hold the key.
		err = fmt.Errorf("thread/get_dataset_tlv of %s: an answer of %d bytes without credentials", net.Name, len(raw))
		m.log.Error("could not read the thread network's credentials", "network", net.Name, "error", err)
		return err
	}
	if _, err := m.o.HA.Call(ctx, map[string]any{"type": "matter/set_thread",
		"thread_operation_dataset": answer.TLV}); err != nil {
		m.log.Error("could not give the matter server the thread network", "network", net.Name, "error", err)
		return err
	}
	return nil
}

func (n networks) threadName() string {
	if n.thread == nil {
		return ""
	}
	return n.thread.Name
}

// with puts in front of a tool's answer which networks Mira gives, so the
// model words its sentence for either kind of device; with several Thread
// networks and none preferred it says so (decision 26).
func (n networks) with(t texts, answer string) string {
	var line string
	switch {
	case n.wifi && n.thread != nil:
		line = fmt.Sprintf(t.netsBoth, n.thread.Name)
	case n.thread != nil:
		line = fmt.Sprintf(t.netsThread, n.thread.Name)
	case n.wifi:
		line = t.netsWifi
	}
	if n.thread == nil && n.count > 1 {
		line = strings.TrimSpace(line + " " + t.netsNoPreferred)
	}
	if line == "" {
		return answer
	}
	return line + " " + answer
}

// missing is what Mira says when the commissioning ends without a device and
// she lacked one of the networks (decision 27); "" when she had both.
func (n networks) missing(t texts) string {
	switch {
	case !n.wifi:
		return " " + t.missingWifi
	case n.thread == nil && n.count > 1:
		return " " + t.missingPreferred
	case n.thread == nil:
		return " " + t.missingThread
	}
	return ""
}
