package watch

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
)

type texts struct {
	// catalog introduces the blueprints to the planner and says how to
	// answer with one.
	catalog string
	// about says per blueprint what it watches; labels name the inputs as
	// the proposal says them; messages are the entries' texts, Home
	// Assistant templates over the blueprint's variables.
	about, labels, messages                               map[string]string
	entityIn, numberIn, textIn, defaultIs                 string
	unknownBlueprint, notReady, unknownInput, notANumber  string
	outOfRange, notAnEntity, wrongEntity, badWindows, gap string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		catalog: "\n\nBlueprints: fertige, geprüfte Wächter in Home Assistant. Passt einer zum Wunsch, nimm ihn statt einer eigenen Automation und fülle nur seine Eingaben: antworte dann nur {\"title\": ein bis drei Wörter, \"summary\": ein Satz an den Nutzer in der Du-Form, was du beobachtest und dass du Bescheid sagst, mit den Werten in Worten, \"blueprint\": der Name, \"inputs\": {Eingabe: Wert}}. Lass eine Eingabe mit Standard weg, wenn der Wunsch nichts dazu sagt. Will der Nutzer etwas, das keine Eingabe abdeckt, bau nichts drumherum: antworte {\"error\": was davon nicht geht}.",
		about: map[string]string{
			"water_runs_on":     "Wasser läuft ohne Pause: der Wasserzähler ändert sich duration Minuten lang immer wieder (Pausen kürzer als gap Minuten zählen als Laufen), dann eine Meldung, einmal je Durchlauf",
			"water_never_rests": "Wasser ruht nie: der Wasserzähler steht in window Stunden nie rest Stunden still (tropfender Hahn, Spülung läuft nach), dann eine Meldung, solange es so bleibt alle window Stunden",
			"daily_routine":     "Tagesablauf eines Geräts: seine Leistung liegt jeden Tag zu festen Zeiten über on_above Watt (windows, z. B. 07:40-11:40, 14:20-21:00); weicht es länger als tolerance Minuten davon ab, eine Meldung",
			"duty_cycle":        "Takt eines Geräts, das den ganzen Tag an- und ausgeht (Kühlschrank, Gefriertruhe): läuft es länger als max_on Minuten ohne Pause über on_above Watt (Tür offen?) oder ist es länger als max_off Minuten aus (Strom weg, defekt?), eine Meldung; max_on und max_off länger als sein längster normaler Lauf und seine längste normale Pause aus device_profile",
			"device_run":        "Gerätelauf eines Geräts, das ab und zu ein Programm fährt (Spülmaschine, Waschmaschine): ein Lauf beginnt über on_above Watt und endet nach off_for Minuten darunter (länger als die Pausen im Programm); läuft er länger als max_minutes, eine Meldung; endet er ohne Spitze über heat_above Watt (0: nicht prüfen), eine Meldung, dass es nicht aufgeheizt hat; die Zahlen aus device_profile",
		},
		labels: map[string]string{
			"meter": "Zähler", "gap": "Pause bis", "duration": "Dauer", "rest": "Ruhe", "window": "Zeitraum",
			"power": "Leistung", "on_above": "an ab", "windows": "Zeiten", "tolerance": "Toleranz",
			"max_on": "längster Lauf", "max_off": "längste Pause", "off_for": "Ende nach", "max_minutes": "längster Lauf",
			"heat_above": "Heizen ab",
		},
		messages: map[string]string{
			"water_runs_on":     "Um {{ now().strftime('%H:%M') }} Uhr lief seit {{ duration }} Minuten Wasser ohne Pause. Zählerstand: {{ states(meter) }} {{ state_attr(meter, 'unit_of_measurement') }}.",
			"water_never_rests": "Um {{ now().strftime('%H:%M') }} Uhr: Der Wasserzähler stand in den letzten {{ window }} Stunden keine {{ rest }} Stunden still. Vielleicht tropft ein Hahn oder die Spülung läuft nach. Zählerstand: {{ states(meter) }} {{ state_attr(meter, 'unit_of_measurement') }}.",
			"daily_routine":     "Um {{ now().strftime('%H:%M') }} Uhr wich {{ state_attr(power, 'friendly_name') }} von seinem Tagesablauf ab: {{ states(power) }} W, erwartet {{ 'an' if expected_on else 'aus' }}.",
			"duty_cycle":        "{% if trigger.id == 'on' %}Um {{ now().strftime('%H:%M') }} Uhr lief {{ state_attr(power, 'friendly_name') }} seit {{ max_on }} Minuten ohne Pause ({{ states(power) }} W). Ist eine Tür offen?{% else %}Um {{ now().strftime('%H:%M') }} Uhr lief {{ state_attr(power, 'friendly_name') }} seit {{ max_off }} Minuten nicht mehr. Ist der Strom weg oder ist es defekt?{% endif %}",
			"device_run":        "{% if what == 'long' %}Um {{ now().strftime('%H:%M') }} Uhr lief {{ state_attr(power, 'friendly_name') }} schon {{ max_minutes }} Minuten, länger als sonst.{% else %}Um {{ now().strftime('%H:%M') }} Uhr war {{ state_attr(power, 'friendly_name') }} nach {{ minutes }} Minuten fertig, hat aber nicht aufgeheizt (nie über {{ heat_above }} W).{% endif %}",
		},
		entityIn:         "Entität %s mit device_class %s",
		numberIn:         "Zahl %s–%s %s",
		textIn:           "Text",
		defaultIs:        ", Standard %v",
		unknownBlueprint: "den Blueprint %q gibt es nicht; nimm einen aus der Liste oder schreib eine eigene Automation",
		notReady:         "der Blueprint %q ist in Home Assistant nicht bereit (%v); schreib eine eigene Automation",
		unknownInput:     "der Blueprint %s hat keine Eingabe %q; seine Eingaben: %s",
		notANumber:       "die Eingabe %s braucht eine Zahl, nicht %v",
		outOfRange:       "die Eingabe %s liegt mit %v außerhalb von %s–%s",
		notAnEntity:      "die Eingabe %s braucht die id einer Entität, nicht %v",
		wrongEntity:      "die Eingabe %s braucht eine Entität %s mit device_class %s aus der Liste; %s ist das nicht",
		badWindows:       "die Eingabe %s braucht Zeiten wie 07:40-11:40, 14:20-21:00, nicht %q",
		gap:              "die Pause (gap %v min) muss kürzer sein als die Dauer (duration %v min)",
	},
	i18n.EN: {
		catalog: "\n\nBlueprints: ready, checked watches in Home Assistant. If one fits the wish, take it instead of an automation of your own and fill only its inputs: then answer only {\"title\": one to three words, \"summary\": one sentence to the user saying what you watch and that you tell them, with the values in words, \"blueprint\": the name, \"inputs\": {input: value}}. Leave out an input with a default when the wish says nothing about it. If the user wants something no input covers, build nothing around it: answer {\"error\": what of it cannot be done}.",
		about: map[string]string{
			"water_runs_on":     "water runs without a pause: the water meter keeps changing for duration minutes (pauses shorter than gap minutes count as running), then a message, once per streak",
			"water_never_rests": "water never rests: the water meter never stands still for rest hours within window hours (a dripping tap, a toilet that runs on), then a message, every window hours while it stays so",
			"daily_routine":     "a device's daily routine: its power is above on_above watts at fixed times every day (windows, e.g. 07:40-11:40, 14:20-21:00); when it differs from that for longer than tolerance minutes, a message",
			"duty_cycle":        "the cycle of a device that switches on and off all day (a fridge, a freezer): when it runs above on_above watts for longer than max_on minutes without a pause (a door open?) or is off for longer than max_off minutes (power gone, broken?), a message; max_on and max_off longer than its longest normal run and pause from device_profile",
			"device_run":        "the device run of a device that runs a programme now and then (a dishwasher, a washing machine): a run starts above on_above watts and ends after off_for minutes below (longer than the programme's pauses); when it runs longer than max_minutes, a message; when it ends without a peak above heat_above watts (0: no check), a message that it did not heat; the numbers from device_profile",
		},
		labels: map[string]string{
			"meter": "meter", "gap": "pause up to", "duration": "duration", "rest": "rest", "window": "window",
			"power": "power", "on_above": "on above", "windows": "times", "tolerance": "tolerance",
			"max_on": "longest run", "max_off": "longest pause", "off_for": "end after", "max_minutes": "longest run",
			"heat_above": "heating above",
		},
		messages: map[string]string{
			"water_runs_on":     "At {{ now().strftime('%H:%M') }} water had run for {{ duration }} minutes without a pause. Meter: {{ states(meter) }} {{ state_attr(meter, 'unit_of_measurement') }}.",
			"water_never_rests": "At {{ now().strftime('%H:%M') }} the water meter had not stood still for {{ rest }} hours within the last {{ window }} hours. A tap may be dripping or the toilet running on. Meter: {{ states(meter) }} {{ state_attr(meter, 'unit_of_measurement') }}.",
			"daily_routine":     "At {{ now().strftime('%H:%M') }} {{ state_attr(power, 'friendly_name') }} left its daily routine: {{ states(power) }} W, expected {{ 'on' if expected_on else 'off' }}.",
			"duty_cycle":        "{% if trigger.id == 'on' %}At {{ now().strftime('%H:%M') }} {{ state_attr(power, 'friendly_name') }} had run for {{ max_on }} minutes without a pause ({{ states(power) }} W). Is a door open?{% else %}At {{ now().strftime('%H:%M') }} {{ state_attr(power, 'friendly_name') }} had not run for {{ max_off }} minutes. Is the power gone, or is it broken?{% endif %}",
			"device_run":        "{% if what == 'long' %}At {{ now().strftime('%H:%M') }} {{ state_attr(power, 'friendly_name') }} had been running for {{ max_minutes }} minutes, longer than usual.{% else %}At {{ now().strftime('%H:%M') }} {{ state_attr(power, 'friendly_name') }} finished a run of {{ minutes }} minutes, but did not heat (never above {{ heat_above }} W).{% endif %}",
		},
		entityIn:         "entity %s with device_class %s",
		numberIn:         "number %s–%s %s",
		textIn:           "text",
		defaultIs:        ", default %v",
		unknownBlueprint: "there is no blueprint %q; take one from the list or write an automation of your own",
		notReady:         "the blueprint %q is not ready in Home Assistant (%v); write an automation of your own",
		unknownInput:     "the blueprint %s has no input %q; its inputs: %s",
		notANumber:       "the input %s needs a number, not %v",
		outOfRange:       "the input %s is %v, outside %s–%s",
		notAnEntity:      "the input %s needs an entity's id, not %v",
		wrongEntity:      "the input %s needs a %s entity with device_class %s from the list; %s is not one",
		badWindows:       "the input %s needs times like 07:40-11:40, 14:20-21:00, not %q",
		gap:              "the pause (gap %v min) must be shorter than the duration (duration %v min)",
	},
}

func (w *Blueprints) texts() texts { return i18n.Pick(catalog, w.opts.Lang) }

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Catalog is what the planner learns of the blueprints that are ready: name,
// what it watches, its inputs with their bounds and defaults, and how to
// answer with one. Empty when none is ready.
func (w *Blueprints) Catalog(ctx context.Context) string {
	t := w.texts()
	list := w.Available(ctx)
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(t.catalog)
	for _, bp := range list {
		fmt.Fprintf(&b, "\n- %s: %s. Inputs:", bp.Name, t.about[bp.Name])
		for _, in := range bp.Inputs {
			if in.Name == MessageInput {
				continue
			}
			var kind string
			switch in.Kind {
			case "entity":
				kind = fmt.Sprintf(t.entityIn, in.Domain, in.DeviceClass)
			case "number":
				kind = strings.TrimSpace(fmt.Sprintf(t.numberIn, num(in.Min), num(in.Max), in.Unit))
			default:
				kind = t.textIn
			}
			fmt.Fprintf(&b, " %s (%s", in.Name, kind)
			if in.HasDefault {
				fmt.Fprintf(&b, t.defaultIs, in.Default)
			}
			b.WriteString(");")
		}
	}
	return b.String()
}

// Entity is what the check needs to know of an entity the planner named.
type Entity struct {
	Domain, DeviceClass string
}

var windowsPattern = regexp.MustCompile(`^\s*([01]?\d|2[0-3]):[0-5]\d\s*-\s*([01]?\d|2[0-3]):[0-5]\d\s*(,\s*([01]?\d|2[0-3]):[0-5]\d\s*-\s*([01]?\d|2[0-3]):[0-5]\d\s*)*$`)

// Check is step A3: the code refuses a blueprint that does not exist or is
// not ready, an input it does not have, a number outside the selector's
// bounds, an entity that is not of the selector's domain and device class
// among the entities Mira may see (entity), and times that do not read. Home
// Assistant checks none of this but missing inputs (blueprint/substitute).
// It returns the blueprint and the inputs to save: the planner's, numbers as
// numbers, and the entry's text in the house's language, which is the
// service's and replaces whatever the planner gave for it.
func (w *Blueprints) Check(name string, inputs map[string]any, entity func(id string) (Entity, bool)) (Blueprint, map[string]any, error) {
	t := w.texts()
	b, ok := ByName(name)
	if !ok {
		return Blueprint{}, nil, fmt.Errorf(t.unknownBlueprint, name)
	}
	if ready, why := w.isReady(name); !ready {
		return Blueprint{}, nil, fmt.Errorf(t.notReady, name, why)
	}
	out := map[string]any{}
	for key, v := range inputs {
		if key == MessageInput {
			continue
		}
		in, ok := b.Input(key)
		if !ok {
			var names []string
			for _, in := range b.Inputs {
				if in.Name != MessageInput {
					names = append(names, in.Name)
				}
			}
			return Blueprint{}, nil, fmt.Errorf(t.unknownInput, name, key, strings.Join(names, ", "))
		}
		switch in.Kind {
		case "number":
			f, ok := number(v)
			if !ok {
				return Blueprint{}, nil, fmt.Errorf(t.notANumber, key, v)
			}
			if f < in.Min || f > in.Max {
				return Blueprint{}, nil, fmt.Errorf(t.outOfRange, key, v, num(in.Min), num(in.Max))
			}
			out[key] = f
		case "entity":
			id, ok := v.(string)
			if !ok || !strings.Contains(id, ".") {
				return Blueprint{}, nil, fmt.Errorf(t.notAnEntity, key, v)
			}
			e, ok := entity(id)
			if !ok || e.Domain != in.Domain || in.DeviceClass != "" && e.DeviceClass != in.DeviceClass {
				return Blueprint{}, nil, fmt.Errorf(t.wrongEntity, key, in.Domain, in.DeviceClass, id)
			}
			out[key] = id
		default:
			s, _ := v.(string)
			if key == "windows" && !windowsPattern.MatchString(s) {
				return Blueprint{}, nil, fmt.Errorf(t.badWindows, key, fmt.Sprint(v))
			}
			out[key] = s
		}
	}
	if b.Name == "water_runs_on" {
		gap, duration := valueOr(b, out, "gap"), valueOr(b, out, "duration")
		if gap >= duration {
			return Blueprint{}, nil, fmt.Errorf(t.gap, gap, duration)
		}
	}
	out[MessageInput] = t.messages[b.Name]
	return b, out, nil
}

// valueOr is a number input as given, else its default.
func valueOr(b Blueprint, inputs map[string]any, name string) float64 {
	if f, ok := number(inputs[name]); ok {
		return f
	}
	in, _ := b.Input(name)
	f, _ := number(in.Default)
	return f
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// Label is how the proposal names an input in the house's language.
func (w *Blueprints) Label(input string) string {
	if l, ok := w.texts().labels[input]; ok {
		return l
	}
	return input
}

// About says what a blueprint watches, in the house's language, as the
// planner's catalog says it.
func (w *Blueprints) About(name string) string { return w.texts().about[name] }
