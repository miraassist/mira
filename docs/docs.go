// Package docs embeds the user guide, so the web UI shows the same text as
// the repository.
package docs

import (
	"embed"
	"fmt"
)

//go:embed guide/*.md
var guide embed.FS

// Guide returns the user guide in a language ("de", "en"), falling back to
// German.
func Guide(lang string) ([]byte, error) {
	b, err := guide.ReadFile(fmt.Sprintf("guide/%s.md", lang))
	if err != nil {
		return guide.ReadFile("guide/de.md")
	}
	return b, nil
}
