package skills

import (
	"fmt"

	"github.com/miraassist/mira/internal/i18n"
)

// Problem is why an enabled skill did not start, worded so that whoever
// wrote or enabled it can see what to fix. A skill with a problem is left
// out; the service starts all the same (docs/spec/skill-actions.md).
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
	KindOAuthExpired  = "oauthexpired"
	KindOAuthNone     = "oauthnone"
	KindNoAnnounce    = "noannounce"
	// A remote agent (docs/spec/remote-agents.md): its card cannot be
	// read, is refused, or says something Mira cannot use.
	KindRemoteDown      = "remotedown"
	KindRemoteRefused   = "remoterefused"
	KindRemoteNoWay     = "remotenoway"
	KindRemoteNoAddress = "remotenoaddress"
	KindRemoteScheme    = "remotescheme"
	KindRemoteOAuth     = "remoteoauth"
	KindRemoteToken     = "remotetoken"
	KindRemoteSkill     = "remoteskill"
	KindRemoteTransport = "remotetransport"
	KindRemoteElsewhere = "remoteelsewhere"
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
		"nofolder":         "Es gibt keinen Ordner „%[1]s“ mit einer Datei SKILL.md in %[2]s. Lege ihn dort an, oder nimm „%[1]s“ unter skills in der mira.yaml heraus (oder schreib dort enabled: false).",
		"unreadable":       "Die Datei SKILL.md lässt sich nicht lesen (%s).",
		"nofront":          "Die Datei SKILL.md muss oben mit einer Zeile --- beginnen; dahinter steht der Kopfbereich mit Name und Beschreibung.",
		"frontopen":        "Der Kopfbereich in SKILL.md ist nicht geschlossen: nach den Angaben fehlt eine Zeile mit ---.",
		"yaml":             "Der Kopfbereich von SKILL.md ist kein gültiges YAML (%s). Meist stimmt eine Einrückung oder ein Doppelpunkt nicht.",
		"name":             "Der Name im Kopfbereich („%s“) muss genauso heißen wie der Ordner und aus kleinen Wörtern mit Bindestrichen bestehen.",
		"nodesc":           "Im Kopfbereich fehlt die Beschreibung (description).",
		"nosettings":       "Die Einstellung „%s“ in der mira.yaml gibt es nicht: Dieser Skill hat keine Einstellungen.",
		"unknownsetting":   "Die Einstellung „%s“ in der mira.yaml gibt es bei diesem Skill nicht. Möglich sind: %s.",
		"required":         "Die Einstellung „%[1]s“ fehlt in der mira.yaml unter skills.%[3]s. Sie wird gebraucht: %[2]s",
		"cron":             "Zeitplan %d: Die Zeitangabe (cron) ist ungültig (%s).",
		"notask":           "Zeitplan %d hat keine Aufgabe (task).",
		"toperperson":      "Zeitplan %d hat to und per_person; ein Zeitplan geht entweder an eine Person (to) oder an jede (per_person).",
		"schedulessetting": "Eine Einstellung darf nicht schedules heißen: Unter diesem Namen ändert die mira.yaml die Zeitpläne.",
		"scheduledup":      "Zwei Zeitpläne heißen „%s“; jeder braucht einen eigenen Namen (name).",
		"scheduleentry":    "Der Eintrag schedules in der mira.yaml ist so nicht lesbar: %v.",
		"schedulecron":     "Die Zeit für den Zeitplan „%s“ in der mira.yaml ist ungültig (%s).",
		"scheduleunknown":  "Einen Zeitplan „%s“ hat dieser Skill nicht; nimm ihn unter schedules in der mira.yaml heraus.",
		"targetneeds":      "Das Gespräch (target) braucht mindestens ein Feld (fields) und einen Abschluss (finish).",
		"targetplan":       "Ein Ablauf mit Plan (target mit plan: true) hat keine Felder (fields) und keinen Abschluss (finish).",
		"targetaction":     "Die Aktion „%s“ braucht einen Dienstbereich vor dem Punkt, zum Beispiel esphome.show_alarm_set.",
		"targetfield":      "Feld %d des Gesprächs (target) braucht einen Namen aus kleinen Wörtern und eine Beschreibung.",
		"targettimeout":    "Das Zeitlimit „%s“ ist keine Dauer wie 15m.",
		"targetbound":      "bound ist „%s“; möglich sind person und room (ohne Angabe: person).",
		"unknownfinish":    "Der Abschluss „%s“ ist Mira nicht bekannt. Bekannt sind: %s. Oder rufe stattdessen eine Home-Assistant-Aktion auf (finish: action).",
		"mailextract":      "Der Abschnitt mail braucht eine Anleitung (extract): wie Mira die Mails liest.",
		"inboxextract":     "Der Abschnitt inbox braucht eine Anleitung (extract): wie Mira die Dokumente liest.",
		"agent":            "Der Agent „%s“ unter agents ist so nicht lesbar (%s).",
		"unknownagent":     "Das Gespräch (target) nennt den Agenten „%s“, den es nicht gibt. Beschreibe ihn unter agents, in der mira.yaml oder im Skill.",
		"privacyword":      "privacy ist „%s“; hier geht nur local.",
		"language":         "language ist „%s“; möglich sind de und en (ohne Angabe: de).",
		"translation":      "Die Übersetzung %s ist so nicht lesbar (%v).",
		"randomnone":       "%s: Unter random steht keine Achse; schreib darunter Achsen mit ihren Werten, oder nimm random heraus.",
		"randomaxis":       "%s: Die Achse „%s“ unter random braucht eine Liste von Werten, oder Werte mit je einer Liste darunter (nicht tiefer).",
		"randomempty":      "%s: Die Achse „%s“ unter random hat keinen Wert, oder einer ihrer Werte hat keine Liste darunter.",
		"mcpurl":           "Der Abschnitt mcp braucht eine Adresse (url), meist aus einer Einstellung wie \"{{ url }}\".",
		"mcpcommand":       "Der Abschnitt mcp mit transport stdio braucht ein Programm (command).",
		"mcptransport":     "transport „%s“ im Abschnitt mcp geht nicht; möglich sind http und stdio.",
		"mcpdown":          "Der MCP-Server des Skills ist nicht erreichbar (%s). Der Skill ist da, aber ohne seine Werkzeuge; Mira versucht es weiter.",
		"mcptools":         "Diese Werkzeuge nennt der Skill unter tools, der Server hat sie aber nicht: %s. Die übrigen gehen.",
		"oauthurl":         "Der Abschnitt oauth braucht authorize_url und token_url, beide als https-Adresse.",
		"oauthauth":        "token_auth „%s“ im Abschnitt oauth geht nicht; möglich sind post und basic.",
		"oauthapi":         "Die Adresse „%s“ unter api im Abschnitt oauth muss eine https-Adresse sein.",
		"oauthmethod":      "Die Methode „%s“ für %s unter api geht nicht; möglich sind GET, POST, PUT, PATCH und DELETE.",
		"oauthmcp":         "Der Abschnitt mcp nimmt token: oauth, aber der Skill hat keinen Abschnitt oauth.",
		"oauthnone":        "Der Skill ist noch nicht mit %s verbunden. Verbinde ihn in Mira auf der Seite Verbindungen.",
		"oauthexpired":     "Die Verbindung mit %s ist abgelaufen oder wurde widerrufen. Verbinde den Skill in Mira auf der Seite Verbindungen neu.",
		"noannounce":       "In SKILL.md fehlt der Abschnitt „## %s“. Er sagt, wie Mira die Nachrichten dieses Skills ansagt; bis er da ist, sagt sie sie nicht an.",
		"notlocal":         "Der Skill muss lokal laufen (privacy: local), aber sein Agent %s läuft auf dem Modell „%s“ (%s). Gib dem Agenten in der mira.yaml unter agents ein lokales Modell.",
		"agentname":        "Der Agent „%s“ hat einen Namen (name); den gibt es nur für einen Agenten, der anderswo läuft (remote).",
		"remotefields":     "Der Agent „%s“ läuft anderswo (remote); %s geht dann nicht: Modell, Werkzeuge und Anleitung hat er selbst. Lass das Feld weg.",
		"remoteprivacy":    "Der Skill muss lokal laufen (privacy: local), aber sein Agent „%s“ läuft anderswo (remote); Mira kann nicht wissen, wo er die Daten verarbeitet.",
		"remoteurl":        "Der Agent „%s“ unter remote braucht eine Adresse (url) mit http:// oder https://, meist aus einer Einstellung wie \"{{ url }}\"; da steht „%s“.",
		"remotedown":       "%s ist nicht erreichbar (%s). Der Skill ist da, aber ohne diesen Agenten; Mira versucht es weiter.",
		"remoterefused":    "%s gibt seine Beschreibung nicht heraus (%s). Prüfe die Adresse in den Einstellungen des Skills; Mira versucht es erst nach einem Neustart wieder.",
		"remotenoway":      "%s kann Ergebnisse weder schicken (Push) noch streamen; so bekommt Mira kein Ergebnis. Der Skill ist da, aber ohne diesen Agenten.",
		"remotenoaddress":  "%s schickt Ergebnisse nur als Push, aber Miras Adresse für entfernte Agenten fehlt (server.url in der mira.yaml, zum Beispiel http://192.168.1.10:8787). Der Skill ist da, aber ohne diesen Agenten.",
		"remotescheme":     "%s verlangt eine Anmeldung, die Mira nicht kann (%s). Mira kann einen Token (Bearer), einen API-Schlüssel und OAuth2. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteoauth":      "%s verlangt eine Anmeldung mit OAuth2 (%s), aber der Skill hat keinen Abschnitt oauth für die Verbindung. Der Skill ist da, aber ohne diesen Agenten.",
		"remotetoken":      "%s verlangt einen Token (%s), aber die Einstellung des Skills dafür ist leer. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteskill":      "%s hat keinen Skill „%s“ (a2a_skill). Seine Skills: %s. Der Skill ist da, aber ohne diesen Agenten.",
		"remotetransport":  "%s nennt keine Schnittstelle, die Mira spricht (%s); Mira spricht A2A 1.0 über JSON-RPC und HTTP+JSON. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteelsewhere":  "%s nimmt Anfragen unter %s an, nicht unter seiner Adresse %s; Mira schickt den Token nur an die Adresse des Skills. Trag in den Einstellungen des Skills die Adresse ein, unter der er Anfragen annimmt.",
	},
	i18n.EN: {
		"nofolder":         "There is no folder \"%[1]s\" with a SKILL.md file in %[2]s. Create it there, or take \"%[1]s\" out from under skills in mira.yaml (or write enabled: false there).",
		"unreadable":       "The SKILL.md file cannot be read (%s).",
		"nofront":          "SKILL.md must begin with a line of ---; the header with name and description follows.",
		"frontopen":        "The header in SKILL.md is not closed: a line with --- is missing after it.",
		"yaml":             "The header of SKILL.md is not valid YAML (%s). Usually an indent or a colon is wrong.",
		"name":             "The name in the header (\"%s\") must match the folder's name and be lower-case words with hyphens.",
		"nodesc":           "The header has no description.",
		"nosettings":       "The setting \"%s\" in mira.yaml does not exist: this skill has no settings.",
		"unknownsetting":   "The setting \"%s\" in mira.yaml does not exist for this skill. Possible: %s.",
		"required":         "The setting \"%[1]s\" is missing in mira.yaml under skills.%[3]s. It is needed: %[2]s",
		"cron":             "Schedule %d: the time (cron) is invalid (%s).",
		"notask":           "Schedule %d has no task.",
		"toperperson":      "Schedule %d has to and per_person; a schedule goes either to one person (to) or to each (per_person).",
		"schedulessetting": "A setting must not be called schedules: under that name mira.yaml changes the schedules.",
		"scheduledup":      "Two schedules are named \"%s\"; each needs a name of its own.",
		"scheduleentry":    "The entry schedules in mira.yaml cannot be read: %v.",
		"schedulecron":     "The time for the schedule \"%s\" in mira.yaml is invalid (%s).",
		"scheduleunknown":  "This skill has no schedule \"%s\"; take it out from under schedules in mira.yaml.",
		"targetneeds":      "The conversation (target) needs at least one field (fields) and a finish.",
		"targetplan":       "A run with a plan (target with plan: true) has no fields and no finish.",
		"targetaction":     "The action \"%s\" needs a domain before the dot, such as esphome.show_alarm_set.",
		"targetfield":      "Field %d of the conversation (target) needs a name in lower-case words and a description.",
		"targettimeout":    "The timeout \"%s\" is not a duration like 15m.",
		"targetbound":      "bound is \"%s\"; possible are person and room (none: person).",
		"unknownfinish":    "Mira does not know the finish \"%s\". Known: %s. Or call a Home Assistant action instead (finish: action).",
		"mailextract":      "The mail section needs instructions (extract): how Mira reads the mails.",
		"inboxextract":     "The inbox section needs instructions (extract): how Mira reads the documents.",
		"agent":            "The agent \"%s\" under agents cannot be read like this (%s).",
		"unknownagent":     "The conversation (target) names the agent \"%s\", which does not exist. Describe it under agents, in mira.yaml or in the skill.",
		"privacyword":      "privacy is \"%s\"; only local goes here.",
		"language":         "language is \"%s\"; possible are de and en (none: de).",
		"translation":      "The translation %s cannot be read like this (%v).",
		"randomnone":       "%s: there is no axis under random; write axes with their values under it, or take random out.",
		"randomaxis":       "%s: the axis \"%s\" under random needs a list of values, or values with a list under each (no deeper).",
		"randomempty":      "%s: the axis \"%s\" under random has no value, or one of its values has no list under it.",
		"mcpurl":           "The mcp section needs an address (url), usually from a setting like \"{{ url }}\".",
		"mcpcommand":       "The mcp section with transport stdio needs a program (command).",
		"mcptransport":     "transport \"%s\" in the mcp section does not work; possible are http and stdio.",
		"mcpdown":          "The skill's MCP server cannot be reached (%s). The skill is there, but without its tools; Mira keeps trying.",
		"mcptools":         "The skill names these tools under tools, but the server does not have them: %s. The others work.",
		"oauthurl":         "The oauth section needs authorize_url and token_url, both as https addresses.",
		"oauthauth":        "token_auth \"%s\" in the oauth section does not work; possible are post and basic.",
		"oauthapi":         "The address \"%s\" under api in the oauth section must be an https address.",
		"oauthmethod":      "The method \"%s\" for %s under api does not work; possible are GET, POST, PUT, PATCH and DELETE.",
		"oauthmcp":         "The mcp section takes token: oauth, but the skill has no oauth section.",
		"oauthnone":        "The skill is not connected with %s yet. Connect it in Mira on the Connections page.",
		"oauthexpired":     "The connection with %s has expired or was revoked. Connect the skill again in Mira on the Connections page.",
		"noannounce":       "SKILL.md has no section \"## %s\". It says how Mira announces this skill's messages; until it is there, she does not announce them.",
		"notlocal":         "The skill must run locally (privacy: local), but its agent %s runs on the model \"%s\" (%s). Give the agent a local model in mira.yaml under agents.",
		"agentname":        "The agent \"%s\" has a name; that is only for an agent that runs elsewhere (remote).",
		"remotefields":     "The agent \"%s\" runs elsewhere (remote); %s does not go with it: it has its own model, tools and instructions. Leave the field out.",
		"remoteprivacy":    "The skill must run locally (privacy: local), but its agent \"%s\" runs elsewhere (remote); Mira cannot know where it handles the data.",
		"remoteurl":        "The agent \"%s\" under remote needs an address (url) with http:// or https://, usually from a setting like \"{{ url }}\"; it says \"%s\".",
		"remotedown":       "%s cannot be reached (%s). The skill is there, but without this agent; Mira keeps trying.",
		"remoterefused":    "%s does not give out its description (%s). Check the address in the skill's settings; Mira tries again only after a restart.",
		"remotenoway":      "%s can neither send results (push) nor stream them, so Mira gets no result. The skill is there, but without this agent.",
		"remotenoaddress":  "%s sends results only as a push, but Mira's address for remote agents is missing (server.url in mira.yaml, e.g. http://192.168.1.10:8787). The skill is there, but without this agent.",
		"remotescheme":     "%s asks for a login Mira cannot do (%s). Mira can do a token (bearer), an API key and OAuth2. The skill is there, but without this agent.",
		"remoteoauth":      "%s asks for a login with OAuth2 (%s), but the skill has no oauth section for the connection. The skill is there, but without this agent.",
		"remotetoken":      "%s asks for a token (%s), but the skill's setting for it is empty. The skill is there, but without this agent.",
		"remoteskill":      "%s has no skill \"%s\" (a2a_skill). Its skills: %s. The skill is there, but without this agent.",
		"remotetransport":  "%s names no interface Mira speaks (%s); Mira speaks A2A 1.0 over JSON-RPC and HTTP+JSON. The skill is there, but without this agent.",
		"remoteelsewhere":  "%s takes requests at %s, not under its address %s; Mira sends the token only to the skill's address. Enter the address where it takes requests in the skill's settings.",
	},
}
