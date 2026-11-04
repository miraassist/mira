package structure

import (
	"context"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

func sampleRegistry() Registry {
	return Registry{
		Areas: []haws.Area{
			{AreaID: "kueche", Name: "Küche", Aliases: []string{"Kochecke"}},
			{AreaID: "bad", Name: "Bad"},
			{AreaID: "keller", Name: "Keller"},
		},
		Devices: []haws.Device{
			{ID: "sat-kueche", AreaID: "kueche", Name: "Voice PE"},
			{ID: "wr", AreaID: "keller", Name: "Wechselrichter", NameByUser: "Solar-WR"},
			{ID: "old", AreaID: "bad", Name: "Alt", DisabledBy: "user"},
		},
		Entities: []haws.Entity{
			{EntityID: "light.kueche_decke", AreaID: "kueche", Name: "Deckenlicht", Aliases: []string{"Hauptlicht", "Decke"}},
			{EntityID: "sensor.kueche_temp", DeviceID: "sat-kueche", OriginalName: "Temperatur"},
			{EntityID: "sensor.inverter_status", DeviceID: "wr", OriginalName: "Status"},
			{EntityID: "light.bad", AreaID: "bad", Name: "Bad"},
			{EntityID: "sensor.bad_diag", AreaID: "bad", Name: "Diag", EntityCat: "diagnostic"},
			{EntityID: "switch.hidden", AreaID: "bad", Name: "Versteckt", HiddenBy: "user"},
			{EntityID: "lock.haustuer", Name: "Haustür"},
			{EntityID: "light.secret", AreaID: "bad", Name: "Nicht exponiert"},
		},
		Exposed: map[string]bool{
			"light.kueche_decke": true, "sensor.kueche_temp": true, "sensor.inverter_status": true,
			"light.bad": true, "sensor.bad_diag": true, "switch.hidden": true, "lock.haustuer": true,
		},
	}
}

func TestBuildKnowsAreasEntitiesAndAliases(t *testing.T) {
	snap := Build(sampleRegistry())
	if got := snap.EntityNames["light.kueche_decke"]; !slices.Equal(got, []string{"light.kueche_decke", "deckenlicht", "decke", "hauptlicht"}) {
		t.Errorf("names of light.kueche_decke = %v", got)
	}
	if got := snap.EntityNames["sensor.inverter_status"]; !slices.Contains(got, "solar-wr") {
		t.Errorf("the device name is a name too: %v", got)
	}
	if snap.EntityLabel["sensor.kueche_temp"] != "Temperatur" || snap.AreaKeys["kochecke"] != "kueche" {
		t.Errorf("label %q, area alias %q", snap.EntityLabel["sensor.kueche_temp"], snap.AreaKeys["kochecke"])
	}
	if snap.AreaCount != 3 || snap.EntityCount != 7 {
		t.Errorf("counts = %d areas, %d entities", snap.AreaCount, snap.EntityCount)
	}
	if !snap.ExposureKnown {
		t.Error("ExposureKnown = false with an exposure list")
	}
	if got := snap.DeviceArea["sat-kueche"]; got != "kueche" {
		t.Errorf("DeviceArea[sat-kueche] = %q", got)
	}
	if _, ok := snap.DeviceArea["old"]; ok {
		t.Error("disabled device must not resolve to an area")
	}
	if got := snap.EntityArea["sensor.kueche_temp"]; got != "kueche" {
		t.Errorf("entity without own area must inherit the device's: %q", got)
	}
	// Only what is not exposed stays out; an exposed diagnostic or hidden
	// entity is in (the list is taken as it is).
	if snap.ExposedEntities["light.secret"] || snap.EntityNames["light.secret"] != nil {
		t.Error("light.secret is not exposed, but known")
	}
	for _, id := range []string{"sensor.bad_diag", "switch.hidden"} {
		if !snap.ExposedEntities[id] {
			t.Errorf("%s is exposed, but not known", id)
		}
	}
}

// The same house in any order is the same house: no change is reported.
func TestBuildIsIndependentOfInputOrder(t *testing.T) {
	want := Build(sampleRegistry())
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		reg := sampleRegistry()
		rng.Shuffle(len(reg.Areas), func(a, b int) { reg.Areas[a], reg.Areas[b] = reg.Areas[b], reg.Areas[a] })
		rng.Shuffle(len(reg.Devices), func(a, b int) { reg.Devices[a], reg.Devices[b] = reg.Devices[b], reg.Devices[a] })
		rng.Shuffle(len(reg.Entities), func(a, b int) { reg.Entities[a], reg.Entities[b] = reg.Entities[b], reg.Entities[a] })
		for j := range reg.Entities {
			al := reg.Entities[j].Aliases
			rng.Shuffle(len(al), func(a, b int) { al[a], al[b] = al[b], al[a] })
		}
		if got := Build(reg); !Same(got, want) {
			t.Fatalf("shuffle %d changed the house: %v", i, got.EntityNames)
		}
	}
}

// Without the list nothing is exposed: not knowing what the user shared
// never means everything is shared, whatever the entities' own options say.
func TestBuildWithoutExposureListListsNothing(t *testing.T) {
	reg := sampleRegistry()
	reg.Exposed = nil
	for i := range reg.Entities {
		reg.Entities[i].Options = map[string]any{"conversation": map[string]any{"should_expose": true}}
	}
	snap := Build(reg)
	if snap.ExposureKnown {
		t.Error("ExposureKnown = true without an exposure list")
	}
	if len(snap.ExposedEntities) != 0 || snap.EntityNames["light.bad"] != nil {
		t.Errorf("listed without a list: %v", snap.ExposedEntities)
	}
}
func TestFriendlyFromEntityID(t *testing.T) {
	if got := friendlyFromEntityID("light.kueche_decke"); got != "Kueche Decke" {
		t.Errorf("got %q", got)
	}
}

// fakeSource is an in-memory Source whose data and failures the test controls.
type fakeSource struct {
	mu    sync.Mutex
	reg   Registry
	reads atomic.Int32
}

// exposed is what the integration would have sent: the registry's list.
func (f *fakeSource) exposed(context.Context) (map[string]bool, bool) {
	return f.get().Exposed, true
}

func (f *fakeSource) set(reg Registry) { f.mu.Lock(); f.reg = reg; f.mu.Unlock() }
func (f *fakeSource) get() Registry    { f.mu.Lock(); defer f.mu.Unlock(); return f.reg }

func (f *fakeSource) Areas(context.Context) ([]haws.Area, error) {
	f.reads.Add(1)
	return f.get().Areas, nil
}
func (f *fakeSource) Devices(context.Context) ([]haws.Device, error) { return f.get().Devices, nil }
func (f *fakeSource) Entities(context.Context) ([]haws.Entity, error) {
	return f.get().Entities, nil
}

// fakeSubscriber hands out the registered handlers so the test can fire them.
type fakeSubscriber struct {
	mu       sync.Mutex
	handlers map[string]haws.EventHandler
}

func (f *fakeSubscriber) Subscribe(_ context.Context, ev string, h haws.EventHandler) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.handlers == nil {
		f.handlers = map[string]haws.EventHandler{}
	}
	f.handlers[ev] = h
	return func() {}, nil
}

func (f *fakeSubscriber) fire(ev string) bool {
	f.mu.Lock()
	h := f.handlers[ev]
	f.mu.Unlock()
	if h == nil {
		return false
	}
	h(haws.Event{EventType: ev})
	return true
}

func TestRebuildCallsOnChangeOnlyWhenTheHouseChanged(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	var changes atomic.Int32
	c := NewCache(Options{Source: src, Exposed: src.exposed, OnChange: func(Snapshot) { changes.Add(1) }})
	ctx := context.Background()

	if changed, err := c.Rebuild(ctx); err != nil || !changed {
		t.Fatalf("first Rebuild = %v, %v", changed, err)
	}
	if changed, _ := c.Rebuild(ctx); changed {
		t.Fatal("identical rebuild reported a change")
	}
	if changes.Load() != 1 {
		t.Fatalf("OnChange ran %d times, want 1", changes.Load())
	}

	reg := sampleRegistry()
	reg.Areas[0].Name = "Wohnküche"
	src.set(reg)
	if changed, _ := c.Rebuild(ctx); !changed {
		t.Fatal("renamed area did not change the house")
	}
	if c.Snapshot().AreaNames["kueche"] != "Wohnküche" || changes.Load() != 2 {
		t.Fatalf("structure or OnChange not updated: %d, %v", changes.Load(), c.Snapshot().AreaNames)
	}
	if id, name, ok := c.AreaForDevice("sat-kueche"); !ok || id != "kueche" || name != "Wohnküche" {
		t.Errorf("AreaForDevice = %q %q %v", id, name, ok)
	}
}

func TestRebuildWithoutTheIntegrationsListShowsNothing(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	c := NewCache(Options{Source: src})
	if _, err := c.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if c.Snapshot().ExposureKnown {
		t.Error("ExposureKnown must be false before the integration sent the list")
	}
	if len(c.Snapshot().ExposedEntities) != 0 {
		t.Errorf("nothing may be listed without the list: %v", c.Snapshot().ExposedEntities)
	}
}

// What the integration sent is what Mira sees, even though Home
// Assistant does not give the list to a user who is no admin.
func TestExposureFromTheIntegration(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	var first string
	for _, e := range sampleRegistry().Entities {
		first = e.EntityID
		break
	}
	c := NewCache(Options{Source: src, Exposed: func(context.Context) (map[string]bool, bool) {
		return map[string]bool{first: true}, true
	}})
	if _, err := c.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	if !snap.ExposureKnown || len(snap.ExposedEntities) != 1 || !snap.ExposedEntities[first] {
		t.Errorf("exposed = %v, known %v", snap.ExposedEntities, snap.ExposureKnown)
	}
}

func TestRegistryEventsAreDebounced(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	sub := &fakeSubscriber{}
	changed := make(chan Snapshot, 4)
	c := NewCache(Options{Source: src, Exposed: src.exposed, Subscriber: sub, Debounce: 50 * time.Millisecond,
		OnChange: func(s Snapshot) { changed <- s }})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := c.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	<-changed
	go c.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for !sub.fire("entity_registry_updated") {
		if time.Now().After(deadline) {
			t.Fatal("cache never subscribed to registry events")
		}
		time.Sleep(5 * time.Millisecond)
	}
	before := src.reads.Load()

	reg := sampleRegistry()
	reg.Entities[0].Aliases = append(reg.Entities[0].Aliases, "Großes Licht")
	src.set(reg)
	for i := 0; i < 10; i++ {
		sub.fire("entity_registry_updated")
		sub.fire("area_registry_updated")
	}

	select {
	case s := <-changed:
		if !slices.Contains(s.EntityNames[reg.Entities[0].EntityID], "großes licht") {
			t.Fatalf("rebuilt structure lacks the new alias: %v", s.EntityNames)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no rebuild after registry events")
	}
	time.Sleep(150 * time.Millisecond)
	if n := src.reads.Load() - before; n != 1 {
		t.Fatalf("burst of 20 events caused %d rebuilds, want 1", n)
	}
}

func TestResolveNameAndArea(t *testing.T) {
	snap := Build(sampleRegistry())
	cases := []struct {
		name, area string
		want       string
	}{
		{"Deckenlicht", "", "light.kueche_decke"},
		{"  hauptlicht ", "", "light.kueche_decke"},
		{"Deckenlicht", "Kochecke", "light.kueche_decke"},
		{"Deckenlicht", "Bad", ""},
		{"Solar-WR", "", "sensor.inverter_status"},
		{"lock.haustuer", "", "lock.haustuer"},
		{"Bad", "", "light.bad"},
		{"Nicht exponiert", "", ""},
	}
	for _, c := range cases {
		if got := strings.Join(snap.ResolveName(c.name, c.area), ","); got != c.want {
			t.Errorf("ResolveName(%q, %q) = %q, want %q", c.name, c.area, got, c.want)
		}
	}
	if got := strings.Join(snap.EntitiesInArea("küche"), ","); got != "light.kueche_decke,sensor.kueche_temp" {
		t.Errorf("EntitiesInArea(küche) = %q", got)
	}
	if got := snap.EntitiesInArea("Dachboden"); got != nil {
		t.Errorf("unknown area resolved to %v", got)
	}
}

// The voice satellites' devices are kept, whatever is exposed: the agent asks
// Home Assistant for its tools as each of them would get them.
func TestBuildKeepsTheSatelliteDevices(t *testing.T) {
	snap := Build(Registry{
		Entities: []haws.Entity{
			{EntityID: "assist_satellite.show", DeviceID: "dev-b"},
			{EntityID: "assist_satellite.echo", DeviceID: "dev-a"},
			{EntityID: "assist_satellite.dup", DeviceID: "dev-a"},
			{EntityID: "assist_satellite.off", DeviceID: "dev-c", DisabledBy: "user"},
			{EntityID: "assist_satellite.nodevice"},
			{EntityID: "light.lamp", DeviceID: "dev-d"},
		},
		Exposed: map[string]bool{},
	})
	if got := strings.Join(snap.SatelliteDevices, ","); got != "dev-a,dev-b" {
		t.Errorf("satellite devices = %q", got)
	}
}

func TestSR05_ATurnsAreaIsTheSatelliteEntitysElseItsDevices(t *testing.T) {
	snap := Build(Registry{
		Devices: []haws.Device{
			{ID: "dev-flur"},
			{ID: "dev-bad", AreaID: "bad"},
			{ID: "dev-both", AreaID: "keller"},
		},
		Entities: []haws.Entity{
			// the entity has an area, its device none
			{EntityID: "assist_satellite.flur", DeviceID: "dev-flur", AreaID: "flur"},
			// the entity has none: the device's, as before
			{EntityID: "assist_satellite.bad", DeviceID: "dev-bad"},
			// both: the entity's
			{EntityID: "assist_satellite.both", DeviceID: "dev-both", AreaID: "kueche"},
			// another entity's own area does not move its device
			{EntityID: "sensor.flur_temp", DeviceID: "dev-bad", AreaID: "flur"},
		},
		Exposed: map[string]bool{},
	})
	for device, want := range map[string]string{"dev-flur": "flur", "dev-bad": "bad", "dev-both": "kueche"} {
		if got := snap.DeviceArea[device]; got != want {
			t.Errorf("DeviceArea[%s] = %q, want %q", device, got, want)
		}
	}
}
