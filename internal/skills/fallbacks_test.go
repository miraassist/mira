package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// A skill whose SKILL.md cannot be read is missing from the list, and the
// log names it; a folder without SKILL.md and a skills folder not made yet
// stay without a line.
func TestFB22_a_skill_that_cannot_be_read_is_in_the_log(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	lines := logcap.Default(t)
	dir := t.TempDir()
	for _, name := range []string{"lesbar", "gesperrt"} {
		os.MkdirAll(filepath.Join(dir, name), 0o755)
		os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: x\n---\nText\n"), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "kein-skill"), 0o755)
	locked := filepath.Join(dir, "gesperrt", "SKILL.md")
	os.Chmod(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	got := Available([]string{dir, filepath.Join(dir, "gibt-es-nicht")}, nil)
	if len(got) != 1 || got[0].Name != "lesbar" {
		t.Errorf("available %+v", got)
	}
	if lines.Line("level=WARN", "could not read a skill", "skill=gesperrt") == "" {
		t.Errorf("no line for the locked skill:\n%s", lines)
	}
	if line := lines.Line("kein-skill"); line != "" {
		t.Errorf("a folder without SKILL.md has a line: %s", line)
	}
	if line := lines.Line("gibt-es-nicht"); line != "" {
		t.Errorf("a missing folder has a line: %s", line)
	}
}
