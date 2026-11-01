package main

import (
	"path/filepath"

	"github.com/miraassist/mira/internal/backup"
	"github.com/miraassist/mira/internal/config"
)

// backupMira is what goes into Mira's snapshot and what a restore writes
// (docs/spec/backup.md): her database, mira.yaml, the secrets and one's own
// skill folders.
func (a *app) backupMira() backup.Mira {
	var own []string
	for _, d := range a.cfg.Skills.Dirs {
		if filepath.Clean(d) != filepath.Clean(config.Default().Skills.Dirs[0]) {
			own = append(own, d)
		}
	}
	return backup.Mira{Store: a.store, DBPath: a.cfg.Store.Path, ConfigPath: a.configPath,
		Secrets: config.DefaultSecrets(), SkillDirs: own, DataDir: filepath.Dir(a.cfg.Store.Path), Version: version}
}
