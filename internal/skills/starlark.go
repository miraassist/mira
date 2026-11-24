package skills

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"slices"
	"strings"

	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/tools"
)

// toolsSection is metadata.mira.tools: which of the skill's Starlark tools
// write and which always ask (docs/spec/api-tools.md, decision 15).
type toolsSection struct {
	Writes  []string `yaml:"writes"`
	Confirm []string `yaml:"confirm"`
	// Reads are Mira's tools the Starlark tools may call with mira.tool
	// (decision 19), by their full names.
	Reads []string `yaml:"reads"`
}

// starlarkTools loads the skill's tools.star (docs/spec/api-tools.md,
// Loading): a skill with an api section brings one; a tools.star without
// one only computes and reads Mira's tools (decision 17); its settings that
// are not secret go to the file.
func starlarkTools(s *Skill, files fs.FS, sec *toolsSection, vals map[string]string, decl map[string]Setting, _ Options) *Problem {
	src, err := fs.ReadFile(files, startools.FileName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if len(s.APIPrefixes()) > 0 {
			return problem("starmissing")
		}
		if sec != nil {
			return problem("starnofile")
		}
		return nil
	case err != nil:
		return problem("unreadable", err)
	}
	plain := map[string]string{}
	for k, v := range vals {
		if d, ok := decl[k]; ok && !d.Secret {
			plain[k] = v
		}
	}
	f, err := startools.Load(s.Name, src, plain, nil)
	if err != nil {
		return problem("starload", err.Error())
	}
	if sec != nil {
		for _, list := range [][]string{sec.Writes, sec.Confirm} {
			for _, n := range list {
				if _, ok := f.Tool(n); !ok {
					return problem("starname", n, strings.Join(f.Names(), ", "))
				}
			}
		}
		s.Writes, s.Confirm, s.Reads = slices.Clone(sec.Writes), slices.Clone(sec.Confirm), slices.Clone(sec.Reads)
	}
	for _, sc := range s.Scripts {
		if _, ok := f.Tool(sc.ID); ok {
			return problem("starclash", sc.ID)
		}
	}
	s.Starlark = f
	return nil
}

// APIPrefixes are where the skill's Starlark tools may send: under its
// login's api section, or its own for a skill without a login.
func (s Skill) APIPrefixes() []APIPrefix {
	if s.OAuth != nil {
		return s.OAuth.API
	}
	return s.API
}

// StarlarkUsable reports whether the skill's Starlark tools may run: always for a
// skill without a login, else while it is connected.
func (s Skill) StarlarkUsable(conn Connector) bool {
	if s.OAuth == nil {
		return true
	}
	return conn != nil && conn.Connected(s.Name)
}

// WritesTool reports whether the Starlark tool is under tools.writes.
func (s Skill) WritesTool(name string) bool { return slices.Contains(s.Writes, name) }

// ConfirmsTool reports whether the Starlark tool is under tools.confirm.
func (s Skill) ConfirmsTool(name string) bool { return slices.Contains(s.Confirm, name) }

// Connector is what the Starlark tools need of the connections
// (oauth.Manager): the requests (with the token of a skill with a login),
// and whether a skill is connected.
type Connector interface {
	startools.Requester
	Connected(skill string) bool
}

// StarlarkTools are the skills' tools in tools.star (docs/spec/api-tools.md):
// tools of their skill, offered as its MCP tools are (after skill_open, to
// its schedules and agents); a skill's with a login only while it is
// connected. Their answers
// are third-party text; a tool under writes is no read-only tool and asks
// first after third-party text, one under confirm always.
func StarlarkTools(list []Skill, conn Connector, reader startools.Reader, log *slog.Logger) []tools.InternalTool {
	var out []tools.InternalTool
	for _, sk := range list {
		f := sk.Starlark
		if f == nil {
			continue
		}
		f.SetLogger(log)
		skill, sk := sk.Name, sk
		for _, t := range f.Tools {
			name := t.Name
			writes := sk.WritesTool(name)
			// Third-party text only from a skill that reaches the network;
			// one that only computes and reads Mira's tools does not
			// (docs/spec/api-tools.md, decision 17; mira.tool refuses
			// third-party text, decision 19).
			web := len(sk.APIPrefixes()) > 0
			out = append(out, tools.InternalTool{
				Name: skill + "__" + name, Description: t.Description, Schema: t.Schema,
				ReadOnly: !writes, WebContent: web, AskAfterWeb: writes, Confirm: sk.ConfirmsTool(name),
				Skill: skill, Background: true,
				Enabled: func() bool { return sk.StarlarkUsable(conn) },
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					res := f.Call(ctx, name, inv.Args, startools.Env{Requester: conn, Writes: writes, Reader: reader, Reads: sk.Reads})
					if res.Err != nil {
						return "", res.Err
					}
					return res.Text, nil
				},
			})
		}
	}
	return out
}
