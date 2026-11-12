package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/devices"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/matter"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/waiting"
	"github.com/miraassist/mira/internal/wifi"
	"github.com/miraassist/mira/internal/zigbee"
)

// Taking devices into the house by voice: the skill geraet-einbinden
// (docs/spec/device-onboarding.md, docs/spec/found-devices.md). The core
// holds each thread's device; Zigbee2MQTT brings a device into its network,
// the found devices notice what Home Assistant found.

// zigbeeQuestion is the skill's agent that words the question for a device
// found.
const zigbeeQuestion = zigbee.Skill + ".frage"

// skillSetting is a setting of the skill geraet-einbinden as Mira's
// configuration has it now ("" when not set).
func (a *app) skillSetting(name string) string {
	values := a.liveConfig().Skills.Enabled[devices.Skill]
	if v, ok := values[name]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

// devicesOn: the skill is switched on (decision 6 of the found devices).
func (a *app) devicesOn() bool {
	_, on := a.cfg.Skills.Enabled[devices.Skill]
	return on
}

func (a *app) areaName(id string) string {
	if name := a.structure.Snapshot().AreaNames[id]; name != "" {
		return name
	}
	return id
}

// The threads are built after the tools, so they are looked up when used.
func (a *app) devicePlan(ctx context.Context, inv tools.Invocation, plan string) (string, error) {
	if a.threads == nil {
		return "", errors.New("no threads")
	}
	return a.threads.SetPlan(ctx, inv, devices.Skill, devices.Skill, plan)
}

func (a *app) deviceThread(ctx context.Context, inv tools.Invocation) string {
	if a.threads == nil {
		return ""
	}
	return a.threads.PlanThread(ctx, inv, devices.Skill)
}

func (a *app) newDeviceCore() *devices.Core {
	return devices.NewCore(devices.CoreOptions{
		Lang: a.lang, Logger: a.log, HA: a.ha,
		AreaKeys: func() map[string]string { return a.structure.Snapshot().AreaKeys },
		AreaName: a.areaName,
		DeviceName: func(id string) string {
			if d, ok := a.structure.Snapshot().Devices[id]; ok {
				return d.DisplayName()
			}
			return ""
		},
		Thread: a.deviceThread,
		Tick:   func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		End:    func(ctx context.Context, thread string) { a.threads.Finish(ctx, thread) },
	})
}

// newWaiting shows at a thread's satellite that Mira waits for a device
// (docs/spec/waiting-indicator.md).
func (a *app) newWaiting() *waiting.Waiting {
	if a.ha == nil {
		return nil
	}
	return waiting.New(waiting.Options{
		Lang: a.lang, Logger: a.log, HA: a.ha,
		Device: func(ctx context.Context, thread string) string {
			th, err := a.store.GetThread(ctx, thread)
			if err != nil {
				a.log.Warn("could not read the thread for its satellite; the wait is not shown", "thread", thread, "error", err)
				return ""
			}
			return th.Device
		},
		Node:      a.deviceNode,
		Satellite: func(deviceID string) string { return a.deviceEntity(deviceID, "assist_satellite") },
		HasAction: func(ctx context.Context, domain, service string) bool {
			return a.hasAction(ctx, domain, service)
		},
		BaseURL: a.haAddress,
	})
}

func (a *app) newZigbee() *zigbee.Pairing {
	var w zigbee.Waiter
	if a.waiting != nil {
		w = a.waiting
	}
	return zigbee.New(zigbee.Options{
		Lang: a.lang, Logger: a.log, HA: a.ha, Core: a.devices, Waiting: w,
		Devices:    func() map[string]haws.Device { return a.structure.Snapshot().Devices },
		DeviceArea: func() map[string]string { return a.structure.Snapshot().DeviceArea },
		AreaKeys:   func() map[string]string { return a.structure.Snapshot().AreaKeys },
		AreaName:   a.areaName,
		Start:      a.devicePlan,
		Thread:     a.deviceThread,
		Tick:       func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		Note:       func(ctx context.Context, thread, text string) error { return a.threads.Note(ctx, thread, text) },
		Ask:        func(ctx context.Context, thread, text string) error { return a.threads.Ask(ctx, thread, text) },
		End:        func(ctx context.Context, thread string) { a.threads.Finish(ctx, thread) },
		Question: func(ctx context.Context, facts string) (string, error) {
			if a.agents == nil {
				return "", errors.New("no agents")
			}
			input, _ := json.Marshal(map[string]string{"input": facts})
			res, err := a.agents.CallByName(ctx, zigbeeQuestion, input,
				agent.CallContext{Origin: agent.FromCode, Rights: agent.AllRights, Lang: string(a.lang)})
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(res.Text), nil
		},
	})
}

// newMatter adds Matter devices over Wi-Fi by voice
// (docs/spec/device-onboarding.md, Matter over Wi-Fi).
func (a *app) newMatter() *matter.Onboarding {
	var w matter.Waiter
	if a.waiting != nil {
		w = a.waiting
	}
	return matter.New(matter.Options{
		Lang: a.lang, Logger: a.log, HA: a.ha, Core: a.devices, Waiting: w,
		Wifi:      func() (string, string) { return a.skillSetting("wlan_ssid"), a.skillSetting("wlan_password") },
		Node:      a.deviceNode,
		HasAction: func(ctx context.Context, domain, service string) bool { return a.hasAction(ctx, domain, service) },
		Devices:   func() map[string]haws.Device { return a.structure.Snapshot().Devices },
		Start:     a.devicePlan,
		Thread:    a.deviceThread,
		Tick:      func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		Note:      func(ctx context.Context, thread, text string) error { return a.threads.Note(ctx, thread, text) },
		Ask:       func(ctx context.Context, thread, text string) error { return a.threads.Ask(ctx, thread, text) },
		Question: func(ctx context.Context, facts string) (string, error) {
			if a.agents == nil {
				return "", errors.New("no agents")
			}
			input, _ := json.Marshal(map[string]string{"input": facts})
			res, err := a.agents.CallByName(ctx, zigbeeQuestion, input,
				agent.CallContext{Origin: agent.FromCode, Rights: agent.AllRights, Lang: string(a.lang)})
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(res.Text), nil
		},
	})
}

func (a *app) newFound() *devices.Found {
	return devices.NewFound(devices.FoundOptions{
		Lang: a.lang, Logger: a.log, HA: a.ha, Core: a.devices, KV: a.store,
		// A found device's own thread at the house and its entry
		// (docs/spec/messages.md, decisions 27, 28).
		Post: a.foundPost, StartHouse: a.foundStartHouse, TakeItem: a.foundTakeItem, ItemThread: a.foundItemThread,
		Claims: []func(string) bool{a.zigbee.Claims, a.matter.Claims},
		Wifi:   func() (string, string) { return a.skillSetting("wlan_ssid"), a.skillSetting("wlan_password") },
		Start:  a.devicePlan,
		Thread: a.deviceThread,
		Tick:   func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		Note:   func(ctx context.Context, thread, text string) error { return a.threads.Note(ctx, thread, text) },
		End:    func(ctx context.Context, thread string) { a.threads.Finish(ctx, thread) },
	})
}

// newWifi brings Wi-Fi devices into the home Wi-Fi
// (docs/spec/wifi-onboarding.md); the machine's Wi-Fi is looked for only
// while the skill is on.
func (a *app) newWifi() *wifi.Onboarding {
	var radio wifi.Radio
	off := "the skill geraet-einbinden is off"
	if a.devicesOn() {
		radio, off = wifi.Detect(a.cfg.HA.URL)
	}
	o := wifi.Options{
		Lang: a.lang, Logger: a.log, Radio: radio, Off: off,
		Home: func() wifi.Home {
			return wifi.Home{SSID: a.skillSetting("wlan_ssid"), Password: a.skillSetting("wlan_password"),
				MQTTHost: a.skillSetting("mqtt_host"), MQTTUser: a.skillSetting("mqtt_user"), MQTTPassword: a.skillSetting("mqtt_password")}
		},
		Start:  a.devicePlan,
		Tick:   func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		Note:   func(ctx context.Context, thread, text string) error { return a.threads.Note(ctx, thread, text) },
		Ask:    func(ctx context.Context, thread, text string) error { return a.threads.Ask(ctx, thread, text) },
		Await:  a.found.Await,
		Assign: a.found.Assign,
	}
	if a.waiting != nil {
		o.Waiting = a.waiting
	}
	return wifi.New(o)
}

// foundKeep is how long a found device's thread and entry wait from when
// Mira finds it (docs/spec/messages.md, decision 11); foundItem is the
// thread's field naming the device.
const (
	foundKeep = 24 * time.Hour
	foundItem = "item"
)

// foundPost puts a found device's entry into the house's device message,
// leading to its thread.
func (a *app) foundPost(ctx context.Context, thread, spoken, details string) error {
	_, _, err := a.store.PutMessage(ctx, store.Message{
		ID: ulid.Make().String(), Kind: store.KindDevice, Source: store.SourceDevices,
		DedupeKey: "devices:" + thread, Importance: 2, Receiver: store.ReceiverHouse,
		Spoken: spoken, Details: details, ThreadID: thread, ExpiresAt: time.Now().Add(foundKeep),
	})
	return err
}

// foundStartHouse starts a found device's own thread at the house, for a
// day from now.
func (a *app) foundStartHouse(ctx context.Context, request, plan, item string) (string, error) {
	if a.threads == nil {
		return "", errors.New("no threads")
	}
	th, err := a.threads.StartAt(ctx, threads.Begin{Target: devices.Skill, Request: request, Plan: plan,
		Deadline: time.Now().Add(foundKeep), Fields: map[string]string{foundItem: item}})
	return th.ID, err
}

// foundTakeItem takes a found device's thread up for the turn; it moves into
// the turn's room.
func (a *app) foundTakeItem(ctx context.Context, inv tools.Invocation, item string) (string, bool, error) {
	if a.threads == nil {
		return "", false, nil
	}
	return a.threads.TakeItem(ctx, inv, devices.Skill, foundItem, item)
}

// foundItemThread is a found device's thread, wherever it is; "" without one.
func (a *app) foundItemThread(ctx context.Context, item string) string {
	if a.threads == nil {
		return ""
	}
	id, err := a.threads.ItemThread(ctx, devices.Skill, foundItem, item)
	if err != nil {
		a.log.Warn("could not find a found device's thread; it ends at its expiry", "item", item, "error", err)
	}
	return id
}
