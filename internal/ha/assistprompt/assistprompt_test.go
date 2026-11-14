package assistprompt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type source struct {
	text string
	err  error
}

func (s *source) PromptAs(context.Context, string, string, string) (string, error) {
	return s.text, s.err
}

// perDevice answers like Home Assistant: a shared start and, at the end, lines
// that depend on the device.
type perDevice struct {
	start  string
	tails  map[string]string
	broken map[string]bool
	asked  []string
}

func (p *perDevice) PromptAs(_ context.Context, _, _, device string) (string, error) {
	p.asked = append(p.asked, device)
	if p.broken[device] {
		return "", errors.New("down")
	}
	return p.start + "\n" + p.tails[device], nil
}

func TestRefreshReportsOnlyRealChanges(t *testing.T) {
	src := &source{text: "Static Context: Küche\n"}
	changes := 0
	c := New(Options{Source: src, Server: "ha", OnChange: func() { changes++ }})
	ctx := context.Background()

	if c.Text() != "" {
		t.Fatal("text before the first fetch")
	}
	if changed, err := c.Refresh(ctx); err != nil || !changed {
		t.Fatalf("first refresh = %v, %v", changed, err)
	}
	if c.Text() != "Static Context: Küche" || changes != 0 {
		t.Fatalf("text = %q, changes = %d (the first fetch is no change)", c.Text(), changes)
	}
	if changed, _ := c.Refresh(ctx); changed || changes != 0 {
		t.Fatal("same text counted as a change")
	}
	src.text = "Static Context: Küche, Bad"
	if changed, _ := c.Refresh(ctx); !changed || changes != 1 {
		t.Fatalf("changed text not reported, changes = %d", changes)
	}
	src.err = errors.New("mcp down")
	if _, err := c.Refresh(ctx); err == nil {
		t.Fatal("error swallowed")
	}
	if c.Text() != "Static Context: Küche, Bad" {
		t.Error("a failed fetch must keep the last text")
	}
}

func newPerDevice() *perDevice {
	return &perDevice{
		start: "Static Context: Küche\nWhen controlling Home Assistant always call the intent tools.",
		tails: map[string]string{
			"":      "When a user asks to turn on all devices, ask for an area.\nThis device is not able to start timers.",
			"sat-a": "You are in area Küche and generic commands target it.",
			"sat-b": "You are in area Bad and generic commands target it.",
		},
	}
}

// What all versions share is layer 4; each device's own lines are the tail,
// and a request without a device gets the version without one.
func TestSharedStartIsLayerFourAndTheRestIsTheDevicesTail(t *testing.T) {
	src := newPerDevice()
	c := New(Options{Source: src, Server: "ha", Devices: func() []string { return []string{"sat-a", "sat-b"} }})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Text(), "Static Context: Küche\nWhen controlling Home Assistant always call the intent tools."; got != want {
		t.Errorf("layer 4 = %q", got)
	}
	if got := c.Tail("sat-a"); got != "You are in area Küche and generic commands target it." {
		t.Errorf("tail a = %q", got)
	}
	if got := c.Tail("sat-b"); got != "You are in area Bad and generic commands target it." {
		t.Errorf("tail b = %q", got)
	}
	for _, device := range []string{"", "unknown"} {
		if got := c.Tail(device); got != "When a user asks to turn on all devices, ask for an area.\nThis device is not able to start timers." {
			t.Errorf("tail for %q = %q", device, got)
		}
	}
	if len(src.asked) != 3 {
		t.Errorf("asked %q", src.asked)
	}
}

// A change of the device lines does not touch layer 4, so it does not
// count as a change (and costs no cache); a change of the shared part does.
func TestOnlyALayerFourChangeFiresOnChange(t *testing.T) {
	src := newPerDevice()
	changes := 0
	c := New(Options{Source: src, Server: "ha", OnChange: func() { changes++ }, Devices: func() []string { return []string{"sat-a"} }})
	ctx := context.Background()
	c.Refresh(ctx)
	src.tails["sat-a"] = "You are in area Wohnzimmer and generic commands target it."
	if changed, _ := c.Refresh(ctx); changed || changes != 0 {
		t.Fatalf("a device line counted as a change (%v, %d)", changed, changes)
	}
	if got := c.Tail("sat-a"); got != "You are in area Wohnzimmer and generic commands target it." {
		t.Errorf("tail not updated: %q", got)
	}
	src.start += "\n- Stehlampe (light)"
	if changed, _ := c.Refresh(ctx); !changed || changes != 1 {
		t.Fatalf("a new entity was not reported (%v, %d)", changed, changes)
	}
}

// One satellite that does not answer keeps its old tail; the others and the
// shared start are untouched.
func TestASatelliteThatFailsKeepsItsTail(t *testing.T) {
	src := newPerDevice()
	c := New(Options{Source: src, Server: "ha", Devices: func() []string { return []string{"sat-a", "sat-b"} }})
	ctx := context.Background()
	c.Refresh(ctx)
	src.broken = map[string]bool{"sat-b": true}
	src.tails["sat-a"] = "You are in area Flur and generic commands target it."
	if _, err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if c.Tail("sat-b") != "You are in area Bad and generic commands target it." || c.Tail("sat-a") != "You are in area Flur and generic commands target it." {
		t.Errorf("tails: a=%q b=%q", c.Tail("sat-a"), c.Tail("sat-b"))
	}
	if c.Text() != "Static Context: Küche\nWhen controlling Home Assistant always call the intent tools." {
		t.Errorf("layer 4 changed: %q", c.Text())
	}
}

// Without any satellite the whole prompt is layer 4, as before.
func TestWithoutSatellitesTheWholePromptIsLayerFour(t *testing.T) {
	src := newPerDevice()
	c := New(Options{Source: src, Server: "ha"})
	c.Refresh(context.Background())
	if want := src.start + "\n" + src.tails[""]; c.Text() != want || c.Tail("") != "" {
		t.Errorf("layer 4 = %q, tail = %q", c.Text(), c.Tail(""))
	}
}

// HP-02 A failed fetch is a problem until the prompt comes
func TestHP02_AFailedFetchIsAProblemUntilThePromptComes(t *testing.T) {
	src := &source{err: errors.New("Unknown prompt: Assist")}
	c := New(Options{Source: src, Server: "ha"})
	if c.Problem() != nil {
		t.Fatal("a problem before the first fetch")
	}
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("error swallowed")
	}
	if err := c.Problem(); err == nil || err.Error() != "Unknown prompt: Assist" {
		t.Fatalf("problem = %v", err)
	}
	src.text, src.err = "", nil
	if _, err := c.Refresh(context.Background()); err == nil || c.Problem() == nil {
		t.Fatalf("an empty prompt is no prompt: %v, %v", err, c.Problem())
	}
	src.text = "Static Context: Küche"
	if _, err := c.Refresh(context.Background()); err != nil || c.Problem() != nil {
		t.Fatalf("the prompt came: %v, problem %v", err, c.Problem())
	}
}

// HP-04 A short outage keeps the last prompt
func TestHP04_AShortOutageKeepsTheLastPrompt(t *testing.T) {
	src := &source{text: "Static Context: Küche"}
	c := New(Options{Source: src, Server: "ha"})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("mcp down")
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("error swallowed")
	}
	if c.Text() != "Static Context: Küche" || c.Problem() != nil {
		t.Fatalf("text %q, problem %v", c.Text(), c.Problem())
	}
}

// Versions that are all the same are layer 4 whole, a prompt of one line too.
func TestSameVersionsAreSharedWhole(t *testing.T) {
	c := New(Options{Source: &source{text: "Static Context: Küche"}, Server: "ha", Devices: func() []string { return []string{"sat-a"} }})
	if _, err := c.Refresh(context.Background()); err != nil || c.Text() != "Static Context: Küche" || c.Tail("sat-a") != "" {
		t.Fatalf("text %q, tail %q, %v", c.Text(), c.Tail("sat-a"), err)
	}
}

// FB-06 A satellite whose prompt was never fetched fetches it on its turn
func TestFB06_ASatelliteWhosePromptWasNeverFetchedFetchesItOnItsTurn(t *testing.T) {
	// Given the satellite's version failed when the prompt was fetched
	p := newPerDevice()
	p.broken = map[string]bool{"sat-a": true}
	c := New(Options{Source: p, Server: "ha", Devices: func() []string { return []string{"sat-a", "sat-b"} }})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// When its turn comes and it still fails, then it is an error, not the version without a device
	if tail, err := c.TailFor(context.Background(), "sat-a"); err == nil {
		t.Fatalf("tail without an error: %q", tail)
	}
	// When it works again, the turn gets the satellite's own lines
	p.broken = nil
	tail, err := c.TailFor(context.Background(), "sat-a")
	if err != nil || tail != "You are in area Küche and generic commands target it." {
		t.Errorf("tail = %q, %v", tail, err)
	}
}

// AD-02 A device that can start timers where no compared one can gets its own
// lines (docs/spec/context.md, 1.1, decision 3; issue 6: the companion app).
func TestAD02_ADeviceThatCanStartTimersWhereNoComparedOneCanGetsItsOwnLines(t *testing.T) {
	// Given every compared version ends with "This device is not able to
	// start timers." (no satellite: the version without a device is all)
	p := newPerDevice()
	p.tails["phone"] = "When a user asks to turn on all devices, ask for an area."
	asked := 0
	c := New(Options{Source: p, Server: "ha", Devices: func() []string { return nil }, OnAsked: func() { asked++ }})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(c.Text(), "This device is not able to start timers.") {
		t.Fatalf("layer 4 = %q", c.Text())
	}
	// When a turn comes from a device that is not compared and whose version
	// lacks that line, then all versions are fetched again with it, and the
	// turn gets its own lines
	tail, err := c.TailFor(context.Background(), "phone")
	if err != nil || tail != "When a user asks to turn on all devices, ask for an area." {
		t.Fatalf("tail = %q, %v", tail, err)
	}
	if asked != 1 || !slices.Equal(c.Asked(), []string{"phone"}) {
		t.Errorf("OnAsked ran %d times, asked %v", asked, c.Asked())
	}
	// And part 1 ends before the timer line from then on, also after the
	// next refresh
	if c.Text() != p.start {
		t.Errorf("layer 4 = %q", c.Text())
	}
	if c.Tail("") != p.tails[""] {
		t.Errorf("tail without a device = %q", c.Tail(""))
	}
	p.asked = nil
	if _, err := c.Refresh(context.Background()); err != nil || c.Text() != p.start || !slices.Contains(p.asked, "phone") {
		t.Errorf("after refresh layer 4 = %q, fetched %v, %v", c.Text(), p.asked, err)
	}
	if _, err := c.TailFor(context.Background(), "phone"); err != nil || asked != 1 {
		t.Errorf("second turn: %v, OnAsked ran %d times", err, asked)
	}
}

// AD-03 The fetch of all versions fails on that turn.
func TestAD03_TheFetchOfAllVersionsFailsOnThatTurn(t *testing.T) {
	p := newPerDevice()
	p.tails["phone"] = "When a user asks to turn on all devices, ask for an area."
	c := New(Options{Source: p, Server: "ha", Devices: func() []string { return nil }})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Given a turn comes from a device that is not compared and whose
	// version does not begin with part 1, when fetching all versions again
	// fails
	p.broken = map[string]bool{"": true}
	tail, err := c.TailFor(context.Background(), "phone")
	// Then the turn is refused and the error names the failed fetch
	if err == nil || !strings.Contains(err.Error(), "fetching all versions again failed: down") ||
		!strings.Contains(err.Error(), "phone") {
		t.Errorf("tail = %q, %v", tail, err)
	}
	// It is compared from then on: the next turn tries again and works.
	p.broken = nil
	if tail, err := c.TailFor(context.Background(), "phone"); err != nil || tail != p.tails["phone"] {
		t.Errorf("next turn: tail = %q, %v", tail, err)
	}
}

// AD-04 An asking device whose version fits stays in the comparison.
func TestAD04_AnAskingDeviceWhoseVersionFitsStaysInTheComparison(t *testing.T) {
	p := newPerDevice()
	p.tails["tablet"] = "You are in area Flur and generic commands target it."
	c := New(Options{Source: p, Server: "ha", Devices: func() []string { return []string{"sat-a", "sat-b"} }})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Given a turn comes from a device that is not compared and whose
	// version begins with part 1, then the turn gets the rest as its lines
	tail, err := c.TailFor(context.Background(), "tablet")
	if err != nil || tail != p.tails["tablet"] {
		t.Fatalf("tail = %q, %v", tail, err)
	}
	// And its version is fetched with every refresh: its new area follows.
	p.tails["tablet"] = "You are in area Küche and generic commands target it."
	if _, err := c.Refresh(context.Background()); err != nil || c.Tail("tablet") != p.tails["tablet"] {
		t.Errorf("after refresh tail = %q, %v", c.Tail("tablet"), err)
	}
}
