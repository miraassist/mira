package music

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Ducking: while a satellite listens, Mira lowers the Music Assistant players
// that play in its area, and sets them back when it is idle again
// (docs/spec/music-players.md, part 7). It is no tool: nothing changes in what
// the model is told.

// DuckOptions are what ducking needs from the app.
type DuckOptions struct {
	// Players are the exposed Music Assistant players now, with their area
	// and device.
	Players func() []Player
	// Satellite is an assist_satellite's area (its entity's, else its
	// device's; "" for none) and its device ("" for none).
	Satellite func(entity string) (area, device string)
	// Named is the entities a name of a tool call reaches (in the area, when
	// one is given), as the house structure resolves names and aliases.
	Named func(name, area string) []string
	// Area is the area id of an area's id, name or alias; "" for none.
	Area func(name string) string
	// Watch subscribes to the states of the satellites (filtered to the
	// domain assist_satellite). changed runs on the connection's read loop
	// and does not block; states are the satellites' states now; stop ends
	// the subscription.
	Watch func(ctx context.Context, changed func()) (states func() map[string]string, stop func(), err error)
	// Volumes reads the state and volume of these players (one filtered
	// read).
	Volumes func(ctx context.Context, entities []string) (map[string]Volume, error)
	// SetVolume is media_player.volume_set on a player.
	SetVolume func(ctx context.Context, entity string, level float64) error
	Logger    *slog.Logger
}

// Volume is a player's state and its volume_level; Known is false when the
// player has no volume_level.
type Volume struct {
	State string
	Level float64
	Known bool
}

// VolumeCall is a call of one of Home Assistant's volume tools
// (HassSetVolume, HassSetVolumeRelative) that succeeded: the area of the turn
// it came from, and its arguments name, area and floor ("" for none).
type VolumeCall struct {
	TurnArea, Name, Area, Floor string
}

// lowered is a player Mira lowered: from its volume to hers.
type lowered struct{ from, to float64 }

// ducked is an area while one of its satellites is in a turn: the satellites
// in it, the players Mira lowered, and those a volume tool call of the turn
// set (they keep their volume).
type ducked struct {
	satellites map[string]bool
	players    map[string]lowered
	kept       map[string]bool
}

// Ducker lowers and restores. Its state lives in its own goroutine (Run);
// Update is exported for the tests. VolumeSet is called from the turns.
type Ducker struct {
	o      DuckOptions
	log    *slog.Logger
	signal chan struct{}
	mu     sync.Mutex
	calls  []VolumeCall // the volume tool calls not yet taken in

	seen  map[string]string  // the satellites' last states
	in    map[string]string  // an active satellite's area
	areas map[string]*ducked // the areas while a satellite is in a turn
}

// NewDucker makes a Ducker.
func NewDucker(o DuckOptions) *Ducker {
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Ducker{o: o, log: log, signal: make(chan struct{}, 1),
		seen: map[string]string{}, in: map[string]string{}, areas: map[string]*ducked{}}
}

// wake signals the goroutine; signals that come while it works fold into one.
func (d *Ducker) wake() {
	select {
	case d.signal <- struct{}{}:
	default:
	}
}

// VolumeSet tells the ducker of a volume tool call of a turn. It never
// blocks: the call is noted and taken in by the goroutine, before the next
// states.
func (d *Ducker) VolumeSet(c VolumeCall) {
	d.mu.Lock()
	d.calls = append(d.calls, c)
	d.mu.Unlock()
	d.wake()
}

// Run watches the satellites until ctx ends. The subscription's handler only
// signals; the states are read and the players called here.
func (d *Ducker) Run(ctx context.Context) error {
	states, stop, err := d.o.Watch(ctx, d.wake)
	if err != nil {
		return fmt.Errorf("ducking: cannot watch the satellites: %w", err)
	}
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.signal:
			d.Update(ctx, states())
		}
	}
}

// active is a satellite's state during a turn: from listening to the end of
// its answer and the follow-up question (row D1).
func active(state string) bool {
	return state == "listening" || state == "processing" || state == "responding"
}

// Update takes the volume tool calls noted so far, then the satellites'
// states now. A satellite seen for the first time is only recorded.
func (d *Ducker) Update(ctx context.Context, states map[string]string) {
	d.mu.Lock()
	calls := d.calls
	d.calls = nil
	d.mu.Unlock()
	for _, c := range calls {
		d.keep(c)
	}
	for _, sat := range slices.Sorted(maps.Keys(states)) {
		prev, known := d.seen[sat]
		now := states[sat]
		d.seen[sat] = now
		switch {
		case !known:
		case !active(prev) && active(now):
			d.listen(ctx, sat)
		case active(prev) && !active(now):
			d.idle(ctx, sat)
		}
	}
	for sat, prev := range d.seen {
		if _, ok := states[sat]; !ok {
			delete(d.seen, sat)
			if active(prev) {
				d.idle(ctx, sat)
			}
		}
	}
}

// keep marks the lowered players a volume tool call reached: they are not
// set back when the turn ends (decision 35). Only a call of a turn from a
// ducked area counts. A name reaches the lowered players it names; a floor
// every lowered player (the house structure knows no floors); an area the
// lowered players of that area; no target those of the turn's area, as Home
// Assistant takes the satellite's area.
func (d *Ducker) keep(c VolumeCall) {
	dk := d.areas[c.TurnArea]
	if c.TurnArea == "" || dk == nil {
		return
	}
	var reached []*ducked
	var named []string
	switch {
	case c.Name != "":
		named = d.o.Named(c.Name, c.Area)
		for _, a := range d.areas {
			reached = append(reached, a)
		}
	case c.Floor != "":
		for _, a := range d.areas {
			reached = append(reached, a)
		}
	case c.Area != "":
		if a := d.areas[d.o.Area(c.Area)]; a != nil {
			reached = append(reached, a)
		}
	default:
		reached = append(reached, dk)
	}
	var kept []string
	for _, a := range reached {
		for id := range a.players {
			if c.Name != "" && !slices.Contains(named, id) {
				continue
			}
			a.kept[id] = true
			kept = append(kept, id)
		}
	}
	sort.Strings(kept)
	d.log.Info("ducking: a volume command of the turn; these players keep their new volume",
		"area", c.TurnArea, "name", c.Name, "target_area", c.Area, "floor", c.Floor, "players", strings.Join(kept, ","))
}

// lowerTo is how far a player goes down: to a third, in whole percent, at
// least 1 %.
func lowerTo(level float64) float64 {
	return math.Max(0.01, math.Round(level*100/3)/100)
}

// onSatellite says whether a player plays on the satellite itself, whose
// firmware lowers its own playback: for now only a player of the satellite's
// device. A Music Assistant player is mostly a device of its own (the Show's
// on the test HA), so this is open (docs/spec/music-players.md, part 7,
// Open); a later rule is one more check here.
func onSatellite(p Player, satelliteDevice string) bool {
	return satelliteDevice != "" && p.Device == satelliteDevice
}

// listen lowers the players that play in the satellite's area, unless
// another satellite there is in a turn already.
func (d *Ducker) listen(ctx context.Context, sat string) {
	area, device := d.o.Satellite(sat)
	if area == "" {
		return
	}
	d.in[sat] = area
	if dk := d.areas[area]; dk != nil {
		dk.satellites[sat] = true
		return
	}
	dk := &ducked{satellites: map[string]bool{sat: true}, players: map[string]lowered{}, kept: map[string]bool{}}
	d.areas[area] = dk
	var ids []string
	for _, p := range d.o.Players() {
		if p.Area == area && !onSatellite(p, device) {
			ids = append(ids, p.Entity)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	vols, err := d.o.Volumes(ctx, ids)
	if err != nil {
		d.log.Warn("ducking: cannot read the players' states; nothing is lowered for this turn",
			"satellite", sat, "area", area, "players", strings.Join(ids, ","), "error", err)
		return
	}
	var done []string
	for _, id := range ids {
		v := vols[id]
		if v.State != "playing" || !v.Known {
			continue
		}
		to := lowerTo(v.Level)
		if to >= v.Level {
			continue
		}
		if err := d.o.SetVolume(ctx, id, to); err != nil {
			d.log.Warn("ducking: home assistant refused to lower a player; it stays as it is",
				"player", id, "volume", v.Level, "to", to, "error", err)
			continue
		}
		dk.players[id] = lowered{from: v.Level, to: to}
		done = append(done, fmt.Sprintf("%s %.2f->%.2f", id, v.Level, to))
	}
	if len(done) > 0 {
		d.log.Info("music lowered while a satellite listens", "satellite", sat, "area", area, "players", strings.Join(done, ", "))
	}
}

// idle sets the lowered players back once no satellite of the area is in a
// turn, without reading them: all but those a volume tool call of the turn
// reached (decision 35).
func (d *Ducker) idle(ctx context.Context, sat string) {
	area, ok := d.in[sat]
	if !ok {
		return
	}
	delete(d.in, sat)
	dk := d.areas[area]
	if dk == nil {
		return
	}
	delete(dk.satellites, sat)
	if len(dk.satellites) > 0 {
		return
	}
	delete(d.areas, area)
	var back, left []string
	for _, id := range slices.Sorted(maps.Keys(dk.players)) {
		l := dk.players[id]
		if dk.kept[id] {
			left = append(left, id)
			continue
		}
		if err := d.o.SetVolume(ctx, id, l.from); err != nil {
			d.log.Warn("ducking: home assistant refused to set a player back; it stays lowered",
				"player", id, "volume", l.from, "error", err)
			continue
		}
		back = append(back, fmt.Sprintf("%s %.2f", id, l.from))
	}
	if len(back) > 0 {
		d.log.Info("music set back after the turn", "area", area, "players", strings.Join(back, ", "))
	}
	if len(left) > 0 {
		d.log.Info("music left at the volume set by a volume command of the turn", "area", area, "players", strings.Join(left, ", "))
	}
}
