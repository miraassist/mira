package phone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const ToolCall = "phone_call"

// Line is the satellite's phone: its ESPHome node, which of the call
// actions it has and the state of its phone (the sensor Phone: Ready, Not set
// up, Connecting, Login refused, Cannot sign in, a call's phase, or
// unavailable).
type Line struct {
	Node            string
	Phone, Intercom bool
	// Named: the device has phone_call_named (TECHO5 from 1.6.5), which
	// shows whom a call is to on the call page.
	Named bool
	State string
}

// maxName is the longest name phone_call_named takes, in characters.
const maxName = 40

// Ready is the phone's state when it is signed in at the registrar.
const Ready = "Ready"

type Options struct {
	Lang i18n.Lang
	// Book is the phone book; nil or off, only rooms and numbers are called.
	Book *Book
	// Line is the phone of a satellite's device.
	Line func(ctx context.Context, deviceID string) Line
	// Device finds a TECHO5 device in the house by a room or its name: the
	// name intercom_call takes and its device id.
	Device func(ctx context.Context, target string) (name, deviceID string, ok bool)
	// Call calls an ESPHome action, esphome.<service>.
	Call func(ctx context.Context, service string, data map[string]any) error
}

type texts struct {
	desc, targetDesc, kindDesc                                string
	notVoice, noPhone, noIntercom, self                       string
	intercom, called, several, kinds, noKind, noNumber        string
	notFound, bookOff, bookError                              string
	notSetUp, connecting, refused, cannot, busy, unavailable  string
	unknownState                                              string
	kindMobile, kindHome, kindWork, kindIntern, kindFixedline string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		desc:          "Ruft jemanden an, vom Satelliten, an dem gerade gesprochen wird: einen Kontakt aus dem Telefonbuch („Ruf Oma an“), ein Gerät oder einen Raum im Haus über die Gegensprechanlage („Ruf die Küche an“) oder eine Nummer. Nur im Sprachgespräch. Wählt sofort, wenn das Ziel eindeutig ist; sonst bekommst du die Auswahl zum Nachfragen. Das Werkzeug liefert Fakten, keine fertigen Sätze: Antworte kurz, in deinen eigenen Worten und in deinem Ton, nie wörtlich und nie mit der Nummer.",
		targetDesc:    "Wen der Nutzer anrufen will, wie er es sagt: ein Name, ein Raum oder Gerät im Haus, oder eine Nummer.",
		kindDesc:      "Welche Nummer, nur wenn der Nutzer es sagt: Handy, privat, geschäftlich oder intern.",
		notVoice:      "Ergebnis: nicht gewählt. Grund: Anrufen geht nur im Sprachgespräch an einem Satelliten mit Telefon.",
		noPhone:       "Ergebnis: nicht gewählt. Grund: Dieser Satellit kann nicht telefonieren.",
		noIntercom:    "Ergebnis: nicht gewählt. Grund: Dieser Satellit kann keine Geräte im Haus anrufen.",
		self:          "Ergebnis: nicht gewählt. Grund: Das Ziel ist der Satellit, an dem der Nutzer selbst steht.",
		intercom:      "Ergebnis: Anruf läuft. Ziel: %s, über die Gegensprechanlage.",
		called:        "Ergebnis: wird gewählt. Ziel: %s.",
		several:       "Ergebnis: nicht gewählt. Mehrere Kontakte passen: %s. Nächster Schritt: nachfragen, wer gemeint ist, dann phone_call mit dem ganzen Namen.",
		kinds:         "Ergebnis: nicht gewählt. %s hat mehrere Nummern: %s. Nächster Schritt: nachfragen, welche, dann phone_call mit kind.",
		noKind:        "Ergebnis: nicht gewählt. %s hat keine Nummer der Art „%s“, nur: %s. Nächster Schritt: nachfragen.",
		noNumber:      "Ergebnis: nicht gewählt. Grund: %s hat keine Nummer, die man anrufen kann.",
		notFound:      "Ergebnis: nicht gewählt. Gesucht: „%s“. Kein Kontakt im Telefonbuch, kein Gerät im Haus.",
		bookOff:       "Ergebnis: nicht gewählt. Gesucht: „%s“. Grund: Das Telefonbuch ist aus (Skill telefon); anrufbar sind nur Geräte im Haus und Nummern.",
		bookError:     "Ergebnis: nicht gewählt. Grund: Das Telefonbuch der FRITZ!Box ließ sich nicht lesen (%s).",
		notSetUp:      "Ergebnis: nicht gewählt. Grund: Das Telefon dieses Satelliten hat keine Anmeldung.",
		connecting:    "Ergebnis: nicht gewählt. Grund: Das Telefon dieses Satelliten ist noch nicht an der Telefonanlage angemeldet. Gleich noch einmal versuchen hilft oft; bleibt es so, ist die Telefonanlage vielleicht nicht erreichbar.",
		refused:       "Ergebnis: nicht gewählt. Grund: Die Telefonanlage hat die Anmeldung dieses Satelliten abgelehnt (Benutzername oder Passwort des IP-Telefons falsch).",
		cannot:        "Ergebnis: nicht gewählt. Grund: Der Satellit erreicht die Telefonanlage nicht (Adresse oder Netz).",
		busy:          "Ergebnis: nicht gewählt. Grund: Die Leitung dieses Satelliten ist belegt (%s).",
		unavailable:   "Ergebnis: nicht gewählt. Grund: Der Satellit selbst ist nicht erreichbar.",
		unknownState:  "Ergebnis: nicht gewählt. Grund: Das Telefon dieses Satelliten ist nicht bereit (%s).",
		kindMobile:    "Handy",
		kindHome:      "privat",
		kindWork:      "geschäftlich",
		kindIntern:    "intern",
		kindFixedline: "Festnetz",
	},
	i18n.EN: {
		desc:          "Calls someone from the satellite being spoken at: a contact of the phone book (\"call Grandma\"), a device or room in the house over the intercom (\"call the kitchen\"), or a number. Only in a voice conversation. Dials at once when the target is clear; otherwise you get the choice to ask back. The tool returns facts, not sentences: answer briefly, in your own words and your own tone, never word for word and never with the number.",
		targetDesc:    "Whom the user wants to call, as they say it: a name, a room or device in the house, or a number.",
		kindDesc:      "Which number, only when the user says: mobile, home, work or intern.",
		notVoice:      "Result: not dialed. Reason: calls go only in a voice conversation at a satellite with a phone.",
		noPhone:       "Result: not dialed. Reason: this satellite cannot make phone calls.",
		noIntercom:    "Result: not dialed. Reason: this satellite cannot call devices in the house.",
		self:          "Result: not dialed. Reason: the target is the satellite the user is standing at.",
		intercom:      "Result: call going. Target: %s, over the intercom.",
		called:        "Result: dialing. Target: %s.",
		several:       "Result: not dialed. Several contacts fit: %s. Next: ask whom the user means, then phone_call with the full name.",
		kinds:         "Result: not dialed. %s has several numbers: %s. Next: ask which, then phone_call with kind.",
		noKind:        "Result: not dialed. %s has no %s number, only: %s. Next: ask.",
		noNumber:      "Result: not dialed. Reason: %s has no number that can be called.",
		notFound:      "Result: not dialed. Looked for: \"%s\". No contact in the phone book, no device in the house.",
		bookOff:       "Result: not dialed. Looked for: \"%s\". Reason: the phone book is off (skill telefon); only devices in the house and numbers can be called.",
		bookError:     "Result: not dialed. Reason: the FRITZ!Box's phone book could not be read (%s).",
		notSetUp:      "Result: not dialed. Reason: this satellite's phone has no login.",
		connecting:    "Result: not dialed. Reason: this satellite's phone is not signed in at the telephone box yet. Trying again in a moment often helps; if it stays so, the box may not be reachable.",
		refused:       "Result: not dialed. Reason: the telephone box refused this satellite's login (the IP phone's user name or password is wrong).",
		cannot:        "Result: not dialed. Reason: the satellite cannot reach the telephone box (address or network).",
		busy:          "Result: not dialed. Reason: this satellite's line is busy (%s).",
		unavailable:   "Result: not dialed. Reason: the satellite itself is not reachable.",
		unknownState:  "Result: not dialed. Reason: this satellite's phone is not ready (%s).",
		kindMobile:    "mobile",
		kindHome:      "home",
		kindWork:      "work",
		kindIntern:    "intern",
		kindFixedline: "landline",
	},
}

// Tool is phone_call.
func Tool(o Options) tools.InternalTool {
	t := i18n.Pick(catalog, o.Lang)
	schema := fmt.Sprintf(`{"type":"object","properties":{"target":{"type":"string","description":%q},"kind":{"type":"string","description":%q}},"required":["target"]}`,
		t.targetDesc, t.kindDesc)
	return tools.InternalTool{
		Name: ToolCall, Description: t.desc, Schema: schema,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var args struct{ Target, Kind string }
			if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Target) == "" {
				return "", errors.New("target is required")
			}
			if inv.Channel != config.ChannelVoice || inv.DeviceID == "" {
				return t.notVoice, nil
			}
			line := o.Line(ctx, inv.DeviceID)
			if !line.Phone && !line.Intercom {
				return t.noPhone, nil
			}
			target := strings.TrimSpace(args.Target)
			// 1. A room or device in the house: the intercom.
			if name, id, ok := o.Device(ctx, target); ok {
				switch {
				case id == inv.DeviceID:
					return t.self, nil
				case !line.Intercom:
					return t.noIntercom, nil
				}
				if err := o.Call(ctx, line.Node+"_intercom_call", map[string]any{"device": name}); err != nil {
					return "", err
				}
				return fmt.Sprintf(t.intercom, name), nil
			}
			// 2. A number as it is.
			if isNumber(target) {
				return dial(ctx, o, t, line, target, "", target)
			}
			// 3. A contact of the phone book.
			if !o.Book.On() {
				return fmt.Sprintf(t.bookOff, target), nil
			}
			contacts, err := o.Book.Contacts(ctx)
			if err != nil {
				return fmt.Sprintf(t.bookError, err), nil
			}
			found := match(contacts, target)
			switch {
			case len(found) == 0:
				return fmt.Sprintf(t.notFound, target), nil
			case len(found) > 1:
				var names []string
				for _, c := range found {
					names = append(names, c.Name)
				}
				return fmt.Sprintf(t.several, strings.Join(names, "; ")), nil
			}
			c := found[0]
			nums := callable(c.Numbers)
			if len(nums) == 0 {
				return fmt.Sprintf(t.noNumber, c.Name), nil
			}
			n, ask := pick(nums, kindOf(args.Kind))
			if ask {
				kinds := kindList(t, nums)
				if args.Kind != "" {
					return fmt.Sprintf(t.noKind, c.Name, args.Kind, kinds), nil
				}
				return fmt.Sprintf(t.kinds, c.Name, kinds), nil
			}
			label := c.Name
			if k := kindName(t, n.Type); k != "" {
				label += " (" + k + ")"
			}
			return dial(ctx, o, t, line, n.Number, c.Name, label)
		},
	}
}

// dial calls a number once the satellite's phone is signed in, with the
// contact's name on the call page where the device can show it.
func dial(ctx context.Context, o Options, t texts, line Line, number, name, label string) (string, error) {
	if !line.Phone {
		return t.noPhone, nil
	}
	switch line.State {
	case Ready:
	case "Not set up":
		return t.notSetUp, nil
	case "Connecting":
		return t.connecting, nil
	case "Login refused":
		return t.refused, nil
	case "Cannot sign in":
		return t.cannot, nil
	case "Ringing", "Calling", "In call":
		return fmt.Sprintf(t.busy, line.State), nil
	case "unavailable", "unknown", "":
		return t.unavailable, nil
	default:
		return fmt.Sprintf(t.unknownState, line.State), nil
	}
	service, data := line.Node+"_phone_call", map[string]any{"number": dialable(number)}
	if line.Named && name != "" {
		if r := []rune(name); len(r) > maxName {
			name = string(r[:maxName])
		}
		service, data["name"] = line.Node+"_phone_call_named", name
	}
	if err := o.Call(ctx, service, data); err != nil {
		return "", err
	}
	return fmt.Sprintf(t.called, label), nil
}

// isNumber says whether what was said is a number: digits with the usual
// separators, at least three of them.
func isNumber(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune(" +-/()*#.", r):
		default:
			return false
		}
	}
	return digits >= 3
}

// match finds the contacts a name means: one whose whole name it is, else
// those that have every word of it as a word of their name.
func match(contacts []Contact, target string) []Contact {
	want := words(target)
	if len(want) == 0 {
		return nil
	}
	var exact, partial []Contact
	for _, c := range contacts {
		have := words(c.Name)
		if strings.Join(have, " ") == strings.Join(want, " ") {
			exact = append(exact, c)
			continue
		}
		all := true
		for _, w := range want {
			found := false
			for _, h := range have {
				if h == w {
					found = true
					break
				}
			}
			if !found {
				all = false
				break
			}
		}
		if all {
			partial = append(partial, c)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

// fold maps letters with accents to their plain ones; the rest is lower
// case.
var fold = strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "é", "e", "è", "e", "ê", "e", "á", "a", "à", "a",
	"ó", "o", "ò", "o", "í", "i", "ì", "i", "ú", "u", "ù", "u", "ç", "c", "ñ", "n")

func words(s string) []string {
	s = fold.Replace(strings.ToLower(s))
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// callable are the numbers that can be called: no fax.
func callable(ns []Number) []Number {
	var out []Number
	for _, n := range ns {
		if !strings.HasPrefix(n.Type, "fax") && dialable(n.Number) != "" {
			out = append(out, n)
		}
	}
	return out
}

// kind is a kind of number as the user says it.
type kind string

const (
	kindNone   kind = ""
	kindMobile kind = "mobile"
	kindHome   kind = "home"
	kindWork   kind = "work"
	kindIntern kind = "intern"
)

func kindOf(said string) kind {
	s := fold.Replace(strings.ToLower(strings.TrimSpace(said)))
	switch {
	case s == "":
		return kindNone
	case strings.Contains(s, "handy") || strings.Contains(s, "mobil") || strings.Contains(s, "cell"):
		return kindMobile
	case strings.Contains(s, "privat") || strings.Contains(s, "zuhause") || strings.Contains(s, "zu hause") ||
		strings.Contains(s, "festnetz") || strings.Contains(s, "home") || strings.Contains(s, "landline"):
		return kindHome
	case strings.Contains(s, "arbeit") || strings.Contains(s, "geschaft") || strings.Contains(s, "buro") || strings.Contains(s, "work"):
		return kindWork
	case strings.Contains(s, "intern"):
		return kindIntern
	}
	return kind(s)
}

// kindOfType is the kind of a FRITZ!Box number type.
func kindOfType(typ string) kind {
	t := strings.ToLower(typ)
	switch {
	case t == "mobile" || strings.Contains(t, "mobil"):
		return kindMobile
	case t == "home" || t == "fixedline" || strings.Contains(t, "home"):
		return kindHome
	case t == "work" || strings.Contains(t, "work"):
		return kindWork
	case t == "intern":
		return kindIntern
	}
	return kindNone
}

// pick chooses the number: of the asked kind (its main one, else the first),
// else the main one, else the only one; ask says the user must choose.
func pick(ns []Number, want kind) (Number, bool) {
	choose := func(list []Number) (Number, bool) {
		for _, n := range list {
			if n.Main {
				return n, true
			}
		}
		if len(list) == 1 {
			return list[0], true
		}
		return Number{}, false
	}
	if want != kindNone {
		var of []Number
		for _, n := range ns {
			if kindOfType(n.Type) == want {
				of = append(of, n)
			}
		}
		if len(of) == 0 {
			return Number{}, true
		}
		if n, ok := choose(of); ok {
			return n, false
		}
		return of[0], false
	}
	if n, ok := choose(ns); ok {
		return n, false
	}
	return Number{}, true
}

// kindName is a number's kind for the user, "" when it has none.
func kindName(t texts, typ string) string {
	switch kindOfType(typ) {
	case kindMobile:
		return t.kindMobile
	case kindHome:
		if typ == "fixedline" {
			return t.kindFixedline
		}
		return t.kindHome
	case kindWork:
		return t.kindWork
	case kindIntern:
		return t.kindIntern
	}
	if label, ok := strings.CutPrefix(typ, "label:"); ok {
		return label
	}
	return ""
}

func kindList(t texts, ns []Number) string {
	var out []string
	for i, n := range ns {
		k := kindName(t, n.Type)
		if k == "" {
			k = fmt.Sprintf("#%d", i+1)
		}
		out = append(out, k)
	}
	return strings.Join(out, ", ")
}
