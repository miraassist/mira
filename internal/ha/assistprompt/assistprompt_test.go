package assistprompt

import (
	"context"
	"errors"
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
