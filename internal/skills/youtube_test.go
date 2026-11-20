package skills_test

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
)

// The shipped skill youtube (docs/spec/youtube.md).

func youtubeSkill(t *testing.T, settings map[string]any) skills.Skill {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	settings["client_id"], settings["client_secret"] = "id-1", "geheim"
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"youtube": settings})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("youtube did not load: %v", problems)
	}
	return loaded[0]
}

// youtubeFake answers by the path of the request and records the queries.
type youtubeFake struct {
	mu      sync.Mutex
	answers map[string]string
	status  int
	queries []url.URL
}

func (f *youtubeFake) Request(_ context.Context, _, method, raw string, _ []byte) (int, []byte, error) {
	u, _ := url.Parse(raw)
	f.mu.Lock()
	f.queries = append(f.queries, *u)
	f.mu.Unlock()
	if f.status != 0 {
		return f.status, []byte(f.answers["error"]), nil
	}
	return 200, []byte(f.answers[strings.TrimPrefix(u.Path, "/youtube/v3/")]), nil
}

func runYouTube(t *testing.T, fake *youtubeFake, tool, args string) (map[string]any, error) {
	t.Helper()
	s := youtubeSkill(t, nil)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	res := s.Starlark.Call(context.Background(), tool, json.RawMessage(args), startools.Env{Requester: fake, Now: func() time.Time { return now }})
	if res.Err != nil {
		return nil, res.Err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		t.Fatalf("%s: %q", tool, res.Text)
	}
	return out, nil
}

const ytVideos = `{"items":[
 {"id":"abc","snippet":{"title":"Echo Dot als Voice Satellite &amp; mehr","channelTitle":"Bastler","publishedAt":"2026-11-19T18:00:00Z","categoryId":"28"},
  "statistics":{"viewCount":"12345"},"contentDetails":{"duration":"PT12M30S"}},
 {"id":"def","snippet":{"title":"Local Assist","channelTitle":"HA Fan","publishedAt":"2026-11-18T08:00:00Z","categoryId":"27"},
  "statistics":{"viewCount":"800"},"contentDetails":{"duration":"PT1H2M"}}]}`

// YT-01 The skill only reads
func TestYT01_TheSkillOnlyReads(t *testing.T) {
	// Given the skill youtube with client id and secret
	// When Mira loads it
	s := youtubeSkill(t, nil)
	// Then its connection asks only for the scope youtube.readonly
	o := s.OAuth
	if o == nil || !slices.Equal(o.Scopes, []string{"https://www.googleapis.com/auth/youtube.readonly"}) || !o.Ready() {
		t.Fatalf("oauth %+v", o)
	}
	// And its api section allows GET below https://www.googleapis.com/youtube/v3/ and nothing else
	if len(o.API) != 1 || o.API[0].URL != "https://www.googleapis.com/youtube/v3/" || !slices.Equal(o.API[0].Methods, []string{"GET"}) {
		t.Errorf("api %+v", o.API)
	}
	// And none of its tools writes
	if s.Starlark == nil || !slices.Equal(s.Starlark.Names(), []string{"charts", "details", "kanal", "suche"}) || len(s.Writes) != 0 {
		t.Errorf("tools %v, writes %v", s.Starlark, s.Writes)
	}
}

// YT-02 Google's login gives a refresh token
func TestYT02_GooglesLoginGivesARefreshToken(t *testing.T) {
	// Given the skill youtube
	o := youtubeSkill(t, nil).OAuth
	// When the integration asks for its login
	// Then the extra parameters are access_type offline and prompt consent, with PKCE
	if o.AuthorizeParams["access_type"] != "offline" || o.AuthorizeParams["prompt"] != "consent" || !o.PKCE ||
		o.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" || o.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("login %+v", o)
	}
}

// YT-03 A search is one search and one look at the details
func TestYT03_ASearchIsOneSearchAndOneLookAtTheDetails(t *testing.T) {
	// Given the tool youtube__suche with a topic and 7 days
	fake := &youtubeFake{answers: map[string]string{"search": `{"items":[{"id":{"videoId":"abc"}},{"id":{"videoId":"def"}}]}`, "videos": ytVideos}}
	// When it runs
	out, err := runYouTube(t, fake, "suche", `{"thema":"echo voice satellite","tage":7,"sortierung":"aufrufe"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Then it makes one search with publishedAfter 7 days ago, the order asked for and only the ids, then one request
	// for the details of all of them
	if len(fake.queries) != 2 || fake.queries[0].Path != "/youtube/v3/search" || fake.queries[1].Path != "/youtube/v3/videos" {
		t.Fatalf("requests %v", fake.queries)
	}
	q := fake.queries[0].Query()
	if q.Get("q") != "echo voice satellite" || q.Get("publishedAfter") != "2026-11-13T09:00:00Z" || q.Get("order") != "viewCount" ||
		q.Get("type") != "video" || q.Get("fields") != "items(id/videoId)" || q.Get("maxResults") != "5" {
		t.Errorf("search %v", q)
	}
	if v := fake.queries[1].Query(); v.Get("id") != "abc,def" || !strings.Contains(v.Get("fields"), "statistics") {
		t.Errorf("details %v", v)
	}
	// And it returns title, channel, date, views, minutes and the address of each video
	vs := out["videos"].([]any)
	first := vs[0].(map[string]any)
	if len(vs) != 2 || first["titel"] != "Echo Dot als Voice Satellite & mehr" || first["kanal"] != "Bastler" || first["datum"] != "2026-11-19" ||
		first["aufrufe"] != 12345.0 || first["minuten"] != 13.0 || first["adresse"] != "https://www.youtube.com/watch?v=abc" ||
		first["kategorie"] != "Wissenschaft und Technik" || vs[1].(map[string]any)["minuten"] != 62.0 {
		t.Errorf("videos %v", vs)
	}
	// Nothing found is a hint, and no look at the details.
	fake = &youtubeFake{answers: map[string]string{"search": `{"items":[]}`}}
	out, err = runYouTube(t, fake, "suche", `{"thema":"nichts"}`)
	if err != nil || out["hinweis"] == nil || len(fake.queries) != 1 {
		t.Errorf("%v %v %v", out, err, fake.queries)
	}
	// Limits are the code's.
	if _, err := runYouTube(t, fake, "suche", `{"thema":"x","tage":90}`); err == nil || !strings.Contains(err.Error(), "tage ist 1 bis 30") {
		t.Errorf("tage 90: %v", err)
	}
}

// YT-04 A channel's uploads cost no search
func TestYT04_AChannelsUploadsCostNoSearch(t *testing.T) {
	// Given the tool youtube__kanal with a handle without @
	fake := &youtubeFake{answers: map[string]string{
		"channels":      `{"items":[{"id":"UC1","snippet":{"title":"heise online"},"contentDetails":{"relatedPlaylists":{"uploads":"UU1"}}}]}`,
		"playlistItems": `{"items":[{"contentDetails":{"videoId":"abc"}},{"contentDetails":{"videoId":"def"}}]}`,
		"videos":        ytVideos}}
	// When it runs
	out, err := runYouTube(t, fake, "kanal", `{"handle":"heise"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Then it asks for the channel by @handle, then its uploads, then their details, and makes no search
	var paths []string
	for _, q := range fake.queries {
		paths = append(paths, strings.TrimPrefix(q.Path, "/youtube/v3/"))
	}
	if !slices.Equal(paths, []string{"channels", "playlistItems", "videos"}) || fake.queries[0].Query().Get("forHandle") != "@heise" ||
		fake.queries[1].Query().Get("playlistId") != "UU1" {
		t.Errorf("requests %v", fake.queries)
	}
	if out["kanal"] != "heise online" || len(out["videos"].([]any)) != 2 {
		t.Errorf("out %v", out)
	}
	// And a handle YouTube does not know is a hint, not an error
	fake = &youtubeFake{answers: map[string]string{"channels": `{"kind":"youtube#channelListResponse","pageInfo":{"totalResults":0}}`}}
	out, err = runYouTube(t, fake, "kanal", `{"handle":"@gibtsnicht"}`)
	if err != nil || !strings.Contains(out["hinweis"].(string), "keinen Kanal @gibtsnicht") {
		t.Errorf("%v %v", out, err)
	}
}

// YT-05 The quota used up is a sentence
func TestYT05_TheQuotaUsedUpIsASentence(t *testing.T) {
	// Given YouTube answers 403 with the reason quotaExceeded
	fake := &youtubeFake{status: 403, answers: map[string]string{"error": `{"error":{"code":403,"message":"The request cannot be completed because you have exceeded your quota.","errors":[{"reason":"quotaExceeded"}]}}`}}
	// When a tool runs
	_, err := runYouTube(t, fake, "charts", `{}`)
	// Then its error says that the daily quota is used up
	if err == nil || !strings.Contains(err.Error(), "Tageskontingent ist aufgebraucht") {
		t.Errorf("%v", err)
	}
	fake = &youtubeFake{status: 400, answers: map[string]string{"error": `{"error":{"code":400,"message":"Invalid FieldMask","errors":[{"reason":"invalidParameter"}]}}`}}
	if _, err := runYouTube(t, fake, "charts", `{}`); err == nil || !strings.Contains(err.Error(), "HTTP 400: invalidParameter") {
		t.Errorf("400: %v", err)
	}
}

// YT-06 The daily list goes to the person named
func TestYT06_TheDailyListGoesToThePersonNamed(t *testing.T) {
	// Given the skill youtube with themen and empfaenger
	s := youtubeSkill(t, map[string]any{"themen": "Home Assistant Voice, lokale KI", "empfaenger": "Stefan"})
	// When its schedule is loaded
	if len(s.Schedules) != 1 {
		t.Fatalf("schedules %+v", s.Schedules)
	}
	sc := s.Schedules[0]
	// Then it runs daily at 7:00 and its message goes to the person, and its task names the topics
	if sc.Name != "liste" || sc.Default != "0 7 * * *" || sc.To != "Stefan" || !strings.Contains(sc.Task, "Die Themen: Home Assistant Voice, lokale KI") {
		t.Errorf("schedule %+v", sc)
	}
	// And without themen the task says "-" and the instructions say to report nothing then
	s = youtubeSkill(t, nil)
	if sc := s.Schedules[0]; !strings.HasSuffix(sc.Task, "Die Themen: -") || sc.To != "" {
		t.Errorf("without themen: %+v", sc)
	}
	if !strings.Contains(s.Body, "„-“ oder keine Themen: importance 0, spoken leer,\nkein Werkzeug.") {
		t.Errorf("instructions:\n%s", s.Body)
	}
}
