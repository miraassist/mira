package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/devices"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/voicemail"
)

// docs/spec/messages.md, step 5: found devices as a kind.

func foundKindApp(t *testing.T) (*app, *bytes.Buffer) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{"../../skills"},
		map[string]map[string]any{devices.Skill: nil, voicemail.Skill: nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	return &app{store: st, log: logger, lang: i18n.DE,
		threads: threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: logger})}, &log
}

func find(t *testing.T, a *app, item, name string) store.Thread {
	t.Helper()
	ctx := context.Background()
	id, err := a.foundStartHouse(ctx, name, "# Neues Gerät\n- [ ] Einbinden\n- [ ] Raum\n- [ ] Name", item)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.foundPost(ctx, id, "Neues Gerät gefunden: "+name, "details"); err != nil {
		t.Fatal(err)
	}
	th, err := a.store.GetThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func deviceMessages(t *testing.T, a *app) []store.Message {
	t.Helper()
	all, err := a.store.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending},
		Receiver: store.ReceiverHouse})
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Message
	for _, m := range all {
		if m.Kind == store.KindDevice {
			out = append(out, m)
		}
	}
	return out
}

func TestMS31_AFoundDeviceStartsItsOwnThreadAtTheHouseAndAnEntry(t *testing.T) {
	a, _ := foundKindApp(t)
	th := find(t, a, "device:d1", "Hue Lampe")
	if th.Place != store.PlaceHouse || th.Target != devices.Skill || th.Fields[foundItem] != "device:d1" ||
		!near(th.ExpiresAt, time.Now().Add(24*time.Hour)) {
		t.Fatalf("thread = %+v", th)
	}
	ms := deviceMessages(t, a)
	if len(ms) != 1 || len(ms[0].Entries) != 1 || ms[0].Entries[0].ThreadID != th.ID {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS32_AFurtherDeviceGetsAThreadOfItsOwn(t *testing.T) {
	a, _ := foundKindApp(t)
	first := find(t, a, "device:d1", "Hue Lampe")
	second := find(t, a, "flow:f2", "Shelly Plus 1")
	if first.ID == second.ID {
		t.Fatal("one thread for two devices")
	}
	ms := deviceMessages(t, a)
	if len(ms) != 1 || len(ms[0].Entries) != 2 || ms[0].Entries[1].ThreadID != second.ID {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS35_TakenUpTheDevicesThreadMovesIntoTheRoom(t *testing.T) {
	a, log := foundKindApp(t)
	ctx := context.Background()
	th := find(t, a, "device:d1", "Hue Lampe")
	inv := tools.Invocation{SessionKey: "voice:k1", Channel: config.ChannelVoice, Area: "kueche", DeviceID: "sat-kueche"}
	id, ok, err := a.foundTakeItem(ctx, inv, "device:d1")
	if err != nil || !ok || id != th.ID {
		t.Fatalf("take: %s %v %v", id, ok, err)
	}
	got, _ := a.store.GetThread(ctx, th.ID)
	if got.Place != "area:kueche" || got.Area != "kueche" || got.Device != "sat-kueche" {
		t.Fatalf("thread = %+v", got)
	}
	if !strings.Contains(log.String(), "thread moved from the house into the room") {
		t.Errorf("no log line:\n%s", log)
	}
	// Bound to the house: the answering machine stays the house's.
	vm, _, err := a.threads.HousePlan(ctx, voicemail.Skill, "AB", "# AB\n- [ ] Oma (id 1)", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.threads.Take(ctx, inv, vm.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetThread(ctx, vm.ID); got.Place != store.PlaceHouse {
		t.Fatalf("the answering machine moved to %s", got.Place)
	}
	// Ignored: its thread ends and its entry leaves.
	other := find(t, a, "device:d2", "Stecker")
	a.threads.Finish(ctx, a.foundItemThread(ctx, "device:d2"))
	if got, _ := a.store.GetThread(ctx, other.ID); store.ThreadOpen(got.State) {
		t.Fatalf("still open: %s", got.State)
	}
}
