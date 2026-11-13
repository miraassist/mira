package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

// ToolHouseCheck is "Ist alles zu?" in a conversation
// (docs/spec/group-watch.md, C1).
const ToolHouseCheck = "house_check"

// CheckCall is a check from Home Assistant (mira.check, C2) or a
// conversation (C1): the watch group, and for mira.check whom an entry is
// for and whether there is one.
type CheckCall struct {
	watch.CheckRequest
	// Receiver is house (default) or a person; Notify (default true) puts an
	// entry for the receiver when something is open.
	Receiver string `json:"receiver"`
	Notify   *bool  `json:"notify"`
}

// CheckAnswer is the check's result and the entry it put, if any.
type CheckAnswer struct {
	watch.CheckResult
	MessageID string `json:"message_id,omitempty"`
}

type checkTexts struct {
	desc, nothingOpen, open, unreadable, checked, entryOpen, entryUnreadable string
	badReceiver                                                              string
	kinds, openWord                                                          map[string]string
}

var checkCatalog = map[i18n.Lang]checkTexts{
	i18n.DE: {
		desc:            "Prüft einmal, ob alles zu ist („Ist alles zu?“, „Ist oben ein Fenster offen?“, „Brennt irgendwo noch Licht?“), über dieselbe Wächtergruppe wie Miras Wächter, ohne was das Label „Mira: nicht überwachen“ trägt. scope: openings (Fenster und Türen, Standard), lights, motion; areas, floors: Bereiche oder Etagen (Name oder id), sonst das ganze Haus. Antwortet mit dem, was offen oder an ist, und dem, was sich nicht meldet; sag beides. Ein Aufruf an Home Assistant, Millisekunden.",
		nothingOpen:     "Alles zu: %d %s geprüft, nichts %s.",
		open:            "%s: %s.",
		unreadable:      "Meldet sich nicht, also nicht geprüft (sag das dazu): %s.",
		checked:         "Geprüft: %d %s.",
		entryOpen:       "Noch %s: %s.",
		entryUnreadable: " Meldet sich nicht: %s.",
		badReceiver:     "receiver muss house oder eine Person sein, nicht %q",
		kinds:           map[string]string{"openings": "Fenster und Türen", "lights": "Lichter", "motion": "Bewegungsmelder"},
		openWord:        map[string]string{"openings": "offen", "lights": "an", "motion": "meldet Bewegung"},
	},
	i18n.EN: {
		desc:            "Checks once whether everything is closed (\"Is everything closed?\", \"Is a window open upstairs?\", \"Is a light still on anywhere?\"), over the same watch group as Mira's watches, without what carries the label \"Mira: don't watch\". scope: openings (windows and doors, default), lights, motion; areas, floors: areas or floors (name or id), else the whole house. Answers what is open or on, and what does not report; say both. One call to Home Assistant, milliseconds.",
		nothingOpen:     "All closed: %d %s checked, nothing %s.",
		open:            "%s: %s.",
		unreadable:      "Not reporting, so not checked (say so too): %s.",
		checked:         "Checked: %d %s.",
		entryOpen:       "Still %s: %s.",
		entryUnreadable: " Not reporting: %s.",
		badReceiver:     "receiver must be house or a person, not %q",
		kinds:           map[string]string{"openings": "windows and doors", "lights": "lights", "motion": "motion sensors"},
		openWord:        map[string]string{"openings": "open", "lights": "on", "motion": "detecting motion"},
	},
}

func (m *Manager) checkTexts() checkTexts { return i18n.Pick(checkCatalog, m.opts.Lang) }

// ErrNoWatch is a check without Mira's blueprints (no way to render).
var ErrNoWatch = errors.New("checks need mira's watch, which is not set up")

// Check is a check of a watch group once (C1, C2): Home Assistant renders
// the members (watch.HouseCheck); with Notify (the default) and something
// open, an entry for the receiver, kind simple, importance 1, with the key
// "check:<scope>" (a second check while it is unheard replaces it) and an
// expiry of 12 hours. Nothing open: no entry. A request naming what does
// not exist is watch.ErrBadCheck (the caller's error); a render that fails
// is Home Assistant's answer, logged by the watch.
func (m *Manager) Check(ctx context.Context, call CheckCall) (CheckAnswer, error) {
	if m.opts.Watch == nil {
		return CheckAnswer{}, ErrNoWatch
	}
	t := m.checkTexts()
	receiver := strings.TrimSpace(call.Receiver)
	if receiver == "" {
		receiver = store.ReceiverHouse
	}
	if receiver != store.ReceiverHouse {
		if _, ok := m.persons()[receiver]; !ok {
			return CheckAnswer{}, watch.ErrBadCheck{Reason: fmt.Sprintf(t.badReceiver, receiver)}
		}
	}
	res, err := m.opts.Watch.HouseCheck(ctx, call.CheckRequest)
	if err != nil {
		return CheckAnswer{}, err
	}
	ans := CheckAnswer{CheckResult: res}
	if len(res.Open) == 0 || call.Notify != nil && !*call.Notify {
		return ans, nil
	}
	text := fmt.Sprintf(t.entryOpen, t.openWord[res.Scope], memberNames(res.Open))
	if len(res.Unreadable) > 0 {
		text += fmt.Sprintf(t.entryUnreadable, memberNames(res.Unreadable))
	}
	n, _, err := m.opts.Store.PutMessage(ctx, store.Message{ID: ulid.Make().String(), Kind: store.KindSimple,
		Source: store.SourceExternal, DedupeKey: "check:" + res.Scope, Importance: 1, Receiver: receiver, Spoken: text,
		ExpiresAt: m.opts.Clock().Add(12 * time.Hour)})
	if err != nil {
		m.log.Error("could not put the check's entry", "scope", res.Scope, "receiver", receiver, "text", text, "error", err)
		return CheckAnswer{}, fmt.Errorf("the check's entry %q: %w", text, err)
	}
	ans.MessageID = n.ID
	m.log.Info("check put an entry", "scope", res.Scope, "receiver", receiver, "open", len(res.Open))
	return ans, nil
}

func (m *Manager) persons() map[string]bool {
	out := map[string]bool{}
	if m.opts.Persons != nil {
		for id := range m.opts.Persons() {
			out[id] = true
		}
	}
	return out
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// memberNames names members with their areas: "Fenster Bad (Bad), Haustür (Flur)".
func memberNames(members []watch.Member) string {
	var out []string
	for _, mb := range members {
		if mb.Area != "" {
			out = append(out, mb.Name+" ("+mb.Area+")")
		} else {
			out = append(out, mb.Name)
		}
	}
	return strings.Join(out, ", ")
}

// DescribeCheck is the check as the model hears it.
func (m *Manager) DescribeCheck(res watch.CheckResult) string {
	t := m.checkTexts()
	kind := t.kinds[res.Scope]
	var parts []string
	if len(res.Open) == 0 {
		parts = append(parts, fmt.Sprintf(t.nothingOpen, res.Checked, kind, t.openWord[res.Scope]))
	} else {
		parts = append(parts, fmt.Sprintf(t.open, upperFirst(t.openWord[res.Scope]), memberNames(res.Open)),
			fmt.Sprintf(t.checked, res.Checked, kind))
	}
	if len(res.Unreadable) > 0 {
		parts = append(parts, fmt.Sprintf(t.unreadable, memberNames(res.Unreadable)))
	}
	return strings.Join(parts, " ")
}

// checkTool is house_check: offered in every turn, without a skill, as
// "Ist alles zu?" is asked at any time. None without Mira's blueprints.
func (m *Manager) checkTool() []tools.InternalTool {
	if m.opts.Watch == nil {
		return nil
	}
	return []tools.InternalTool{{
		Name: ToolHouseCheck, Description: m.checkTexts().desc, ReadOnly: true,
		Schema: `{"type":"object","properties":{"scope":{"type":"string","enum":["openings","lights","motion"]},"areas":{"type":"array","items":{"type":"string"}},"floors":{"type":"array","items":{"type":"string"}}}}`,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var req watch.CheckRequest
			if len(inv.Args) > 0 {
				if err := json.Unmarshal(inv.Args, &req); err != nil {
					return "", fmt.Errorf("%s: %w", ToolHouseCheck, err)
				}
			}
			res, err := m.opts.Watch.HouseCheck(ctx, req)
			if err != nil {
				return "", err
			}
			return m.DescribeCheck(res), nil
		},
	}}
}
