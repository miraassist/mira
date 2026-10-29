package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	devices, err := a.ha.Devices(ctx)
	if err != nil {
		return phone.Line{}
	}
	var line phone.Line
	for _, d := range devices {
		if d.ID == deviceID {
			line.Node = slug(d.Name)
		}
	}
	if line.Node == "" {
		return line
	}
	if services, err := a.ha.Services(ctx); err == nil {
		_, line.Phone = services["esphome"][line.Node+"_phone_call"]
		_, line.Named = services["esphome"][line.Node+"_phone_call_named"]
		_, line.Intercom = services["esphome"][line.Node+"_intercom_call"]
	}
	if !line.Phone {
		return line
	}
	entities, err := a.ha.Entities(ctx)
	if err != nil {
		return line
	}
	sensor := ""
	for _, e := range entities {
		if e.DeviceID == deviceID && strings.HasPrefix(e.EntityID, "sensor.") && strings.HasSuffix(e.EntityID, "_phone") {
			sensor = e.EntityID
		}
	}
	if states, err := a.ha.States(ctx); err == nil {
		for _, s := range states {
			if s.EntityID == sensor {
				line.State = s.State
			}
		}
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
	devices, err := a.ha.Devices(ctx)
	if err != nil {
		return "", "", false
	}
	services, err := a.ha.Services(ctx)
	if err != nil {
		return "", "", false
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	for _, d := range devices {
		if _, ok := services["esphome"][slug(d.Name)+"_intercom_call"]; !ok {
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
