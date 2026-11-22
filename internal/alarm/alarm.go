// Package alarm is Mira's alarm: "weck mich morgen um 6" at any satellite
// (docs/spec/alarm.md). Mira does not keep the alarm. She puts an automation
// into Home Assistant, which rings it – even when Mira is off: it sets the
// satellite's speaker to 90 %, plays the wake sound round by round until the
// satellite listens (someone reacted) or ten minutes are over, and sets the
// volume back. Mira builds the automation (config.go), has the integration
// check it (mira/automation/validate), saves it with the label "Mira: Wecker",
// deletes a once-alarm after it rang, and cancels a ringing alarm before
// deleting it.
package alarm

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/ha/automation"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Tool is the alarm's tool.
const Tool = "alarm"

// CancelEvent stops a ringing alarm's run; its data names the automation.
const CancelEvent = "mira_alarm_cancel"

// Speaker features an alarm needs: setting the volume and playing as an
// announcement (Home Assistant's MediaPlayerEntityFeature).
const (
	featureVolumeSet     = 4
	featureMediaAnnounce = 1048576
)

// days are the weekdays as Home Assistant's time condition names them.
var days = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// HA is the part of the Home Assistant client the alarms use.
type HA interface {
	automation.HA
}

type Options struct {
	Lang   i18n.Lang
	HA     HA
	Own    automation.Own
	Logger *slog.Logger
	// Watch follows the states of these automations and calls changed on
	// every change, without blocking; stop ends it.
	Watch    func(ctx context.Context, entities []string, changed func()) (stop func(), err error)
	Clock    func() time.Time
	Location *time.Location
	// Speaker is the media_player of a satellite's device; "" if it has none.
	Speaker func(satellite string) string
	// Lights are the exposed lights in an area.
	Lights func(area string) []string
	// Music finds what an alarm wakes with in an area (decision 10): name and
	// kind (radio, playlist, album, artist, track, or "" for any), on the
	// area's Music Assistant player where there is one, else a station on the
	// speaker. nil and the names found when none is meant for sure;
	// ErrNeedsMusicAssistant for another kind than a station without Music
	// Assistant; another error when searching fails.
	Music func(ctx context.Context, area, speaker, name, kind string) (*Music, []string, error)
	// Features are a media player's supported_features.
	Features func(ctx context.Context, entity string) (int, error)
	// SoundURL is where a satellite fetches one of Mira's sounds.
	SoundURL func(ctx context.Context, name string) string
	// AreaName is the display name of an area id.
	AreaName func(area string) string
	// Forbidden is what the integration refuses in an alarm's actions.
	Forbidden func() map[string]any
	// Settle is how long a deletion waits for a cancelled alarm's run to end;
	// a safety net only, the signal is the automation's state. 0: 10 s.
	Settle time.Duration
	// NewID makes an automation's id; nil: mira_ and 12 random hex digits.
	NewID func() string
}

// At most this many alarms for one person in one room.
const maxPerOwnerArea = 10

// Alarm is one of Mira's alarms, as the integration lists it.
type Alarm struct {
	ID       string   `json:"id"`
	EntityID string   `json:"entity_id"`
	Title    string   `json:"title"`
	Time     string   `json:"time"`
	Days     []string `json:"days"`
	Date     string   `json:"date"` // a once-alarm's day
	Music    string   `json:"music"`
	Light    bool     `json:"light"`
	Owner    string   `json:"owner"`
	Area     string   `json:"area"`
	Ran      bool     `json:"ran"`
	Running  int      `json:"running"`
}

// Alarms are the alarms: the tool, and the loop that deletes a once-alarm
// after it rang.
type Alarms struct{ c *controller }

type controller struct {
	o     Options
	t     texts
	log   *slog.Logger
	pokes chan struct{}
	// Only Run's tidy touches these.
	noIntegration bool
}

// New makes the alarms; Run tidies them up.
func New(o Options) *Alarms {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Settle == 0 {
		o.Settle = 10 * time.Second
	}
	if o.NewID == nil {
		o.NewID = func() string {
			b := make([]byte, 6)
			rand.Read(b)
			return "mira_" + hex.EncodeToString(b)
		}
	}
	return &Alarms{c: &controller{o: o, t: i18n.Pick(catalog, o.Lang), log: o.Logger.With("component", "alarm"),
		pokes: make(chan struct{}, 1)}}
}

// Tools is the alarm's tool.
func (s *Alarms) Tools() []tools.InternalTool {
	c, t := s.c, s.c.t
	return []tools.InternalTool{{
		Name: Tool, Description: t.desc,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"action":{"type":"string","enum":["list","delete"],"description":%q},"id":{"type":"string","description":%q}}}`,
			t.actionDesc, t.idDesc),
		Run: c.run,
	}}
}

func (c *controller) run(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct{ Action, ID string }
	if len(inv.Args) > 0 {
		if err := json.Unmarshal(inv.Args, &a); err != nil {
			return "", fmt.Errorf("arguments %q: %w", excerpt.Bytes(inv.Args), err)
		}
	}
	switch a.Action {
	case "", "list":
		return c.list(ctx, inv)
	case "delete":
		return c.delete(ctx, inv, a.ID)
	}
	return c.t.badAction, nil
}

var hhmm = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

// The building blocks the skill wecker's thread names (docs/spec/alarm.md,
// decision 16): its finish sets the alarm, its undo takes back the one just
// set ("Nein, halb sieben", thread_undo).
const (
	ActionSet           = "alarm.set"
	ActionDeleteCreated = "alarm.delete_created"
)

// ErrNeedsMusicAssistant: music other than a station was asked for in a room
// without Music Assistant (decision 10).
var ErrNeedsMusicAssistant = errors.New("music other than a station needs Music Assistant")

// Refusal is the building block's no: nothing was set, and Text says why,
// for the model.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return r.Text }

// SetAction is the building block alarm.set: time, days, and the place as
// the thread fills it in (satellite, area, person).
func (s *Alarms) SetAction(ctx context.Context, data map[string]any) error {
	str := func(k string) string { v, _ := data[k].(string); return strings.TrimSpace(v) }
	return s.c.set(ctx, request{person: str("person"), area: str("area"), satellite: str("satellite"),
		time: str("time"), days: str("days"), music: str("music"), kind: str("music_kind"), light: wants(str("light"))})
}

// DeleteCreated is the building block alarm.delete_created: the newest alarm
// of the person (anonymous: of the room) with that time and those days.
func (s *Alarms) DeleteCreated(ctx context.Context, data map[string]any) error {
	c := s.c
	str := func(k string) string { v, _ := data[k].(string); return strings.TrimSpace(v) }
	at, ok := clockOf(str("time"))
	on, okDays := parseDays(str("days"))
	if !ok || !okDays {
		return fmt.Errorf("alarm.delete_created: time %q, days %q not understood", str("time"), str("days"))
	}
	mine, err := c.mine(ctx, tools.Invocation{Person: str("person"), Area: str("area")})
	if err != nil {
		return err
	}
	for i := len(mine) - 1; i >= 0; i-- {
		al := mine[i]
		if al.Time == at && slices.Equal(al.Days, on) {
			if al.Running > 0 {
				c.cancel(ctx, al)
			}
			if err := automation.Delete(ctx, c.o.HA, c.o.Own, c.log, al.ID); err != nil {
				return err
			}
			c.Poke()
			return nil
		}
	}
	return fmt.Errorf("alarm.delete_created: no alarm at %s %v", at, on)
}

func clockOf(s string) (string, bool) {
	m := hhmm.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	return fmt.Sprintf("%02s:%s", m[1], m[2]), true
}

// request is what the thread of wecker hands alarm.set.
type request struct {
	person, area, satellite, time, days, music, kind string
	light                                            bool
}

// wants reads the field light: set for yes, empty or a "-" (not needed) for no.
func wants(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "-", "no", "nein", "false", "off", "aus":
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(s), "-")
}

func (c *controller) set(ctx context.Context, r request) error {
	person, area, satellite := r.person, r.area, r.satellite
	if satellite == "" {
		return &Refusal{c.t.noSatellite}
	}
	at, ok := clockOf(r.time)
	if !ok {
		return &Refusal{fmt.Sprintf(c.t.badTime, r.time)}
	}
	on, ok := parseDays(r.days)
	if !ok {
		return &Refusal{fmt.Sprintf(c.t.badDays, r.days)}
	}
	speaker := c.o.Speaker(satellite)
	if speaker == "" {
		return &Refusal{c.t.cannot}
	}
	features, err := c.o.Features(ctx, speaker)
	if err != nil {
		c.log.Warn("could not read what the satellite's speaker can; no alarm set", "speaker", speaker, "error", err)
		return &Refusal{fmt.Sprintf(c.t.failed, err)}
	}
	if features&featureVolumeSet == 0 || features&featureMediaAnnounce == 0 {
		c.log.Info("the satellite's speaker cannot set its volume or announce; no alarm", "speaker", speaker, "features", features)
		return &Refusal{c.t.cannot}
	}
	room := c.o.AreaName(area)
	if room == "" {
		room = area
	}
	mine, err := c.mine(ctx, tools.Invocation{Person: person, Area: area})
	if err != nil {
		c.log.Warn("could not read the alarms before setting one; no alarm set", "error", err)
		return &Refusal{c.failed(err)}
	}
	// The same alarm again is no second one (decision 18), as a TECHO5's
	// alarm_set turns on the one it has: a model that starts the thread
	// twice sets one alarm.
	for _, al := range mine {
		if al.Time == at && slices.Equal(al.Days, on) {
			c.log.Info("the same alarm is already set; no second one", "automation", al.ID, "time", at, "days", on, "area", area)
			return nil
		}
	}
	if len(mine) >= maxPerOwnerArea {
		return &Refusal{fmt.Sprintf(c.t.tooMany, maxPerOwnerArea)}
	}
	sp := spec{
		ID: c.o.NewID(), Title: fmt.Sprintf(c.t.title, room), Time: at, Days: on, Owner: person, Area: area,
		Satellite: satellite, Speaker: speaker,
		RiseURL: c.o.SoundURL(ctx, "wake_rise"), LoopURL: c.o.SoundURL(ctx, "wake_loop"),
		FailedText: fmt.Sprintf(c.t.couldNotRing, c.when(at, on), room),
	}
	if len(on) == 0 {
		sp.Date = c.next(at).Format("2006-01-02")
	}
	if r.light {
		if sp.Lights = c.o.Lights(area); len(sp.Lights) == 0 {
			return &Refusal{fmt.Sprintf(c.t.noLight, room)}
		}
		sp.LightAt = lightAt(at)
	}
	if name := strings.TrimSpace(r.music); name != "" && !strings.HasPrefix(name, "-") {
		kind := strings.ToLower(strings.TrimSpace(r.kind))
		if strings.HasPrefix(kind, "-") {
			kind = ""
		}
		m, titles, err := c.o.Music(ctx, area, speaker, name, kind)
		switch {
		case errors.Is(err, ErrNeedsMusicAssistant):
			return &Refusal{fmt.Sprintf(c.t.needsMusicAssistant, name, room)}
		case err != nil:
			c.log.Warn("could not search the music for an alarm; no alarm set", "music", name, "kind", kind, "error", err)
			return &Refusal{fmt.Sprintf(c.t.musicUnavailable, name, err)}
		case m == nil && len(titles) == 0:
			return &Refusal{fmt.Sprintf(c.t.noMusic, name)}
		case m == nil:
			return &Refusal{fmt.Sprintf(c.t.severalMusic, name, strings.Join(titles, "; "))}
		}
		sp.Music = m
	}
	id := sp.ID
	config := build(sp)
	answer, err := c.o.HA.Call(ctx, map[string]any{"type": "mira/automation/validate", "config": config, "forbidden": c.forbidden()})
	if err != nil {
		c.log.Warn("the integration did not check the alarm; no alarm set", "time", at, "days", on, "error", err)
		return &Refusal{c.failed(err)}
	}
	var checked struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(answer, &checked); err != nil || !checked.Valid {
		c.log.Warn("the integration refused the alarm's automation; no alarm set", "answer", excerpt.Bytes(answer), "error", err)
		return &Refusal{fmt.Sprintf(c.t.failed, cmp.Or(checked.Error, "unexpected answer of the integration"))}
	}
	if err := automation.Save(ctx, c.o.HA, c.o.Own, c.log, id, config, automation.LabelAlarm); err != nil {
		return &Refusal{c.failed(err)}
	}
	c.log.Info("alarm set", "automation", id, "time", at, "days", on, "date", sp.Date, "area", area, "satellite", satellite,
		"music", sp.Music != nil, "light", len(sp.Lights) > 0)
	c.Poke()
	return nil
}

// when says when an alarm rings: "morgen (Mo., 23.11.) um 06:00" for a
// once-alarm, "Mo, Di um 06:00" for one that repeats.
func (c *controller) when(at string, on []string) string {
	if len(on) > 0 {
		names := make([]string, len(on))
		for i, d := range on {
			names[i] = c.dayName(slices.Index(days, d))
		}
		if len(on) == 7 {
			return fmt.Sprintf(c.t.daily, at)
		}
		return fmt.Sprintf(c.t.repeats, strings.Join(names, ", "), at)
	}
	next := c.next(at)
	day := c.t.today
	if now := c.o.Clock().In(c.o.Location); next.Day() != now.Day() {
		day = c.t.tomorrow
	}
	wd := (int(next.Weekday()) + 6) % 7
	return fmt.Sprintf(c.t.once, day, c.dayName(wd), next.Day(), int(next.Month()), at)
}

// next is the next time the clock shows at: today, or tomorrow when it has
// passed.
func (c *controller) next(at string) time.Time {
	now := c.o.Clock().In(c.o.Location)
	h, m := 0, 0
	fmt.Sscanf(at, "%d:%d", &h, &m)
	next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, c.o.Location)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// lightAt is when the light starts: lightLead minutes before the alarm, but
// not before midnight, so that it falls on the alarm's day (its weekday
// condition, a once-alarm's date).
func lightAt(at string) string {
	h, m := 0, 0
	fmt.Sscanf(at, "%d:%d", &h, &m)
	start := max(h*60+m-lightLead, 0)
	return fmt.Sprintf("%02d:%02d", start/60, start%60)
}

// parseDays reads days: empty or "once" is a once-alarm (nil); "daily",
// "weekdays", "weekends", or days by their English names or prefixes.
func parseDays(s string) ([]string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "once":
		return nil, true
	case "daily", "every day", "everyday":
		return slices.Clone(days), true
	case "weekdays":
		return slices.Clone(days[:5]), true
	case "weekends":
		return slices.Clone(days[5:]), true
	}
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		found := ""
		for _, d := range days {
			if len(f) >= 2 && strings.HasPrefix(d, f[:min(len(f), 3)]) && strings.HasPrefix(dayLong[d], f) {
				found = d
			}
		}
		if found == "" {
			return nil, false
		}
		seen[found] = true
	}
	var out []string
	for _, d := range days {
		if seen[d] {
			out = append(out, d)
		}
	}
	return out, len(out) > 0
}

var dayLong = map[string]string{"mon": "monday", "tue": "tuesday", "wed": "wednesday", "thu": "thursday",
	"fri": "friday", "sat": "saturday", "sun": "sunday"}

func (c *controller) list(ctx context.Context, inv tools.Invocation) (string, error) {
	mine, err := c.mine(ctx, inv)
	if err != nil {
		return c.failed(err), nil
	}
	if len(mine) == 0 {
		return c.t.none, nil
	}
	var b strings.Builder
	for _, al := range mine {
		fmt.Fprintf(&b, "- "+c.t.listLine+"%s\n", al.ID, c.when(al.Time, al.Days), c.o.AreaName(al.Area), c.with(al))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c *controller) delete(ctx context.Context, inv tools.Invocation, id string) (string, error) {
	mine, err := c.mine(ctx, inv)
	if err != nil {
		return c.failed(err), nil
	}
	var al *Alarm
	switch {
	case id != "":
		for i := range mine {
			if mine[i].ID == id {
				al = &mine[i]
			}
		}
	case len(mine) == 1:
		al = &mine[0]
	case len(mine) > 1:
		var names []string
		for _, a := range mine {
			names = append(names, fmt.Sprintf("%s (%s, %s)", a.ID, c.when(a.Time, a.Days), c.o.AreaName(a.Area)))
		}
		return fmt.Sprintf(c.t.several, strings.Join(names, "; ")), nil
	}
	if al == nil {
		return c.t.noMatch, nil
	}
	if al.Running > 0 {
		c.cancel(ctx, *al)
	}
	if err := automation.Delete(ctx, c.o.HA, c.o.Own, c.log, al.ID); err != nil && c.still(ctx, al.ID) {
		return c.failed(err), nil
	}
	c.Poke()
	return c.t.deleted, nil
}

// cancel ends a ringing alarm's run, which stops the sound and sets the
// volume back (decision 15): the event, then the automation's state until
// its run is over. Deleting goes on after Settle with a log line if no
// end is seen; the volume then stays where the run left it.
func (c *controller) cancel(ctx context.Context, al Alarm) {
	// The subscription's handler runs on the connection's read loop: it only
	// passes the change on; the check below reads the alarms.
	changed := make(chan struct{}, 1)
	signal := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	over := func() bool {
		all, err := c.all(ctx)
		if err != nil {
			return false
		}
		for _, a := range all {
			if a.ID == al.ID {
				return a.Running == 0
			}
		}
		return true
	}
	stop := func() {}
	if c.o.Watch != nil && al.EntityID != "" {
		if s, err := c.o.Watch(ctx, []string{al.EntityID}, signal); err != nil {
			c.log.Warn("could not watch a ringing alarm while cancelling it", "automation", al.ID, "error", err)
		} else {
			stop = s
		}
	}
	defer stop()
	if _, err := c.o.HA.Call(ctx, map[string]any{"type": "fire_event", "event_type": CancelEvent,
		"event_data": map[string]any{"automation": al.ID}}); err != nil {
		c.log.Warn("could not cancel a ringing alarm; deleting it ends its run, the volume stays at 90 %", "automation", al.ID, "error", err)
		return
	}
	deadline := time.After(c.o.Settle)
	for !over() {
		select {
		case <-changed:
		case <-deadline:
			c.log.Warn("a cancelled alarm's run did not end; deleting it all the same, the volume may stay at 90 %", "automation", al.ID, "waited", c.o.Settle)
			return
		case <-ctx.Done():
			return
		}
	}
}

// with names what an alarm wakes with besides the wake sound: ", mit SWR3, mit Licht".
func (c *controller) with(al Alarm) string {
	out := ""
	if al.Music != "" {
		out += ", " + fmt.Sprintf(c.t.withMusic, al.Music)
	}
	if al.Light {
		out += ", " + c.t.withLight
	}
	return out
}

// still says whether an alarm is still there after deleting it failed: a
// cancelled once-alarm has rung, and the tidy loop may delete it first
// (seen 2026-11-22: Home Assistant answered "Resource not found"). If the
// alarms cannot be read, it counts as there.
func (c *controller) still(ctx context.Context, id string) bool {
	all, err := c.all(ctx)
	if err != nil {
		return true
	}
	for _, a := range all {
		if a.ID == id {
			return true
		}
	}
	c.log.Info("the alarm to delete is already gone; deleted by the tidy loop after it rang", "automation", id)
	return false
}

// rang says whether a once-alarm is over: its time has passed and no run of
// it is running (the light's run before it is no ring). One that never ran
// (Home Assistant was off: decision 12, it does not ring later) is over a
// minute after its time.
func (c *controller) rang(al Alarm) bool {
	if len(al.Days) > 0 || al.Running > 0 {
		return false
	}
	if al.Date == "" {
		return al.Ran
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", al.Date+" "+al.Time, c.o.Location)
	if err != nil {
		return false
	}
	if !al.Ran {
		at = at.Add(time.Minute)
	}
	return !c.o.Clock().Before(at)
}

// mine are the alarms of the person (anonymous: of the room), by time.
func (c *controller) mine(ctx context.Context, inv tools.Invocation) ([]Alarm, error) {
	all, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Alarm
	for _, al := range all {
		if c.rang(al) {
			continue // a once-alarm that rang, about to be deleted
		}
		if al.Owner == inv.Person && (inv.Person != "" || al.Area == inv.Area) {
			out = append(out, al)
		}
	}
	slices.SortFunc(out, func(a, b Alarm) int { return strings.Compare(a.Time, b.Time) })
	return out, nil
}

// all are the automations labelled "Mira: Wecker" that are alarms, with what
// their variables say.
func (c *controller) all(ctx context.Context) ([]Alarm, error) {
	raw, err := c.o.HA.Call(ctx, map[string]any{"type": "mira/automation/list", "label": automation.LabelAlarm})
	if err != nil {
		return nil, err
	}
	var res struct {
		Automations []struct {
			ID            string `json:"id"`
			EntityID      string `json:"entity_id"`
			Current       int    `json:"current"`
			LastTriggered string `json:"last_triggered"`
			Config        struct {
				Alias     string            `json:"alias"`
				Variables map[string]string `json:"variables"`
			} `json:"config"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	var out []Alarm
	for _, a := range res.Automations {
		v := a.Config.Variables
		if v["mira_kind"] != kindAlarm {
			continue
		}
		var on []string
		if v["mira_days"] != "" {
			on = strings.Split(v["mira_days"], ",")
		}
		out = append(out, Alarm{ID: a.ID, EntityID: a.EntityID, Title: cmp.Or(v["mira_title"], a.Config.Alias),
			Time: v["mira_time"], Days: on, Date: v["mira_date"], Music: v["mira_music"], Light: v["mira_light"] == "on",
			Owner: v["mira_owner"], Area: v["mira_area"],
			Ran: a.LastTriggered != "", Running: a.Current})
	}
	return out, nil
}

func (c *controller) forbidden() map[string]any {
	if c.o.Forbidden == nil {
		return map[string]any{"domains": []string{}, "entities": []string{}}
	}
	return c.o.Forbidden()
}

// failed is the tool's answer when Home Assistant or the integration refused.
func (c *controller) failed(err error) string {
	if errors.Is(err, haws.ErrNotAdmin) {
		c.log.Warn("home assistant refused mira's token for an alarm; the repair for an access is open", "error", err)
		return c.t.needAccess
	}
	return fmt.Sprintf(c.t.failed, err)
}
