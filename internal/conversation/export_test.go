package conversation

import "time"

// SetDelegateWait shortens how long a voice conversation waits for the
// delegate; the returned func restores it.
func SetDelegateWait(voice time.Duration) (restore func()) {
	old := delegateWaitVoice
	delegateWaitVoice = voice
	return func() { delegateWaitVoice = old }
}
