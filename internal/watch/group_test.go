package watch

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// fakeTemplates plays render_template: the answer of each call in order.
type fakeTemplates struct {
	answers   []json.RawMessage
	errs      []error
	templates []string
	vars      []map[string]any
}

func (f *fakeTemplates) RenderTemplate(_ context.Context, tpl string, vars map[string]any) (json.RawMessage, error) {
	f.templates = append(f.templates, tpl)
	f.vars = append(f.vars, vars)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, nil
}

func groupTest(t *testing.T, tpl *fakeTemplates) *Blueprints {
	t.Helper()
	w := New(Options{HA: &fakeHA{have: current()}, Templates: tpl, Lang: i18n.DE, Policy: &retry.Policy{Attempts: 3, Growth: retry.Fixed}})
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	return w
}

// Every group blueprint holds the shared selection twice (its trigger and
// the members it names), word for word, and its domain and classes are one
// of the scopes; its inputs areas, floors and group are read as such.
func TestGroupBlueprintsShareTheSelection(t *testing.T) {
	sel := strings.Join(strings.Fields(SelectionTemplate()), " ")
	var group []string
	for _, b := range All {
		if !b.Group() {
			continue
		}
		group = append(group, b.Name)
		if n := strings.Count(strings.Join(strings.Fields(b.YAML()), " "), sel); n != 2 {
			t.Errorf("%s holds the selection %d times", b.Name, n)
		}
		if _, ok := ScopeOf(b); !ok {
			t.Errorf("%s: domain %s, classes %v is no scope", b.Name, b.Domain, b.Classes)
		}
		for name, kind := range map[string]string{"areas": "area", "floors": "floor", GroupInput: "group"} {
			if in, ok := b.Input(name); !ok || in.Kind != kind || !in.HasDefault {
				t.Errorf("%s.%s: %+v", b.Name, name, in)
			}
		}
		if !strings.Contains(b.YAML(), "dedupe_key: \"watch:{{ this.attributes.id }}\"") ||
			!strings.Contains(b.YAML(), "'persons' if states('zone.home') | int(1) == 0 else 'house'") {
			t.Errorf("%s: the entry's receiver or key", b.Name)
		}
	}
	want := []string{"left_on", "motion_at_night", "motion_while_away", "open_at_night", "open_too_long", "open_while_away", "temperature_out_of_range"}
	if !slices.Equal(group, want) {
		t.Errorf("group blueprints %v", group)
	}
	tb, _ := ByName("temperature_out_of_range")
	if below, _ := tb.Input("below"); below.Kind != "number" || !below.HasDefault || below.Default != nil {
		t.Errorf("below: %+v", below)
	}
	nb, _ := ByName("open_at_night")
	if start, _ := nb.Input("night_start"); start.Kind != "time" || start.HasDefault {
		t.Errorf("night_start: %+v", start)
	}
	lb, _ := ByName("left_on")
	if away, _ := lb.Input("only_away"); away.Kind != "boolean" || away.Default != false {
		t.Errorf("only_away: %+v", away)
	}
}

// The check reads a group blueprint's inputs: lists of areas and floors,
// times, a boolean; an optional input left empty stays out.
func TestCheckGroupInputs(t *testing.T) {
	w := groupTest(t, &fakeTemplates{})
	_, in, err := w.Check("open_at_night", map[string]any{"floors": []any{"dachgeschoss"}, "areas": "keller, bad", "night_start": "23:00", "night_end": "6:30", "group": ""}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in["floors"].([]string), []string{"dachgeschoss"}) || !slices.Equal(in["areas"].([]string), []string{"keller", "bad"}) ||
		in["night_start"] != "23:00:00" || in["night_end"] != "06:30:00" || in[GroupInput] != nil {
		t.Errorf("inputs %v", in)
	}
	_, in, err = w.Check("temperature_out_of_range", map[string]any{"below": 5, "above": nil}, nil)
	if _, has := in["above"]; err != nil || has || in["below"] != 5.0 {
		t.Errorf("inputs %v, %v", in, err)
	}
	for _, c := range []struct {
		name   string
		inputs map[string]any
		want   string
	}{
		{"open_at_night", map[string]any{"night_start": "nachts"}, "Uhrzeit"},
		{"left_on", map[string]any{"only_away": "vielleicht"}, "true oder false"},
		{"left_on", map[string]any{"group": "Lichter"}, "id einer Entität"},
	} {
		if _, _, err := w.Check(c.name, c.inputs, nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v: %v, want %q", c.name, c.inputs, err, c.want)
		}
	}
}

var testHouse = House{
	Floors: []Floor{{ID: "erdgeschoss", Name: "Erdgeschoss", Areas: []string{"bad"}}, {ID: "dachgeschoss", Name: "Dachgeschoss", Areas: []string{"buro"}}},
	Areas:  map[string]string{"bad": "Bad", "buro": "Büro", "garten": "Garten"},
	Groups: []GroupHelper{{ID: "binary_sensor.fenster_gast", Name: "Fenster Gast", DeviceClass: "window"}, {ID: "light.aussen", Name: "Außen"}},
}

// The selection names what exists: areas, floors, a group helper of the
// blueprint's domain, not with areas or floors; a temperature needs a limit.
func TestCheckSelection(t *testing.T) {
	w := groupTest(t, &fakeTemplates{})
	open, _ := ByName("open_too_long")
	lights, _ := ByName("left_on")
	temp, _ := ByName("temperature_out_of_range")
	sel, err := w.CheckSelection(open, map[string]any{"floors": []string{"dachgeschoss"}, "areas": []string{"garten"}}, testHouse)
	if err != nil || sel.Scope.Name != "openings" || !slices.Equal(sel.Floors, []string{"dachgeschoss"}) {
		t.Errorf("%+v %v", sel, err)
	}
	if _, err := w.CheckSelection(lights, map[string]any{"group": "light.aussen"}, testHouse); err != nil {
		t.Error(err)
	}
	for _, c := range []struct {
		b      Blueprint
		inputs map[string]any
		want   string
	}{
		{open, map[string]any{"floors": []string{"turm"}}, "Etage \"turm\" gibt es nicht; die Etagen: Erdgeschoss (erdgeschoss), Dachgeschoss (dachgeschoss)"},
		{open, map[string]any{"areas": []string{"keller"}}, "Bereich \"keller\""},
		{open, map[string]any{"group": "light.aussen"}, "kein Gruppen-Helfer"},
		{open, map[string]any{"group": "binary_sensor.fenster_gast", "areas": []string{"bad"}}, "nicht beides"},
		{temp, map[string]any{}, "below oder above"},
		{temp, map[string]any{"below": 20.0, "above": 10.0}, "unter above"},
	} {
		if _, err := w.CheckSelection(c.b, c.inputs, testHouse); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v: %v, want %q", c.b.Name, c.inputs, err, c.want)
		}
	}
}

// Members are rendered by Home Assistant with the shared selection and the
// selection's variables; the answer may come parsed or as a string. A lost
// connection is tried again, a template Home Assistant refuses is not.
func TestMembersRendered(t *testing.T) {
	tpl := &fakeTemplates{
		errs:    []error{haws.ErrNotConnected},
		answers: []json.RawMessage{json.RawMessage(`[{"id": "binary_sensor.a", "name": "Fenster Bad", "state": "on", "area": "Bad"}]`)},
	}
	w := groupTest(t, tpl)
	scope, _ := ScopeByName("openings")
	got, err := w.Members(context.Background(), Selection{Scope: scope, Floors: []string{"erdgeschoss"}})
	if err != nil || len(got) != 1 || got[0].Name != "Fenster Bad" || len(tpl.templates) != 2 {
		t.Fatalf("%v %v (%d calls)", got, err, len(tpl.templates))
	}
	if !strings.HasPrefix(tpl.templates[1], SelectionTemplate()) || tpl.vars[1]["domain"] != "binary_sensor" ||
		!slices.Equal(tpl.vars[1]["floors"].([]string), []string{"erdgeschoss"}) || tpl.vars[1]["group"] != "" {
		t.Errorf("template %q, vars %v", tpl.templates[1], tpl.vars[1])
	}
	tpl2 := &fakeTemplates{answers: []json.RawMessage{json.RawMessage(`"[{\"id\": \"light.a\", \"name\": \"A\", \"state\": \"off\", \"area\": \"\"}]"`)}}
	w2 := groupTest(t, tpl2)
	if got, err := w2.Members(context.Background(), Selection{Scope: Scopes[0]}); err != nil || got[0].ID != "light.a" {
		t.Errorf("%v %v", got, err)
	}
	refused := &haws.Error{Command: "render_template", Code: "template_error", Message: "UndefinedError: 'x' is undefined"}
	tpl3 := &fakeTemplates{errs: []error{refused}}
	w3 := groupTest(t, tpl3)
	if _, err := w3.Members(context.Background(), Selection{Scope: Scopes[0]}); !errors.Is(err, refused) || len(tpl3.templates) != 1 {
		t.Errorf("%v, %d calls", err, len(tpl3.templates))
	}
}

// The planner's catalog explains the watch groups and names the house's
// floors, areas and group helpers, read once.
func TestGroupCatalog(t *testing.T) {
	h, _ := json.Marshal(testHouse)
	tpl := &fakeTemplates{answers: []json.RawMessage{h, json.RawMessage(`[{"id": "light.terrasse", "name": "Terrasse", "state": "off", "area": "Garten"}]`),
		json.RawMessage(`[]`), json.RawMessage(`[]`), json.RawMessage(`[]`)}}
	w := groupTest(t, tpl)
	cat := w.Catalog(context.Background(), true)
	for _, want := range []string{"Wächtergruppe", "exclude", "Etage Dachgeschoss (dachgeschoss): Büro (buro)", "Garten (garten)",
		"Gruppen-Helfer: Fenster Gast (binary_sensor.fenster_gast)", "- left_on:", "areas (Liste von Bereichs-ids, optional)",
		"night_start (Uhrzeit HH:MM)", "below (Zahl -40–60 °C, optional)", "Im ganzen Haus heute, Lichter (id, Name, Bereich): light.terrasse (Terrasse, Garten)"} {
		if !strings.Contains(cat, want) {
			t.Errorf("catalog lacks %q:\n%s", want, cat)
		}
	}
	// Without a watch group in the wish the house is not read.
	if cat := w.Catalog(context.Background(), false); strings.Contains(cat, "Etage Dachgeschoss") || !strings.Contains(cat, "left_on") || len(tpl.templates) != 5 {
		t.Errorf("%d renders: %s", len(tpl.templates), cat)
	}
}
