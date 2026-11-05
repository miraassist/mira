package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// A skill the manifest names is not in the archive: the restore goes on
// without it, and the log names it.
func TestFB23_a_skill_of_a_backup_that_is_not_restored_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files(t, snapshot(t, mira(t, "", "alt"))) {
		if strings.HasPrefix(name, skillsDir+"/") {
			continue // the manifest still names the skill
		}
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()

	restoreInto(t, mira(t, "", "neu"), buf.Bytes())
	if lines.Line("level=WARN", "not in it", "skill=blumen") == "" {
		t.Errorf("no line for the missing skill:\n%s", lines)
	}
}
