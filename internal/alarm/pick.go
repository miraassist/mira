package alarm

import (
	"slices"
	"strings"
	"unicode"
)

// Hit is one result of Music Assistant's search: its media class (artist,
// album, playlist, track; a station's class is music, known by "://radio/"
// in its address), its title and what play_media gets.
type Hit struct {
	Class, Title, ContentID, ContentType string
}

// kindOf is the kind a hit is of: radio, playlist, album, artist, track.
func kindOf(h Hit) string {
	if strings.Contains(h.ContentID, "://radio/") {
		return "radio"
	}
	return h.Class
}

// Pick chooses what an alarm wakes with among Music Assistant's hits
// (docs/spec/alarm.md, Music): of the kind asked for (any for ""), the hit
// whose name equals the one asked for (case, spaces and punctuation aside;
// for an album or a track the part after "Artist - " counts too), Music
// Assistant's library first; a single hit as it is. Else nil and up to five
// names to ask about.
func Pick(hits []Hit, name, kind string) (*Hit, []string) {
	var fit []Hit
	for _, h := range hits {
		if kind == "" || kindOf(h) == kind {
			fit = append(fit, h)
		}
	}
	if len(fit) == 1 {
		return &fit[0], nil
	}
	want := norm(name)
	var exact []Hit
	for _, h := range fit {
		_, after, _ := strings.Cut(h.Title, " - ")
		if norm(h.Title) == want || (after != "" && norm(after) == want) {
			exact = append(exact, h)
		}
	}
	if len(exact) > 0 {
		for i := range exact {
			if strings.HasPrefix(exact[i].ContentID, "library://") {
				return &exact[i], nil
			}
		}
		return &exact[0], nil
	}
	var titles []string
	for _, h := range fit {
		if !slices.Contains(titles, h.Title) && len(titles) < 5 {
			titles = append(titles, h.Title)
		}
	}
	return nil, titles
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
