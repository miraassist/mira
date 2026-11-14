package conversation

import (
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/tools"
)

// SetAgentWait shortens how long a voice conversation waits for an agent
// it calls as a tool; the returned func restores it.
func SetAgentWait(voice time.Duration) (restore func()) {
	old := agentWaitVoice
	agentWaitVoice = voice
	return func() { agentWaitVoice = old }
}

// ToolsForTest is the engine's tool registry.
func (e *Engine) ToolsForTest() *tools.Registry { return e.tools }

// AgentsForTest is the engine's agent runtime.
func (e *Engine) AgentsForTest() *agent.Runtime { return e.agents }
