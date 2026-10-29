package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/phone"
	"github.com/miraassist/mira/internal/skills"
)

// The phone (docs/spec/telefon.md): a TECHO5 satellite's ESPHome actions
// phone_call and intercom_call, and the FRITZ!Box's phone book from Mira
// FRITZ!Box when the skill telefon is on.

// phoneSkill is the skill that switches the phone book on.
const phoneSkill = "telefon"

// phoneBookOn says whether the skill telefon is on and started.
func (a *app) phoneBookOn() bool {
	_, ok := a.cfg.Skills.Enabled[phoneSkill]
	return ok && a.skills != nil && slices.ContainsFunc(a.skills.Skills, func(s skills.Skill) bool { return s.Name == phoneSkill })
}

// readPhonebook asks Mira FRITZ!Box for the phone book (its action
// get_phonebook).
func (a *app) readPhonebook(ctx context.Context) (phone.Phonebook, error) {
	raw, err := a.ha.CallServiceResponse(ctx, "mira_fritz", "get_phonebook", nil)
	if errors.Is(err, haws.ErrNotFound) {
		return phone.Phonebook{}, phone.ErrNoIntegration
	}
	if err != nil {
		return phone.Phonebook{}, err
	}
	var pb phone.Phonebook
	if err := json.Unmarshal(raw, &pb); err != nil {
		return phone.Phonebook{}, fmt.Errorf("the phone book of Mira FRITZ!Box: %w", err)
	}
	return pb, nil
}

// phoneLine is the phone of a satellite's device: its node, its call actions
// and the state of its Phone sensor.
func (a *app) phoneLine(ctx context.Context, deviceID string) phone.Line {
	line := phone.Line{Node: a.deviceNode(deviceID)}
	if line.Node == "" {
		return line
	}
	line.Phone = a.hasAction(ctx, "esphome", line.Node+"_phone_call")
	line.Named = a.hasAction(ctx, "esphome", line.Node+"_phone_call_named")
	line.Intercom = a.hasAction(ctx, "esphome", line.Node+"_intercom_call")
	if !line.Phone {
		return line
	}
	sensor := ""
	for _, id := range a.structure.Snapshot().DeviceEntities[deviceID] {
		if strings.HasPrefix(id, "sensor.") && strings.HasSuffix(id, "_phone") {
			sensor = id
		}
	}
	if sensor == "" {
		return line
	}
	states, err := a.ha.GetStates(ctx, haws.StateFilter{EntityIDs: []string{sensor}})
	if err != nil {
		a.log.Warn("could not read the phone line", "sensor", sensor, "error", err)
	}
	for _, s := range states {
		line.State = s.State
	}
	return line
}

// articles are what comes before a room in "ruf die Küche an".
var articles = []string{"in der ", "in die ", "im ", "ins ", "die ", "das ", "den ", "dem ", "der ", "the "}

// intercomDevice is a TECHO5 device in the house by a room or its name: the
// name intercom_call takes and its device id.
func (a *app) intercomDevice(ctx context.Context, target string) (string, string, bool) {
	t := strings.ToLower(strings.TrimSpace(target))
	for _, art := range articles {
		t = strings.TrimPrefix(t, art)
	}
	area := ""
	if a.structure != nil {
		area = a.structure.Snapshot().AreaKeys[t]
	}
	devices := slices.Collect(maps.Values(a.structure.Snapshot().Devices))
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	for _, d := range devices {
		if !a.hasAction(ctx, "esphome", slug(d.Name)+"_intercom_call") {
			continue
		}
		if (area != "" && d.AreaID == area) || strings.ToLower(d.Name) == t || strings.ToLower(d.NameByUser) == t {
			return d.Name, d.ID, true
		}
	}
	return "", "", false
}

func (a *app) callESPHome(ctx context.Context, service string, data map[string]any) error {
	return a.ha.CallService(ctx, "esphome", service, data, nil)
}
