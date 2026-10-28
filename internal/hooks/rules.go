package hooks

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.starlark.net/starlark"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/tools"
)

// Rules Mira writes and manages on request (docs/spec/hooks-schreiben.md):
// checking a rule, saving it, switching it on or off, deleting it, and the
// tools the skill "regel" brings. Every change passes the hooks first, as
// the tools hook_save, hook_switch and hook_delete (system/self.star).

//go:embed reference.md
var reference string

// Rule is a rule the agent wrote, as the field of its thread holds it.
// Error is the agent's sentence why it could not write one.
type Rule struct {
	File    string `json:"file"`
	Summary string `json:"summary"`
	Code    string `json:"code"`
	Error   string `json:"error,omitempty"`
}

// Tool names of the rules.
const (
	ToolSave      = "hook_save"
	ToolSwitch    = "hook_switch"
	ToolDelete    = "hook_delete"
	ToolList      = "hook_list"
	ToolTry       = "hook_try"
	ToolReference = "hook_reference"
)

var (
	safeName = regexp.MustCompile(`^[a-z0-9_]+\.star$`)
	activeRe = regexp.MustCompile(`(?m)^ACTIVE\s*=\s*(True|False)\s*$`)
	descRe   = regexp.MustCompile(`(?m)^DESCRIPTION\s*=.*$`)
	headRe   = regexp.MustCompile(`(?m)^# Written by Mira on (\S+) for (.+):\n# (.*)$`)
)

// fileName is a rule's name as a file: lower case, ending in .star.
func fileName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(name, ".star") {
		name += ".star"
	}
	return name
}

// protected: a name of Mira's own rules, which the household's rules never
// take over from her (critical.star only by hand).
func (e *Engine) protected(name string) bool {
	if name == criticalFile {
		return true
	}
	return slices.ContainsFunc(e.system, func(h *hook) bool { return h.name == name })
}

// Try checks a rule's code as the agent writes it: it loads, and its
// pre_tool does not fail on a first call.
func (e *Engine) Try(name, code string) error {
	h, err := e.compile(Household, fileName(name), []byte(code))
	if err != nil {
		return err
	}
	// A harmless read by someone nobody knows, and one by a known person:
	// the calls every rule sees most.
	for _, c := range []tools.Call{
		{Spec: tools.Spec{ToolDef: prompt.ToolDef{Server: "ha", Name: "GetLiveContext"}, ReadOnly: true},
			Channel: config.ChannelVoice, Time: time.Now()},
		{Spec: tools.Spec{ToolDef: prompt.ToolDef{Server: "ha", Name: "HassTurnOn"}}, Args: map[string]any{"name": "Licht"},
			Person: "someone", PersonName: "Someone", Channel: config.ChannelTelegram, Time: time.Now()},
	} {
		res, err := starlark.Call(e.thread(h), h.pre, starlark.Tuple{callValue(c), &verdictValue{kind: kindAllow}}, nil)
		if err == nil {
			_, err = asVerdict(res)
		}
		if err != nil {
			return fmt.Errorf(e.texts.smokeFailed, describeErr(err))
		}
	}
	return nil
}

// CheckRule is the check hook.check of a thread's field: the agent's rule
// as JSON. It must name a file that is free and no one of Mira's own, and
// its code must pass Try. It returns the rule as it will be saved.
func (e *Engine) CheckRule(value string) (string, error) {
	var r Rule
	if err := json.Unmarshal([]byte(extractJSON(value)), &r); err != nil {
		return "", fmt.Errorf("no rule as JSON: %w", err)
	}
	if r.Error != "" {
		return "", errors.New(r.Error)
	}
	r.File = fileName(r.File)
	switch {
	case strings.TrimSpace(r.Code) == "" || strings.TrimSpace(r.Summary) == "":
		return "", errors.New(e.texts.noDescription)
	case !safeName.MatchString(r.File):
		return "", fmt.Errorf(e.texts.badName, r.File)
	case e.protected(r.File):
		return "", fmt.Errorf(e.texts.protected, r.File)
	}
	if err := e.Try(r.File, r.Code); err != nil {
		return "", err
	}
	r.File = e.freeName(r.File)
	out, _ := json.Marshal(r)
	return string(out), nil
}

// extractJSON is the JSON object in a model's answer, which may come with
// text or a code fence around it.
func extractJSON(s string) string {
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

// freeName is name, or name with a number when a file of that name exists.
func (e *Engine) freeName(name string) string {
	base := strings.TrimSuffix(name, ".star")
	for n := 1; ; n++ {
		cand := name
		if n > 1 {
			cand = fmt.Sprintf("%s_%d.star", base, n)
		}
		if _, err := os.Stat(filepath.Join(e.dir, cand)); errors.Is(err, os.ErrNotExist) {
			return cand
		}
	}
}

// Save writes a rule into the household's folder, with a head that says who
// asked for it, when and in which words, and switched on; the hooks are
// loaded anew at once.
func (e *Engine) Save(r Rule, person, wish string, now time.Time) error {
	if e.dir == "" {
		return errors.New("no folder for rules")
	}
	if !safeName.MatchString(r.File) || e.protected(r.File) {
		return fmt.Errorf(e.texts.badName, r.File)
	}
	head := fmt.Sprintf("# Written by Mira on %s for %s:\n# %s\n", now.Format("2006-01-02"), person,
		strings.ReplaceAll(strconvQuote(wish), "\n", " "))
	code := setActive(strings.TrimSpace(r.Code), true)
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.dir, r.File), []byte(head+code+"\n"), 0o644); err != nil {
		return err
	}
	e.log.Info("hook written", "file", r.File, "person", person, "wish", wish)
	e.Reload()
	return nil
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(s), `"`, `'`) + `"`
}

// setActive sets the line ACTIVE, or adds it after DESCRIPTION.
func setActive(code string, on bool) string {
	line := "ACTIVE = False"
	if on {
		line = "ACTIVE = True"
	}
	if activeRe.MatchString(code) {
		return activeRe.ReplaceAllString(code, line)
	}
	if loc := descRe.FindStringIndex(code); loc != nil {
		return code[:loc[1]] + "\n" + line + code[loc[1]:]
	}
	return line + "\n" + code
}

// Switch switches a household rule on or off: only its line ACTIVE changes.
func (e *Engine) Switch(name string, on bool) error {
	path, err := e.household1(name)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(setActive(string(src), on)), 0o644); err != nil {
		return err
	}
	e.log.Info("hook switched", "file", filepath.Base(path), "on", on)
	e.Reload()
	return nil
}

// Delete deletes a household rule.
func (e *Engine) Delete(name string) error {
	path, err := e.household1(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	e.log.Info("hook deleted", "file", filepath.Base(path))
	e.Reload()
	return nil
}

// household1 is the path of one of the household's rules.
func (e *Engine) household1(name string) (string, error) {
	name = fileName(name)
	if e.protected(name) {
		return "", fmt.Errorf(e.texts.protected, name)
	}
	path := filepath.Join(e.dir, name)
	if e.dir == "" || !safeName.MatchString(name) {
		return "", fmt.Errorf(e.texts.notFound, name)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf(e.texts.notFound, name)
	}
	return path, nil
}

// list is hook_list's answer.
func (e *Engine) list() string {
	var lines []string
	for _, i := range e.Infos() {
		if i.Level != Household {
			continue
		}
		var line string
		switch {
		case i.Problem != "":
			line = fmt.Sprintf(e.texts.ruleBroken, i.Name, i.Problem)
		case i.Off:
			line = fmt.Sprintf(e.texts.ruleOff, i.Name, i.Description)
		default:
			line = fmt.Sprintf(e.texts.ruleOn, i.Name, i.Description)
		}
		if m := headRe.FindStringSubmatch(i.Source); m != nil {
			line += fmt.Sprintf(e.texts.askedBy, m[2], m[1], m[3])
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return e.texts.noRules
	}
	return strings.Join(lines, "\n")
}

// Tools are the tools the skill named skill brings: listing, switching and
// deleting rules for the conversation, trying a rule and its reference for
// the agent that writes it. house describes the house for the reference.
func (e *Engine) Tools(skill string, house func() string) []tools.InternalTool {
	t := e.texts
	type nameArgs struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
		Code string `json:"code"`
	}
	parse := func(raw string) nameArgs {
		var a nameArgs
		_ = json.Unmarshal([]byte(raw), &a)
		return a
	}
	schema := func(props string, required ...string) string {
		if required == nil {
			required = []string{}
		}
		req, _ := json.Marshal(required)
		return `{"type":"object","properties":{` + props + `},"required":` + string(req) + `}`
	}
	name := `"name":{"type":"string","description":` + jsonString(t.argName) + `}`
	return []tools.InternalTool{
		{Name: ToolList, Description: t.listDesc, Schema: schema(""), ReadOnly: true, Skill: skill,
			Run: func(context.Context, tools.Invocation) (string, error) { return e.list(), nil }},
		{Name: ToolSwitch, Description: t.switchDesc, Skill: skill,
			Schema: schema(name+`,"on":{"type":"boolean","description":`+jsonString(t.argOn)+`}`, "name", "on"),
			Run: func(_ context.Context, inv tools.Invocation) (string, error) {
				a := parse(string(inv.Args))
				if err := e.Switch(a.Name, a.On); err != nil {
					return "", err
				}
				if a.On {
					return fmt.Sprintf(t.switchedOn, fileName(a.Name)), nil
				}
				return fmt.Sprintf(t.switchedOff, fileName(a.Name)), nil
			}},
		{Name: ToolDelete, Description: t.deleteDesc, Skill: skill, Schema: schema(name, "name"),
			Run: func(_ context.Context, inv tools.Invocation) (string, error) {
				a := parse(string(inv.Args))
				if err := e.Delete(a.Name); err != nil {
					return "", err
				}
				return fmt.Sprintf(t.deleted, fileName(a.Name)), nil
			}},
		{Name: ToolTry, Description: t.tryDesc, ReadOnly: true, Skill: skill,
			Schema: schema(name+`,"code":{"type":"string","description":`+jsonString(t.argCode)+`}`, "code"),
			Run: func(_ context.Context, inv tools.Invocation) (string, error) {
				a := parse(string(inv.Args))
				if a.Name == "" {
					a.Name = "rule.star"
				}
				if err := e.Try(a.Name, a.Code); err != nil {
					return err.Error(), nil
				}
				return t.tryOK, nil
			}},
		{Name: ToolReference, Description: t.referenceDesc, ReadOnly: true, Skill: skill, Schema: schema(""),
			Run: func(context.Context, tools.Invocation) (string, error) {
				out := reference
				if house != nil {
					out += "\n\n# This house\n\n" + house()
				}
				return out, nil
			}},
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
