package conversation

import (
	"time"

	"github.com/miraassist/mira/internal/tools"
)

// SetDelegateWait shortens how long a voice conversation waits for the
// delegate; the returned func restores it.
func SetDelegateWait(voice time.Duration) (restore func()) {
	old := delegateWaitVoice
	delegateWaitVoice = voice
	return func() { delegateWaitVoice = old }
}

// ToolsForTest is the engine's tool registry.
func (e *Engine) ToolsForTest() *tools.Registry { return e.tools }
