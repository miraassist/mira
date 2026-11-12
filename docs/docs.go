// Package docs embeds the user guide, so the web UI shows the same text as
// the repository.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed guide/*.md guide/img
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

// GuideFile returns a picture or recording of the guide (guide/img/<name>);
// name may hold folders below img ("paperless/tags.png"), never "..".
func GuideFile(name string) ([]byte, error) {
	if name == "" || !fs.ValidPath(name) {
		return nil, fmt.Errorf("bad name %q", name)
	}
	return guide.ReadFile("guide/img/" + name)
}
