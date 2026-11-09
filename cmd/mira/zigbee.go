package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/miraassist/mira/internal/agent"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/zigbee"
)

// Adding a Zigbee device by voice (docs/spec/device-onboarding.md): the
// skill geraet-einbinden's tools on Home Assistant and the threads.

// zigbeeQuestion is the skill's agent that words the question for a device
// found.
const zigbeeQuestion = zigbee.Skill + ".frage"

func (a *app) newZigbee() *zigbee.Pairing {
	return zigbee.New(zigbee.Options{
		Lang: a.lang, Logger: a.log, HA: a.ha,
		Devices:    func() map[string]haws.Device { return a.structure.Snapshot().Devices },
		DeviceArea: func() map[string]string { return a.structure.Snapshot().DeviceArea },
		AreaKeys:   func() map[string]string { return a.structure.Snapshot().AreaKeys },
		AreaName: func(id string) string {
			if name := a.structure.Snapshot().AreaNames[id]; name != "" {
				return name
			}
			return id
		},
		// The threads are built after the tools, so they are looked up when used.
		Start: func(ctx context.Context, inv tools.Invocation, plan string) (string, error) {
			if a.threads == nil {
				return "", errors.New("no threads")
			}
			return a.threads.SetPlan(ctx, inv, zigbee.Skill, zigbee.Skill, plan)
		},
		Thread: func(ctx context.Context, inv tools.Invocation) string {
			if a.threads == nil {
				return ""
			}
			return a.threads.PlanThread(ctx, inv, zigbee.Skill)
		},
		Tick: func(ctx context.Context, thread, match string) error { return a.threads.TickPlan(ctx, thread, match) },
		Note: func(ctx context.Context, thread, text string) error { return a.threads.Note(ctx, thread, text) },
		Ask:  func(ctx context.Context, thread, text string) error { return a.threads.Ask(ctx, thread, text) },
		End:  func(ctx context.Context, thread string) { a.threads.Finish(ctx, thread) },
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
