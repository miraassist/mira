package setup

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

// A reset deletes Mira's data and keeps the config (mira.yaml, secrets):
// afterwards the agent is like a new one and pairs without a code
// (docs/spec/einrichtung.md). It is asked for on the agent's machine, never
// over the network: by mira reset (a mark next to the database), or as a
// Home Assistant app by the option reset_data. Either takes effect at the
// next start, before the database is opened.

// AskReset leaves the mark mira reset sets; the next start resets.
func AskReset(dbPath string) error {
	return os.WriteFile(dbPath+".reset", nil, 0o600)
}

// ResetIfAsked deletes the database when asked: by the mark, or by the app's
// option in optionsPath ("" outside the app). The option holds while it is
// on, so it resets once, until it is turned off and on again. It says
// whether it reset.
func ResetIfAsked(dbPath, optionsPath string) (bool, error) {
	asked := exists(dbPath + ".reset")
	done := dbPath + ".reset-done"
	if optionsPath != "" {
		var options struct {
			ResetData bool `json:"reset_data"`
		}
		if raw, err := os.ReadFile(optionsPath); err == nil && json.Unmarshal(raw, &options) == nil {
			switch {
			case !options.ResetData:
				if err := remove(done); err != nil {
					return false, err
				}
			case !exists(done):
				asked = true
				if err := os.WriteFile(done, nil, 0o600); err != nil {
					return false, err
				}
			}
		}
	}
	if !asked {
		return false, nil
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", dbPath + ".reset"} {
		if err := remove(p); err != nil {
			return false, err
		}
	}
	return true, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
