package voicemail

import (
	"errors"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The speaker cannot play the message: the playback ends, the message stays
// new, and the log names it.
func TestFB15_a_message_that_cannot_be_played_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	b := &box{messages: []Message{oma()}, playErr: errors.New("speaker unavailable")}
	run(t, b.machine(), ToolPlay, config.ChannelVoice, `{"ids":["0:13"]}`)
	if len(b.done) != 0 {
		t.Errorf("counted as heard: %v", b.done)
	}
	if lines.Line("level=ERROR", "could not play a message", "message=0:13", "speaker=media_player.show_speaker", "speaker unavailable") == "" {
		t.Errorf("no line for the failed playback:\n%s", lines)
	}
}
