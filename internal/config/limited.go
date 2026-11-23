package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Limited is the config Mira starts limited with when mira.yaml cannot be
// read or does not fit (docs/spec/parsing.md, PA-07): the defaults, with
// where Mira listens, her token and her store taken from the file wherever
// its YAML can be read at all, so Home Assistant finds and reaches her and
// the problem reaches its repair. Nothing else of the file is used.
func Limited(path string) Config {
	cfg := Default()
	raw, err := os.ReadFile(ConfigPath(path))
	if err != nil {
		return cfg
	}
	lookup := OSLookup
	secrets := DefaultSecrets()
	if _, err := secrets.Load(); err == nil {
		lookup = secrets.Lookup(OSLookup)
	}
	if expanded, err := expandEnv(raw, lookup); err == nil {
		raw = expanded
	}
	var part struct {
		Server struct {
			Listen string `yaml:"listen"`
			Token  string `yaml:"token"`
		} `yaml:"server"`
		Store struct {
			Path string `yaml:"path"`
		} `yaml:"store"`
	}
	// A file whose YAML does not parse at all leaves the defaults; its error
	// is the problem the limited start reports.
	if yaml.Unmarshal(raw, &part) != nil {
		return cfg
	}
	if part.Server.Listen != "" {
		cfg.Server.Listen = part.Server.Listen
	}
	if part.Server.Token != "" {
		cfg.Server.Token = part.Server.Token
	}
	if part.Store.Path != "" {
		cfg.Store.Path = part.Store.Path
	}
	return cfg
}
