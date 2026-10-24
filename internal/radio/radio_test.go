package radio

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

type played struct{ entity, id string }

func env(stations []Station, players map[string][]Player, searchErr error) (tools.InternalTool, *[]played) {
	var got []played
	tool := Tool(Options{
		Lang: i18n.DE,
		Search: func(ctx context.Context, q string) ([]Station, error) {
			return stations, searchErr
		},
		Play: func(ctx context.Context, entity string, st Station) error {
			got = append(got, played{entity, st.ContentID})
			return nil
		},
		Players: func(area string) []Player { return players[area] },
		Area: func(name string) string {
			return map[string]string{"küche": "kueche", "wohnzimmer": "wohnzimmer"}[name]
		},
		AreaName: func(id string) string {
			return map[string]string{"kueche": "Küche", "wohnzimmer": "Wohnzimmer"}[id]
		},
	})
	return tool, &got
}

func call(t *testing.T, tool tools.InternalTool, area, args string) string {
	t.Helper()
	out, err := tool.Run(context.Background(), tools.Invocation{Area: area, Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var swr3 = []Station{
	{Title: "SWR3 - 96K AAC", ContentID: "media-source://radio_browser/b", ContentType: "audio/aac"},
	{Title: "SWR3", ContentID: "media-source://radio_browser/a", ContentType: "audio/mpeg"},
}

func TestPlaysTheNamedStationInTheUsersRoom(t *testing.T) {
	players := map[string][]Player{"wohnzimmer": {{Entity: "media_player.show_speaker"}}}
	tool, got := env(swr3, players, nil)
	out := call(t, tool, "wohnzimmer", `{"station":"SWR 3"}`)
	if len(*got) != 1 || (*got)[0] != (played{"media_player.show_speaker", "media-source://radio_browser/a"}) {
		t.Fatalf("played %+v", *got)
	}
	if !strings.Contains(out, "SWR3") || !strings.Contains(out, "Wohnzimmer") {
		t.Errorf("answer: %q", out)
	}
}

func TestAnotherRoomByName(t *testing.T) {
	players := map[string][]Player{"kueche": {{Entity: "media_player.echo"}}}
	tool, got := env(swr3, players, nil)
	call(t, tool, "wohnzimmer", `{"station":"swr3","room":"Küche"}`)
	if len(*got) != 1 || (*got)[0].entity != "media_player.echo" {
		t.Errorf("played %+v", *got)
	}
}

func TestNothingPlaysWhenItCannotBeSure(t *testing.T) {
	one := map[string][]Player{"wohnzimmer": {{Entity: "media_player.show_speaker"}}}
	two := map[string][]Player{"wohnzimmer": {{Entity: "media_player.a", Names: []string{"a"}}, {Entity: "media_player.b", Names: []string{"b"}}}}
	several := []Station{{Title: "Rock Antenne", ContentID: "x"}, {Title: "Rock FM", ContentID: "y"}}
	for name, c := range map[string]struct {
		stations []Station
		players  map[string][]Player
		area     string
		args     string
		err      error
		want     string
	}{
		"no room":          {swr3, one, "", `{"station":"SWR3"}`, nil, "welchem Raum"},
		"unknown room":     {swr3, one, "wohnzimmer", `{"station":"SWR3","room":"Keller"}`, nil, "welchem Raum"},
		"no speaker":       {swr3, one, "kueche", `{"station":"SWR3"}`, nil, "kein Lautsprecher"},
		"several speakers": {swr3, two, "wohnzimmer", `{"station":"SWR3"}`, nil, "mehrere Lautsprecher"},
		"no station":       {nil, one, "wohnzimmer", `{"station":"Nix"}`, nil, "Kein Sender"},
		"several stations": {several, one, "wohnzimmer", `{"station":"Rock"}`, nil, "Rock Antenne; Rock FM"},
		"search broken":    {nil, one, "wohnzimmer", `{"station":"SWR3"}`, errors.New("boom"), "Radio Browser"},
	} {
		tool, got := env(c.stations, c.players, c.err)
		if out := call(t, tool, c.area, c.args); !strings.Contains(out, c.want) {
			t.Errorf("%s: %q lacks %q", name, out, c.want)
		}
		if len(*got) != 0 {
			t.Errorf("%s: played %+v", name, *got)
		}
	}
}

// The question back names the speakers as Home Assistant does, with the
// entity to call; the answer says which one plays.
func TestSpeakersByName(t *testing.T) {
	two := map[string][]Player{"bad": {
		{Entity: "media_player.show", Name: "Show", Names: []string{"show"}},
		{Entity: "media_player.show_bad_speaker", Name: "Show Bad Speaker", Names: []string{"show bad speaker", "show bad"}}}}
	tool, got := env(swr3, two, nil)
	if out := call(t, tool, "bad", `{"station":"SWR3"}`); !strings.Contains(out, "Show (media_player.show), Show Bad Speaker (media_player.show_bad_speaker)") {
		t.Errorf("question: %q", out)
	}
	out := call(t, tool, "bad", `{"station":"SWR3","player":"media_player.show_bad_speaker"}`)
	if len(*got) != 1 || (*got)[0].entity != "media_player.show_bad_speaker" || !strings.Contains(out, "auf Show Bad Speaker im Raum") {
		t.Errorf("played %+v: %q", *got, out)
	}
}

func TestSeveralSpeakersChosenByName(t *testing.T) {
	two := map[string][]Player{"wohnzimmer": {{Entity: "media_player.a", Names: []string{"küchenradio"}}, {Entity: "media_player.b", Names: []string{"show"}}}}
	tool, got := env(swr3, two, nil)
	call(t, tool, "wohnzimmer", `{"station":"SWR3","player":"Show"}`)
	if len(*got) != 1 || (*got)[0].entity != "media_player.b" {
		t.Errorf("played %+v", *got)
	}
}

type fakeHA struct {
	cmd  map[string]any
	svc  string
	data map[string]any
}

func (f *fakeHA) Call(ctx context.Context, c map[string]any) (json.RawMessage, error) {
	f.cmd = c
	return json.RawMessage(`{"result":[
	 {"title":"SWR3","media_content_id":"media-source://radio_browser/a","media_content_type":"audio/mpeg","can_play":true},
	 {"title":"Ordner","media_content_id":"media-source://radio_browser/dir","media_content_type":"","can_play":false},
	 {"title":"Fremd","media_content_id":"media-source://other/x","media_content_type":"audio/mpeg","can_play":true}]}`), nil
}

func (f *fakeHA) CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error {
	f.svc, f.data = domain+"."+service, data
	f.data["target"] = target.EntityID[0]
	return nil
}

// Only playable stations of the Radio Browser count; play_media gets the
// station's address and type.
func TestHomeAssistantSide(t *testing.T) {
	ha := &fakeHA{}
	st, err := Search(ha)(context.Background(), "SWR3")
	if err != nil || len(st) != 1 || st[0].Title != "SWR3" {
		t.Fatalf("search: %+v %v", st, err)
	}
	if ha.cmd["type"] != "media_source/search_media" || ha.cmd["search_query"] != "SWR3" {
		t.Errorf("command: %v", ha.cmd)
	}
	if err := Play(ha)(context.Background(), "media_player.show_speaker", st[0]); err != nil {
		t.Fatal(err)
	}
	if ha.svc != "media_player.play_media" || ha.data["media_content_id"] != "media-source://radio_browser/a" ||
		ha.data["media_content_type"] != "audio/mpeg" || ha.data["target"] != "media_player.show_speaker" {
		t.Errorf("service: %s %v", ha.svc, ha.data)
	}
}
