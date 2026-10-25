// Package memfile writes Mira's memory as a Markdown file for people and
// reads it back (docs/spec/gedaechtnis-datei.md): a heading per owner, a
// fact per line, tags as #words at its end. Importing replaces the facts of
// every owner the file has a section for, like loading a file into an
// editor.
package memfile

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/miraassist/mira/internal/store"
	"github.com/oklog/ulid/v2"
)

// Owner is a memory the file may hold: the house or a person, with the name
// its heading shows.
type Owner struct {
	Key  string
	Name string
}

// HouseHeadings are the headings that mean the house, in the file's
// languages; the first is the one written.
var HouseHeadings = []string{"Haus", "House"}

// Line is one fact of the file.
type Line struct {
	Text string
	Tags []string
}

// Section is an owner's part of the file, as written.
type Section struct {
	Heading string
	Lines   []Line
}

// Write renders the facts of these owners, the house first if it is among
// them, each owner's facts in the order they were saved.
func Write(ctx context.Context, st *store.Store, owners []Owner) (string, error) {
	var b strings.Builder
	for i, o := range owners {
		facts, err := st.AllFacts(ctx, o.Key)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "# %s\n\n", heading(o))
		for _, f := range facts {
			b.WriteString("- " + oneLine(f.Text))
			for _, t := range f.Tags {
				if t = tagWord(t); t != "" {
					b.WriteString(" #" + t)
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func heading(o Owner) string {
	if o.Key == store.OwnerHouse {
		return HouseHeadings[0]
	}
	if o.Name != "" {
		return o.Name
	}
	return o.Key
}

// oneLine keeps a fact on its line: line breaks become spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// tagWord makes a tag one word, as #tag needs it.
func tagWord(t string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '#' {
			return '_'
		}
		return r
	}, strings.TrimSpace(t))
}

// LineError is a fact line before any heading.
type LineError struct{ Line int }

func (e LineError) Error() string { return fmt.Sprintf("line %d: a fact before any heading", e.Line) }

// Parse reads the file: "# " lines start a section, "- " and "* " lines are
// facts; everything else is the file's own notes and ignored.
func Parse(text string) ([]Section, error) {
	var out []Section
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if h, ok := strings.CutPrefix(line, "# "); ok {
			out = append(out, Section{Heading: strings.TrimSpace(h)})
			continue
		}
		item, ok := strings.CutPrefix(line, "- ")
		if !ok {
			item, ok = strings.CutPrefix(line, "* ")
		}
		if !ok {
			continue
		}
		if len(out) == 0 {
			return nil, LineError{Line: i + 1}
		}
		if l := parseLine(item); l.Text != "" {
			out[len(out)-1].Lines = append(out[len(out)-1].Lines, l)
		}
	}
	return out, nil
}

// parseLine splits the #tags at the end of a line off its text.
func parseLine(s string) Line {
	words := strings.Fields(s)
	end := len(words)
	for end > 0 && strings.HasPrefix(words[end-1], "#") && len(words[end-1]) > 1 {
		end--
	}
	var tags []string
	for _, w := range words[end:] {
		tags = append(tags, strings.TrimPrefix(w, "#"))
	}
	text := strings.Join(words[:end], " ")
	text = strings.NewReplacer("**", "", "__", "").Replace(text)
	return Line{Text: strings.TrimSpace(text), Tags: tags}
}

// Key compares facts: without case, extra spaces and a final full stop.
func Key(text string) string {
	return strings.TrimSuffix(strings.ToLower(oneLine(text)), ".")
}

// HeadingError says why the file cannot be imported: a heading that names
// no owner, or one whoever imports may not change.
type HeadingError struct {
	Heading string
	Unknown bool
}

func (e HeadingError) Error() string {
	if e.Unknown {
		return fmt.Sprintf("no house or person is called %q", e.Heading)
	}
	return fmt.Sprintf("the section %q may not be imported here", e.Heading)
}

// Change is what an import does to one owner's memory.
type Change struct {
	Owner  Owner
	Add    []Line
	Remove []store.Fact
	// Keep are the facts that stay; their tags are the file's.
	Keep []store.Fact
}

// Plan compares the file with the memory now. known are every house and
// person there is, allowed those whoever imports may change. A heading that
// is neither stops the import; nothing is changed by Plan.
func Plan(ctx context.Context, st *store.Store, sections []Section, known, allowed []Owner) ([]Change, error) {
	var out []Change
	seen := map[string]int{}
	for _, s := range sections {
		o, ok := find(known, s.Heading)
		if !ok {
			return nil, HeadingError{Heading: s.Heading, Unknown: true}
		}
		if _, ok := find(allowed, s.Heading); !ok {
			return nil, HeadingError{Heading: s.Heading}
		}
		// The same owner twice in a file: its sections count as one.
		if i, dup := seen[o.Key]; dup {
			out[i].Add = append(out[i].Add, s.Lines...)
			continue
		}
		seen[o.Key] = len(out)
		out = append(out, Change{Owner: o, Add: s.Lines})
	}
	for i := range out {
		c := &out[i]
		facts, err := st.AllFacts(ctx, c.Owner.Key)
		if err != nil {
			return nil, err
		}
		inFile := map[string]Line{}
		var add []Line
		for _, l := range c.Add {
			k := Key(l.Text)
			if _, dup := inFile[k]; dup {
				continue
			}
			inFile[k] = l
			add = append(add, l)
		}
		have := map[string]bool{}
		for _, f := range facts {
			k := Key(f.Text)
			if l, ok := inFile[k]; ok && !have[k] {
				have[k] = true
				f.Tags = l.Tags
				c.Keep = append(c.Keep, f)
				continue
			}
			c.Remove = append(c.Remove, f)
		}
		c.Add = c.Add[:0]
		for _, l := range add {
			if !have[Key(l.Text)] {
				c.Add = append(c.Add, l)
			}
		}
	}
	return out, nil
}

// find matches a heading to an owner: the house by one of HouseHeadings, a
// person by name or key, without case.
func find(owners []Owner, h string) (Owner, bool) {
	for _, o := range owners {
		if o.Key == store.OwnerHouse {
			for _, w := range HouseHeadings {
				if strings.EqualFold(h, w) {
					return o, true
				}
			}
			continue
		}
		if strings.EqualFold(h, o.Key) || (o.Name != "" && strings.EqualFold(h, o.Name)) {
			return o, true
		}
	}
	return Owner{}, false
}

// Apply writes the changes in one go. New facts get author (a person key,
// or empty) and the source "import".
func Apply(ctx context.Context, st *store.Store, changes []Change, author string) error {
	var add, keep []store.Fact
	var remove []string
	for _, c := range changes {
		for _, l := range c.Add {
			add = append(add, store.Fact{ID: ulid.Make().String(), Text: l.Text, Tags: l.Tags, Owner: c.Owner.Key,
				Author: author, Source: Source})
		}
		keep = append(keep, c.Keep...)
		for _, f := range c.Remove {
			remove = append(remove, f.ID)
		}
	}
	return st.ReplaceFacts(ctx, add, keep, remove)
}

// Source marks facts that came in from a file.
const Source = "import"
