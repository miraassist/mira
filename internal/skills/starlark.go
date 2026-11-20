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
}

// starlarkTools loads the skill's tools.star (docs/spec/api-tools.md,
// Loading): a skill with an api section brings one, and a tools.star needs
// an api section; its settings that are not secret go to the file.
func starlarkTools(s *Skill, files fs.FS, sec *toolsSection, vals map[string]string, decl map[string]Setting, _ Options) *Problem {
	src, err := fs.ReadFile(files, startools.FileName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if s.OAuth != nil && len(s.OAuth.API) > 0 {
			return problem("starmissing")
		}
		if sec != nil {
			return problem("starnofile")
		}
		return nil
	case err != nil:
		return problem("unreadable", err)
	}
	if s.OAuth == nil || len(s.OAuth.API) == 0 {
		return problem("starnoapi")
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
		s.Writes, s.Confirm = slices.Clone(sec.Writes), slices.Clone(sec.Confirm)
	}
	for _, sc := range s.Scripts {
		if _, ok := f.Tool(sc.ID); ok {
			return problem("starclash", sc.ID)
		}
	}
	s.Starlark = f
	return nil
}

// WritesTool reports whether the Starlark tool is under tools.writes.
func (s Skill) WritesTool(name string) bool { return slices.Contains(s.Writes, name) }

// ConfirmsTool reports whether the Starlark tool is under tools.confirm.
func (s Skill) ConfirmsTool(name string) bool { return slices.Contains(s.Confirm, name) }

// Connector is what the Starlark tools need of the connections
// (oauth.Manager): the requests with the token, and whether a skill is
// connected.
type Connector interface {
	startools.Requester
	Connected(skill string) bool
}

// StarlarkTools are the skills' tools in tools.star (docs/spec/api-tools.md):
// tools of their skill, offered as its MCP tools are (after skill_open, to
// its schedules and agents) and only while it is connected. Their answers
// are third-party text; a tool under writes is no read-only tool and asks
// first after third-party text, one under confirm always.
func StarlarkTools(list []Skill, conn Connector, log *slog.Logger) []tools.InternalTool {
	var out []tools.InternalTool
	for _, sk := range list {
		f := sk.Starlark
		if f == nil {
			continue
		}
		f.SetLogger(log)
		skill := sk.Name
		for _, t := range f.Tools {
			name := t.Name
			writes := sk.WritesTool(name)
			out = append(out, tools.InternalTool{
				Name: skill + "__" + name, Description: t.Description, Schema: t.Schema,
				ReadOnly: !writes, WebContent: true, AskAfterWeb: writes, Confirm: sk.ConfirmsTool(name),
				Skill: skill, Background: true,
				Enabled: func() bool { return conn != nil && conn.Connected(skill) },
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					res := f.Call(ctx, name, inv.Args, startools.Env{Requester: conn, Writes: writes})
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
