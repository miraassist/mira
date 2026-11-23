package startools

import (
	"context"
	"strings"
	"testing"
)

// EN-07 A Starlark tool whose caller ended says so
// (docs/spec/endings.md): its own time limit and its caller's end are told
// apart.
func TestEN07_AStarlarkToolWhoseCallerEndedSaysSo(t *testing.T) {
	if got := cancelReason(context.Canceled); got != "the tool's caller ended" {
		t.Errorf("caller ended: %q", got)
	}
	if got := cancelReason(context.DeadlineExceeded); !strings.HasPrefix(got, "the tool took longer than") {
		t.Errorf("own limit: %q", got)
	}
}
