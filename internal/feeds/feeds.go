// Package feeds reads RSS 2.0 and Atom feeds. Feed text is third-party
// content: callers hand it to the model as data on the background channel,
// which cannot act on the house.
package feeds

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Item is one entry, reduced to what an assessment needs.
type Item struct {
	ID        string
	Title     string
	Link      string
	Summary   string
	Published time.Time
}

// maxBody bounds a feed download; a household feed is a few hundred KB.
const maxBody = 4 << 20

// Fetch downloads and parses a feed.
func Fetch(ctx context.Context, client *http.Client, url string) ([]Item, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lares (household agent)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return Parse(body)
}

type rss struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		PubDate     string `xml:"pubDate"`
		Description string `xml:"description"`
	} `xml:"channel>item"`
}

type atom struct {
	Entries []struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Publ    string `xml:"published"`
		Summary string `xml:"summary"`
		Content string `xml:"content"`
		Links   []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// Parse reads RSS 2.0 or Atom.
func Parse(body []byte) ([]Item, error) {
	var root struct{ XMLName xml.Name }
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("not a feed: %w", err)
	}
	switch root.XMLName.Local {
	case "rss":
		var f rss
		if err := xml.Unmarshal(body, &f); err != nil {
			return nil, err
		}
		out := make([]Item, 0, len(f.Items))
		for _, it := range f.Items {
			id := strings.TrimSpace(it.GUID)
			if id == "" {
				id = strings.TrimSpace(it.Link)
			}
			out = append(out, Item{ID: id, Title: clean(it.Title), Link: strings.TrimSpace(it.Link),
				Summary: clean(it.Description), Published: parseDate(it.PubDate)})
		}
		return out, nil
	case "feed":
		var f atom
		if err := xml.Unmarshal(body, &f); err != nil {
			return nil, err
		}
		out := make([]Item, 0, len(f.Entries))
		for _, e := range f.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			summary := e.Summary
			if summary == "" {
				summary = e.Content
			}
			published := parseDate(e.Publ)
			if published.IsZero() {
				published = parseDate(e.Updated)
			}
			id := strings.TrimSpace(e.ID)
			if id == "" {
				id = link
			}
			out = append(out, Item{ID: id, Title: clean(e.Title), Link: link, Summary: clean(summary), Published: published})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown feed format <%s>", root.XMLName.Local)
}

var tags = regexp.MustCompile(`<[^>]*>`)

// clean strips markup and squeezes whitespace; summaries are capped because
// they go into a prompt.
func clean(s string) string {
	s = html.UnescapeString(tags.ReplaceAllString(s, " "))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 400 {
		s = string(r[:400]) + " …"
	}
	return s
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 MST", "Mon, 2 Jan 2006 15:04:05 -0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
