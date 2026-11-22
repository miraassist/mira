package alarm

import "testing"

// Hits as Music Assistant's search gave them on the test HA (2026-11-22).
var hits = []Hit{
	{Class: "artist", Title: "Queen", ContentID: "library://artist/1041", ContentType: "music"},
	{Class: "artist", Title: "David Bowie & Queen", ContentID: "library://artist/1869", ContentType: "music"},
	{Class: "album", Title: "Queen - A Night At The Opera", ContentID: "library://album/1765", ContentType: "music"},
	{Class: "track", Title: "Queen - Bohemian Rhapsody", ContentID: "deezer--x://track/4091937401", ContentType: "music"},
	{Class: "playlist", Title: "A night at the opera", ContentID: "deezer--x://playlist/14284780101", ContentType: "music"},
	{Class: "music", Title: "SWR3", ContentID: "deezer--x://radio/34966", ContentType: "music"},
	{Class: "music", Title: "SWR3", ContentID: "library://radio/1", ContentType: "music"},
	{Class: "music", Title: "SWR3 Rock", ContentID: "deezer--x://radio/34960", ContentType: "music"},
}

func TestPickTakesTheKindAndTheName(t *testing.T) {
	for _, c := range []struct{ name, kind, want string }{
		{"Queen", "artist", "library://artist/1041"},
		{"A Night at the Opera", "album", "library://album/1765"}, // the part after "Queen - "
		{"A Night at the Opera", "playlist", "deezer--x://playlist/14284780101"},
		{"Bohemian Rhapsody", "track", "deezer--x://track/4091937401"},
		{"SWR 3", "radio", "library://radio/1"}, // the library first
		{"Queen", "", "library://artist/1041"},
	} {
		got, titles := Pick(hits, c.name, c.kind)
		if got == nil || got.ContentID != c.want {
			t.Errorf("%s %s: %+v %v", c.name, c.kind, got, titles)
		}
	}
	if got, titles := Pick(hits, "Opera", ""); got != nil || len(titles) != 5 {
		t.Errorf("no exact hit: %+v %v", got, titles)
	}
	if got, _ := Pick(hits, "Freddie", "artist"); got != nil {
		t.Errorf("artist not found: %+v", got)
	}
}
