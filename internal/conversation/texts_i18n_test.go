package conversation

import (
	"testing"

	"github.com/miraassist/mira/internal/i18n"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}
