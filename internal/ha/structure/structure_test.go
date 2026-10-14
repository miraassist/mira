package structure

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	haws "github.com/sker65/lares-agent/internal/ha/ws"
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

func TestBuildRendersAreasEntitiesAndAliases(t *testing.T) {
	snap := Build(sampleRegistry())
	want := strings.Join([]string{
		"Bereich Bad (bad)",
		"  light.bad — Bad",
		"Bereich Keller (keller)",
		"  sensor.inverter_status — Status, Gerät: Solar-WR",
		"Bereich Küche (kueche), auch genannt: Kochecke",
		"  light.kueche_decke — Deckenlicht, auch genannt: Decke, Hauptlicht",
		"  sensor.kueche_temp — Temperatur, Gerät: Voice PE",
		"Ohne Bereich",
		"  lock.haustuer — Haustür",
	}, "\n")
	if snap.Block != want {
		t.Fatalf("block mismatch\n got:\n%s\nwant:\n%s", snap.Block, want)
	}
	if snap.AreaCount != 3 || snap.EntityCount != 5 {
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
	for _, id := range []string{"light.secret", "sensor.bad_diag", "switch.hidden"} {
		if snap.ExposedEntities[id] || strings.Contains(snap.Block, id) {
			t.Errorf("%s must not be in the block", id)
		}
	}
}

// The cache invariant: the same house in any order renders to the same bytes.
func TestBuildIsIndependentOfInputOrder(t *testing.T) {
	want := Build(sampleRegistry()).Block
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
		if got := Build(reg).Block; got != want {
			t.Fatalf("shuffle %d changed the block:\n%s", i, got)
		}
	}
}

func TestBuildWithoutExposureListUsesEntityOptions(t *testing.T) {
	reg := sampleRegistry()
	reg.Exposed = nil
	for i := range reg.Entities {
		if reg.Entities[i].EntityID == "light.secret" {
			reg.Entities[i].Options = map[string]any{"conversation": map[string]any{"should_expose": false}}
		}
	}
	snap := Build(reg)
	if snap.ExposureKnown {
		t.Error("ExposureKnown = true without an exposure list")
	}
	if strings.Contains(snap.Block, "light.secret") {
		t.Error("entity marked should_expose=false must stay out")
	}
	if !strings.Contains(snap.Block, "light.bad") {
		t.Error("unmarked entity must be included in the fallback")
	}
}

func TestFriendlyFromEntityID(t *testing.T) {
	if got := friendlyFromEntityID("light.kueche_decke"); got != "Kueche Decke" {
		t.Errorf("got %q", got)
	}
}

// fakeSource is an in-memory Source whose data and failures the test controls.
type fakeSource struct {
	mu       sync.Mutex
	reg      Registry
	exposeEr error
	reads    atomic.Int32
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
func (f *fakeSource) ExposedEntities(context.Context) (map[string]bool, error) {
	if f.exposeEr != nil {
		return nil, f.exposeEr
	}
	return f.get().Exposed, nil
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

func TestRebuildCallsOnChangeOnlyWhenTheBlockChanged(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	var changes atomic.Int32
	c := NewCache(Options{Source: src, OnChange: func(Snapshot) { changes.Add(1) }})
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
		t.Fatal("renamed area did not change the block")
	}
	if !strings.Contains(c.Block(), "Wohnküche") || changes.Load() != 2 {
		t.Fatalf("block or OnChange not updated: %d\n%s", changes.Load(), c.Block())
	}
	if id, name, ok := c.AreaForDevice("sat-kueche"); !ok || id != "kueche" || name != "Wohnküche" {
		t.Errorf("AreaForDevice = %q %q %v", id, name, ok)
	}
}

func TestRebuildSurvivesMissingExposureList(t *testing.T) {
	src := &fakeSource{exposeEr: errors.New("unknown_command")}
	src.set(sampleRegistry())
	c := NewCache(Options{Source: src})
	if _, err := c.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if c.Snapshot().ExposureKnown {
		t.Error("ExposureKnown must be false when the list could not be read")
	}
	if c.Block() == "" {
		t.Error("fallback produced an empty block")
	}
}

func TestRegistryEventsAreDebounced(t *testing.T) {
	src := &fakeSource{}
	src.set(sampleRegistry())
	sub := &fakeSubscriber{}
	changed := make(chan Snapshot, 4)
	c := NewCache(Options{Source: src, Subscriber: sub, Debounce: 50 * time.Millisecond,
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
		if !strings.Contains(s.Block, "Großes Licht") {
			t.Fatalf("rebuilt block lacks the new alias:\n%s", s.Block)
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
