package skills

import (
	"fmt"

	"github.com/miraassist/mira/internal/i18n"
)

// Problem is why an enabled skill did not start, worded so that whoever
// wrote or enabled it can see what to fix. A skill with a problem is left
// out; the agent starts all the same (docs/spec/skill-aktionen.md).
type Problem struct {
	Skill string
	kind  string
	args  []any
}

func problem(kind string, args ...any) *Problem { return &Problem{kind: kind, args: args} }

// NewProblem is for the checks outside this package (an unknown building
// block of a target).
func NewProblem(skill, kind string, args ...any) Problem {
	return Problem{Skill: skill, kind: kind, args: args}
}

// Kinds used outside the package.
const (
	KindUnknownFinish = "unknownfinish"
	KindUnknownAgent  = "unknownagent"
	KindNotLocal      = "notlocal"
	KindMCPDown       = "mcpdown"
	KindMCPTools      = "mcptools"
)

// Text is the problem in a language, without the skill's name.
func (p Problem) Text(l i18n.Lang) string {
	t := i18n.Pick(problemTexts, l)
	format, ok := t[p.kind]
	if !ok {
		return p.kind
	}
	return fmt.Sprintf(format, p.args...)
}

// Error is the English text, for logs and tests that read errors.
func (p *Problem) Error() string { return p.Skill + ": " + p.Text(i18n.EN) }

type problemTable map[string]string

var problemTexts = map[i18n.Lang]problemTable{
	i18n.DE: {
		"nofolder":       "Es gibt keinen Ordner „%[1]s“ mit einer Datei SKILL.md in %[2]s. Lege ihn dort an, oder nimm „%[1]s“ unter skills in der mira.yaml heraus (oder schreib dort enabled: false).",
		"unreadable":     "Die Datei SKILL.md lässt sich nicht lesen (%s).",
		"nofront":        "Die Datei SKILL.md muss oben mit einer Zeile --- beginnen; dahinter steht der Kopfbereich mit Name und Beschreibung.",
		"frontopen":      "Der Kopfbereich in SKILL.md ist nicht geschlossen: nach den Angaben fehlt eine Zeile mit ---.",
		"yaml":           "Der Kopfbereich von SKILL.md ist kein gültiges YAML (%s). Meist stimmt eine Einrückung oder ein Doppelpunkt nicht.",
		"name":           "Der Name im Kopfbereich („%s“) muss genauso heißen wie der Ordner und aus kleinen Wörtern mit Bindestrichen bestehen.",
		"nodesc":         "Im Kopfbereich fehlt die Beschreibung (description).",
		"nosettings":     "Die Einstellung „%s“ in der mira.yaml gibt es nicht: Dieser Skill hat keine Einstellungen.",
		"unknownsetting": "Die Einstellung „%s“ in der mira.yaml gibt es bei diesem Skill nicht. Möglich sind: %s.",
		"required":       "Die Einstellung „%[1]s“ fehlt in der mira.yaml unter skills.%[3]s. Sie wird gebraucht: %[2]s",
		"cron":           "Zeitplan %d: Die Zeitangabe (cron) ist ungültig (%s).",
		"notask":         "Zeitplan %d hat keine Aufgabe (task).",
		"targetneeds":    "Das Gespräch (target) braucht mindestens ein Feld (fields) und einen Abschluss (finish).",
		"targetplan":     "Ein Ablauf mit Plan (target mit plan: true) hat keine Felder (fields) und keinen Abschluss (finish).",
		"targetaction":   "Die Aktion „%s“ braucht einen Dienstbereich vor dem Punkt, zum Beispiel esphome.show_alarm_set.",
		"targetfield":    "Feld %d des Gesprächs (target) braucht einen Namen aus kleinen Wörtern und eine Beschreibung.",
		"targettimeout":  "Das Zeitlimit „%s“ ist keine Dauer wie 15m.",
		"targetbound":    "bound ist „%s“; möglich sind person und room (ohne Angabe: person).",
		"unknownfinish":  "Der Abschluss „%s“ ist Mira nicht bekannt. Bekannt sind: %s. Oder rufe stattdessen eine Home-Assistant-Aktion auf (finish: action).",
		"mailextract":    "Der Abschnitt mail braucht eine Anleitung (extract): wie Mira die Mails liest.",
		"agent":          "Der Agent „%s“ unter agents ist so nicht lesbar (%s).",
		"unknownagent":   "Das Gespräch (target) nennt den Agenten „%s“, den es nicht gibt. Beschreibe ihn unter agents, in der mira.yaml oder im Skill.",
		"privacyword":    "privacy ist „%s“; hier geht nur local.",
		"language":       "language ist „%s“; möglich sind de und en (ohne Angabe: de).",
		"translation":    "Die Übersetzung %s ist so nicht lesbar (%v).",
		"mcpurl":         "Der Abschnitt mcp braucht eine Adresse (url), meist aus einer Einstellung wie \"{{ url }}\".",
		"mcpcommand":     "Der Abschnitt mcp mit transport stdio braucht ein Programm (command).",
		"mcptransport":   "transport „%s“ im Abschnitt mcp geht nicht; möglich sind http und stdio.",
		"mcpdown":        "Der MCP-Server des Skills ist nicht erreichbar (%s). Der Skill ist da, aber ohne seine Werkzeuge; Mira versucht es weiter.",
		"mcptools":       "Diese Werkzeuge nennt der Skill unter tools, der Server hat sie aber nicht: %s. Die übrigen gehen.",
		"notlocal":       "Der Skill muss lokal laufen (privacy: local), aber sein Agent %s läuft auf dem Modell „%s“ (%s). Gib dem Agenten in der mira.yaml unter agents ein lokales Modell.",
	},
	i18n.EN: {
		"nofolder":       "There is no folder \"%[1]s\" with a SKILL.md file in %[2]s. Create it there, or take \"%[1]s\" out from under skills in mira.yaml (or write enabled: false there).",
		"unreadable":     "The SKILL.md file cannot be read (%s).",
		"nofront":        "SKILL.md must begin with a line of ---; the header with name and description follows.",
		"frontopen":      "The header in SKILL.md is not closed: a line with --- is missing after it.",
		"yaml":           "The header of SKILL.md is not valid YAML (%s). Usually an indent or a colon is wrong.",
		"name":           "The name in the header (\"%s\") must match the folder's name and be lower-case words with hyphens.",
		"nodesc":         "The header has no description.",
		"nosettings":     "The setting \"%s\" in mira.yaml does not exist: this skill has no settings.",
		"unknownsetting": "The setting \"%s\" in mira.yaml does not exist for this skill. Possible: %s.",
		"required":       "The setting \"%[1]s\" is missing in mira.yaml under skills.%[3]s. It is needed: %[2]s",
		"cron":           "Schedule %d: the time (cron) is invalid (%s).",
		"notask":         "Schedule %d has no task.",
		"targetneeds":    "The conversation (target) needs at least one field (fields) and a finish.",
		"targetplan":     "A run with a plan (target with plan: true) has no fields and no finish.",
		"targetaction":   "The action \"%s\" needs a domain before the dot, such as esphome.show_alarm_set.",
		"targetfield":    "Field %d of the conversation (target) needs a name in lower-case words and a description.",
		"targettimeout":  "The timeout \"%s\" is not a duration like 15m.",
		"targetbound":    "bound is \"%s\"; possible are person and room (none: person).",
		"unknownfinish":  "Mira does not know the finish \"%s\". Known: %s. Or call a Home Assistant action instead (finish: action).",
		"mailextract":    "The mail section needs instructions (extract): how Mira reads the mails.",
		"agent":          "The agent \"%s\" under agents cannot be read like this (%s).",
		"unknownagent":   "The conversation (target) names the agent \"%s\", which does not exist. Describe it under agents, in mira.yaml or in the skill.",
		"privacyword":    "privacy is \"%s\"; only local goes here.",
		"language":       "language is \"%s\"; possible are de and en (none: de).",
		"translation":    "The translation %s cannot be read like this (%v).",
		"mcpurl":         "The mcp section needs an address (url), usually from a setting like \"{{ url }}\".",
		"mcpcommand":     "The mcp section with transport stdio needs a program (command).",
		"mcptransport":   "transport \"%s\" in the mcp section does not work; possible are http and stdio.",
		"mcpdown":        "The skill's MCP server cannot be reached (%s). The skill is there, but without its tools; Mira keeps trying.",
		"mcptools":       "The skill names these tools under tools, but the server does not have them: %s. The others work.",
		"notlocal":       "The skill must run locally (privacy: local), but its agent %s runs on the model \"%s\" (%s). Give the agent a local model in mira.yaml under agents.",
	},
}
