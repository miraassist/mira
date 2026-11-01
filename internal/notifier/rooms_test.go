package notifier

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
)

// Several satellites in a room (docs/spec/satellites-rooms.md): the bath
// has its primary and "essen" as another, each with its own motion sensor
// and ring; the hallway sensor is the primary's and essen's.
func roomsEnv(t *testing.T) *env {
	e := newEnv(t, func(c *config.Config) {
		c.Satellites["bad"] = config.Entities{"assist_satellite.bad", "assist_satellite.essen"}
		c.Notifier.MorningTrigger.Presence["bad"] = config.Entities{"binary_sensor.bad_motion", "binary_sensor.essen_motion", "binary_sensor.flur"}
		c.Notifier.MorningTrigger.SpeakAt = map[string]string{"binary_sensor.essen_motion": "assist_satellite.essen"}
		c.Notifier.Indicators = map[string]config.Entities{"bad": {"light.bad_ring", "light.essen_ring"}}
	})
	e.ha.set("assist_satellite.essen", "idle", map[string]any{"supported_features": float64(3)})
	e.now = at("08:00") // after quiet hours
	e.n.speakingStart = 20 * time.Millisecond
	return e
}

// spokenAt are the satellites Mira spoke at, in order.
func (e *env) spokenAt() []string {
	e.ha.mu.Lock()
	defer e.ha.mu.Unlock()
	var out []string
	for _, c := range e.ha.calls {
		if c.Domain == "assist_satellite" && c.Target != nil {
			out = append(out, c.Target.EntityID...)
		}
	}
	return out
}

// fakeTriggers is the trigger subscription, faked: Fire runs the handler of
// the sensor's trigger.
type fakeTriggers struct {
	mu       sync.Mutex
	handlers map[string]haws.EventHandler
}

func (f *fakeTriggers) SubscribeTrigger(_ context.Context, trigger map[string]any, h haws.EventHandler) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[trigger["entity_id"].(string)] = h
	return func() {}, nil
}

func (f *fakeTriggers) fire(t *testing.T, sensor string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		h := f.handlers[sensor]
		f.mu.Unlock()
		if h != nil {
			h(haws.Event{})
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nothing watches %s", sensor)
}

// motion runs the notifier, fires a sensor and waits for what is spoken.
func (e *env) motion(sensor string) []string {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := &fakeTriggers{handlers: map[string]haws.EventHandler{}}
	done := make(chan struct{})
	go func() { e.n.Run(ctx, sub); close(done) }()
	sub.fire(e.t, sensor)
	deadline := time.Now().Add(2 * time.Second)
	for len(e.spokenAt()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	return e.spokenAt()
}

func TestSR22_TheMorningAnnouncementComesWhereTheMotionSensorFired(t *testing.T) {
	e := roomsEnv(t)
	e.message("n1", 2, nil)
	if got := e.motion("binary_sensor.essen_motion"); strings.Join(got, ",") != "assist_satellite.essen" {
		t.Errorf("spoken at %v", got)
	}
}

func TestSR23_ASensorSetOnTwoSatellitesSpeaksAtThePrimary(t *testing.T) {
	e := roomsEnv(t)
	e.message("n1", 2, nil)
	if got := e.motion("binary_sensor.flur"); strings.Join(got, ",") != "assist_satellite.bad" {
		t.Errorf("spoken at %v", got)
	}
}

func TestSR24_AReminderComesAtTheSatelliteItWasAskedAt(t *testing.T) {
	e := roomsEnv(t)
	if _, err := e.st.ScheduleMessage(context.Background(), store.Message{ID: "r1", Source: store.SourceReminder, Importance: 2,
		DeliverAt: e.now, Spoken: "Tee", AreaID: "bad", OriginDevice: "dev-essen", Receiver: store.ReceiverHouse}); err != nil {
		t.Fatal(err)
	}
	e.n.DueMessages(context.Background())
	if got := e.spokenAt(); strings.Join(got, ",") != "assist_satellite.essen" {
		t.Errorf("spoken at %v", got)
	}
}

func TestSR25_AReminderFromASatelliteThatLeftTheRoomComesAtThePrimary(t *testing.T) {
	e := roomsEnv(t)
	// asked at the hallway satellite, which is no longer in the bath
	if _, err := e.st.ScheduleMessage(context.Background(), store.Message{ID: "r1", Source: store.SourceReminder, Importance: 2,
		DeliverAt: e.now, Spoken: "Tee", AreaID: "bad", OriginDevice: "dev-flur", Receiver: store.ReceiverHouse}); err != nil {
		t.Fatal(err)
	}
	e.n.DueMessages(context.Background())
	if got := e.spokenAt(); strings.Join(got, ",") != "assist_satellite.bad" {
		t.Errorf("spoken at %v", got)
	}
}

func TestSR26_AMessageToTheRoomComesAtThePrimary(t *testing.T) {
	e := roomsEnv(t)
	if err := e.n.Tell(context.Background(), store.Message{ID: "p1", Source: store.SourceTask, DedupeKey: "p1",
		Importance: 2, Receiver: store.ReceiverHouse, Spoken: "Post ist da."}, "bad"); err != nil {
		t.Fatal(err)
	}
	if got := e.spokenAt(); strings.Join(got, ",") != "assist_satellite.bad" {
		t.Errorf("spoken at %v", got)
	}
}

func TestSR27_EveryRingOfTheRoomShowsWhatWaits(t *testing.T) {
	e := roomsEnv(t)
	e.n.cfg.MorningTrigger.Areas = []string{"kueche"} // the bath shows, it does not announce
	e.n.areas = map[string]bool{"kueche": true}
	e.message("n1", 1, nil)
	e.n.SyncIndicators(context.Background())
	on := map[string]string{}
	for _, c := range e.lightCalls() {
		on[c.Target.EntityID[0]] = c.Service
	}
	if on["light.bad_ring"] != "turn_on" || on["light.essen_ring"] != "turn_on" {
		t.Fatalf("rings %v", on)
	}
	if _, err := e.n.ReadOut(context.Background(), "bad", "dev-essen", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.lightCalls() {
		on[c.Target.EntityID[0]] = c.Service
	}
	if on["light.bad_ring"] != "turn_off" || on["light.essen_ring"] != "turn_off" {
		t.Errorf("rings after it was heard %v", on)
	}
}

func TestSR28_AnUrgentAnnouncementComesOncePerRoom(t *testing.T) {
	e := roomsEnv(t)
	e.message("u1", 3, nil)
	e.tick()
	got := e.spokenAt()
	if strings.Join(got, ",") != "assist_satellite.bad,assist_satellite.kueche" {
		t.Errorf("spoken at %v", got)
	}
}
