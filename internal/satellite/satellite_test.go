package satellite

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// Every sound listed has its file, every file is listed.
func TestSoundsMatchFiles(t *testing.T) {
	names, _ := fs.Glob(files, "sounds/*.mp3")
	if len(names) != len(Sounds) {
		t.Errorf("%d files, %d sounds", len(names), len(Sounds))
	}
	for _, s := range Sounds {
		if data, ok := File(s.Name); !ok || len(data) == 0 {
			t.Errorf("no file for %s", s.Name)
		}
	}
}

type fake struct {
	played  []string
	shown   []Card
	actions []string
	waited  time.Duration
}

func env() (map[string]tools.InternalTool, *fake) {
	f := &fake{}
	list := Tools(Options{
		Lang: i18n.DE,
		Speaker: func(_ context.Context, device string) string {
			return map[string]string{"show": "media_player.show_speaker"}[device]
		},
		Play: func(_ context.Context, entity, url string) error {
			f.played = append(f.played, entity+" "+url)
			return nil
		},
		BaseURL: func(context.Context) string { return "http://ha.local:8123/" },
		CardAction: func(_ context.Context, device string) string {
			return map[string]string{"show": "esphome.show_notify_picture"}[device]
		},
		Show: func(_ context.Context, action string, c Card) error {
			f.actions = append(f.actions, action)
			f.shown = append(f.shown, c)
			return nil
		},
		Wait: func(_ context.Context, d time.Duration) error {
			f.waited += d
			return nil
		},
	})
	out := map[string]tools.InternalTool{}
	for _, tool := range list {
		out[tool.Name] = tool
	}
	return out, f
}

func call(t *testing.T, tool tools.InternalTool, device, args string) string {
	t.Helper()
	out, err := tool.Run(context.Background(), tools.Invocation{DeviceID: device, Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSA01_SoundPlaysOnTheSatellitesSpeaker(t *testing.T) {
	tl, f := env()
	call(t, tl[ToolSound], "show", `{"name":"fanfare"}`)
	want := "media_player.show_speaker http://ha.local:8123/api/mira/panel/ui/sounds/fanfare.mp3"
	if len(f.played) != 1 || f.played[0] != want {
		t.Errorf("played %v", f.played)
	}
}

func TestSA02_ToolReturnsWhenTheSoundIsOver(t *testing.T) {
	tl, f := env()
	call(t, tl[ToolSound], "show", `{"name":"fanfare"}`)
	if f.waited < 1700*time.Millisecond {
		t.Errorf("waited %v", f.waited)
	}
}

func TestSA03_WithoutSatelliteNoSoundAndNoCard(t *testing.T) {
	tl, f := env()
	out := call(t, tl[ToolSound], "", `{"name":"fanfare"}`)
	out2 := call(t, tl[ToolCard], "", `{"title":"T","line":"L","picture":"https://x/y.jpg"}`)
	if len(f.played)+len(f.shown) != 0 || !strings.Contains(out, "keinen Satelliten") || !strings.Contains(out2, "keinen Satelliten") {
		t.Errorf("played %v, shown %v: %q %q", f.played, f.shown, out, out2)
	}
}

func TestSA04_UnknownSound(t *testing.T) {
	tl, f := env()
	out := call(t, tl[ToolSound], "show", `{"name":"trommel"}`)
	if len(f.played) != 0 || !strings.Contains(out, "fanfare_big") {
		t.Errorf("played %v: %q", f.played, out)
	}
}

func TestSA06_CardOnTheSatellitesScreen(t *testing.T) {
	tl, f := env()
	call(t, tl[ToolCard], "show", `{"title":"Pfannkuchen · Schritt 2 von 5","line":"Teig glatt rühren","picture":"https://img/p.jpg"}`)
	if len(f.actions) != 1 || f.actions[0] != "esphome.show_notify_picture" || f.shown[0].Line != "Teig glatt rühren" {
		t.Fatalf("actions %v, shown %v", f.actions, f.shown)
	}
	data := CardData(f.shown[0])
	if data["chime"] != "off" || data["speak"] != "off" || data["seconds"] != 0 || data["message"] != "Teig glatt rühren" {
		t.Errorf("data %v", data)
	}
}

func TestSA07_SatelliteWithoutCards(t *testing.T) {
	tl, f := env()
	out := call(t, tl[ToolCard], "echo", `{"title":"T","line":"L","picture":"https://x/y.jpg"}`)
	if len(f.shown) != 0 || !strings.Contains(out, "keinen Bildschirm") {
		t.Errorf("shown %v: %q", f.shown, out)
	}
}
