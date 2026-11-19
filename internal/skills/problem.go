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
	// KindTakesTwice: two switched-on skills take the same kind of message
	// (docs/spec/messages.md, decision 33).
	KindTakesTwice = "takestwice"
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
	// The skill source (docs/spec/skill-source.md, Errors): the problem's
	// skill is the source's host for what concerns the whole source.
	KindSourceDown  = "sourcedown"
	KindSourceKey   = "sourcekey"
	KindSourceNot   = "sourcenot"
	KindSourceURL   = "sourceurl"
	KindSourceSkill = "sourceskill"
	KindSourceTwice = "sourcetwice"
	// Skill blueprints and skill automations (docs/spec/skill-files.md,
	// Errors): the skill runs, without them.
	KindBlueprintUnusable = "blueprintunusable"
	KindBlueprintList     = "blueprintlist"
	KindBlueprintSave     = "blueprintsave"
	KindBlueprintRefused  = "blueprintrefused"
	KindAutomationInput   = "automationinput"
	KindAutomationNeeds   = "automationneeds"
	KindAutomationRead    = "automationread"
	KindAutomationSave    = "automationsave"
	// Skill scripts (docs/spec/skill-files.md, step 5).
	KindBlueprintKind = "blueprintkind"
	KindScriptInput   = "scriptinput"
	KindScriptNeeds   = "scriptneeds"
	KindScriptRead    = "scriptread"
	KindScriptSave    = "scriptsave"
	KindScriptTool    = "scripttool"
	KindScriptTools   = "scripttools"
	// Programs for skills (docs/spec/skill-programs.md): the download.
	KindProgramDownload = "programdownload"
	KindProgramSum      = "programsum"
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
		"nofolder":            "Es gibt keinen Ordner „%[1]s“ mit einer Datei SKILL.md in %[2]s. Lege ihn dort an, oder nimm „%[1]s“ unter skills in der mira.yaml heraus (oder schreib dort enabled: false).",
		"unreadable":          "Die Datei SKILL.md lässt sich nicht lesen (%s).",
		"nofront":             "Die Datei SKILL.md muss oben mit einer Zeile --- beginnen; dahinter steht der Kopfbereich mit Name und Beschreibung.",
		"frontopen":           "Der Kopfbereich in SKILL.md ist nicht geschlossen: nach den Angaben fehlt eine Zeile mit ---.",
		"yaml":                "Der Kopfbereich von SKILL.md ist kein gültiges YAML (%s). Meist stimmt eine Einrückung oder ein Doppelpunkt nicht.",
		"name":                "Der Name im Kopfbereich („%s“) muss genauso heißen wie der Ordner und aus kleinen Wörtern mit Bindestrichen bestehen.",
		"nodesc":              "Im Kopfbereich fehlt die Beschreibung (description).",
		"nosettings":          "Die Einstellung „%s“ in der mira.yaml gibt es nicht: Dieser Skill hat keine Einstellungen.",
		"unknownsetting":      "Die Einstellung „%s“ in der mira.yaml gibt es bei diesem Skill nicht. Möglich sind: %s.",
		"required":            "Die Einstellung „%[1]s“ fehlt in der mira.yaml unter skills.%[3]s. Sie wird gebraucht: %[2]s",
		"cron":                "Zeitplan %d: Die Zeitangabe (cron) ist ungültig (%s).",
		"notask":              "Zeitplan %d hat keine Aufgabe (task).",
		"toperperson":         "Zeitplan %d hat to und per_person; ein Zeitplan geht entweder an eine Person (to) oder an jede (per_person).",
		"schedulessetting":    "Eine Einstellung darf nicht schedules heißen: Unter diesem Namen ändert die mira.yaml die Zeitpläne.",
		"scheduledup":         "Zwei Zeitpläne heißen „%s“; jeder braucht einen eigenen Namen (name).",
		"scheduleentry":       "Der Eintrag schedules in der mira.yaml ist so nicht lesbar: %v.",
		"schedulecron":        "Die Zeit für den Zeitplan „%s“ in der mira.yaml ist ungültig (%s).",
		"scheduleunknown":     "Einen Zeitplan „%s“ hat dieser Skill nicht; nimm ihn unter schedules in der mira.yaml heraus.",
		"targetneeds":         "Das Gespräch (target) braucht mindestens ein Feld (fields) und einen Abschluss (finish).",
		"targetplan":          "Ein Ablauf mit Plan (target mit plan: true) hat keine Felder (fields) und keinen Abschluss (finish).",
		"targetaction":        "Die Aktion „%s“ braucht einen Dienstbereich vor dem Punkt, zum Beispiel esphome.show_alarm_set.",
		"targetfield":         "Feld %d des Gesprächs (target) braucht einen Namen aus kleinen Wörtern und eine Beschreibung.",
		"targettimeout":       "Das Zeitlimit „%s“ ist keine Dauer wie 15m.",
		"targetbound":         "bound ist „%s“; möglich sind person und room (ohne Angabe: person).",
		"unknownfinish":       "Der Abschluss „%s“ ist Mira nicht bekannt. Bekannt sind: %s. Oder rufe stattdessen eine Home-Assistant-Aktion auf (finish: action).",
		"mailextract":         "Der Abschnitt mail braucht eine Anleitung (extract): wie Mira die Mails liest.",
		"inboxextract":        "Der Abschnitt inbox braucht eine Anleitung (extract): wie Mira die Dokumente liest.",
		"agent":               "Der Agent „%s“ unter agents ist so nicht lesbar (%s).",
		"unknownagent":        "Das Gespräch (target) nennt den Agenten „%s“, den es nicht gibt. Beschreibe ihn unter agents, in der mira.yaml oder im Skill.",
		"privacyword":         "privacy ist „%s“; hier geht nur local.",
		"language":            "language ist „%s“; möglich sind de und en (ohne Angabe: de).",
		"translation":         "Die Übersetzung %s ist so nicht lesbar (%v).",
		"randomnone":          "%s: Unter random steht keine Achse; schreib darunter Achsen mit ihren Werten, oder nimm random heraus.",
		"randomaxis":          "%s: Die Achse „%s“ unter random braucht eine Liste von Werten, oder Werte mit je einer Liste darunter (nicht tiefer).",
		"randomempty":         "%s: Die Achse „%s“ unter random hat keinen Wert, oder einer ihrer Werte hat keine Liste darunter.",
		"mcpurl":              "Der Abschnitt mcp braucht eine Adresse (url), meist aus einer Einstellung wie \"{{ url }}\".",
		"mcpcommand":          "Der Abschnitt mcp mit transport stdio braucht ein Programm (command).",
		"mcptransport":        "transport „%s“ im Abschnitt mcp geht nicht; möglich sind http und stdio.",
		"mcpconfirm":          "Diese Werkzeuge stehen unter confirm, aber nicht unter tools: %s. Nimm sie unter tools auf oder aus confirm heraus.",
		"programfields":       "Der Abschnitt program braucht name, version, url und sha256.",
		"mcpsetup":            "Das Werkzeug %s steht unter setup und zugleich unter tools oder confirm. Werkzeuge zum Einrichten sind nur für die Seite, nie für ein Modell: nimm es aus tools und confirm heraus.",
		"mcpsetupentry":       "Jeder Eintrag unter setup braucht tool und label.",
		"programname":         "name und version im Abschnitt program dürfen nur Buchstaben, Ziffern, Punkt, Binde- und Unterstrich enthalten: %q, %q.",
		"programarch":         "%s gibt es nicht für diese Architektur (%s): unter sha256 fehlt sie.",
		"programurl":          "Die Adresse des Programms muss mit https beginnen: %s",
		"programsumformat":    "sha256 für %s ist keine SHA-256-Summe (64 Hex-Zeichen).",
		"programdownload":     "Das Programm ließ sich nicht von %s laden: %v. Mira versucht es in 10 Minuten wieder.",
		"programsum":          "Das Programm von %s ist nicht das erwartete (SHA-256 erwartet %s, bekommen %s). Es wird weder gespeichert noch gestartet.",
		"skilldata":           "Der Ordner des Skills ließ sich nicht anlegen: %v",
		"reservedsetting":     "Die Einstellung %q heißt wie ein Platzhalter von Mira (data, arch). Nenn sie anders.",
		"mcpdown":             "Der MCP-Server des Skills ist nicht erreichbar (%s). Der Skill ist da, aber ohne seine Werkzeuge; Mira versucht es weiter.",
		"mcptools":            "Diese Werkzeuge nennt der Skill unter tools, der Server hat sie aber nicht: %s. Die übrigen gehen.",
		"oauthurl":            "Der Abschnitt oauth braucht authorize_url und token_url, beide als https-Adresse.",
		"oauthauth":           "token_auth „%s“ im Abschnitt oauth geht nicht; möglich sind post und basic.",
		"oauthapi":            "Die Adresse „%s“ unter api im Abschnitt oauth muss eine https-Adresse sein.",
		"oauthmethod":         "Die Methode „%s“ für %s unter api geht nicht; möglich sind GET, POST, PUT, PATCH und DELETE.",
		"oauthmcp":            "Der Abschnitt mcp nimmt token: oauth, aber der Skill hat keinen Abschnitt oauth.",
		"oauthnone":           "Der Skill ist noch nicht mit %s verbunden. Verbinde ihn in Mira auf der Seite Verbindungen.",
		"oauthexpired":        "Die Verbindung mit %s ist abgelaufen oder wurde widerrufen. Verbinde den Skill in Mira auf der Seite Verbindungen neu.",
		"noannounce":          "In SKILL.md fehlt der Abschnitt „## %s“. Er sagt, wie Mira die Nachrichten dieses Skills ansagt; bis er da ist, sagt sie sie nicht an.",
		"takestwice":          "Dieser Skill und der Skill %[2]s nehmen beide die Nachrichten der Art %[1]s auf. Schalte einen der beiden aus; bis dahin nimmt keiner sie auf.",
		"notlocal":            "Der Skill muss lokal laufen (privacy: local), aber sein Agent %s läuft auf dem Modell „%s“ (%s). Gib dem Agenten in der mira.yaml unter agents ein lokales Modell.",
		"agentname":           "Der Agent „%s“ hat einen Namen (name); den gibt es nur für einen Agenten, der anderswo läuft (remote).",
		"remotefields":        "Der Agent „%s“ läuft anderswo (remote); %s geht dann nicht: Modell, Werkzeuge und Anleitung hat er selbst. Lass das Feld weg.",
		"remoteprivacy":       "Der Skill muss lokal laufen (privacy: local), aber sein Agent „%s“ läuft anderswo (remote); Mira kann nicht wissen, wo er die Daten verarbeitet.",
		"remoteurl":           "Der Agent „%s“ unter remote braucht eine Adresse (url) mit http:// oder https://, meist aus einer Einstellung wie \"{{ url }}\"; da steht „%s“.",
		"remotedown":          "%s ist nicht erreichbar (%s). Der Skill ist da, aber ohne diesen Agenten; Mira versucht es weiter.",
		"remoterefused":       "%s gibt seine Beschreibung nicht heraus (%s). Prüfe die Adresse in den Einstellungen des Skills; Mira versucht es erst nach einem Neustart wieder.",
		"remotenoway":         "%s kann Ergebnisse weder schicken (Push) noch streamen; so bekommt Mira kein Ergebnis. Der Skill ist da, aber ohne diesen Agenten.",
		"remotenoaddress":     "%s meldet Ergebnisse nur von sich aus, und dafür fehlt Miras Adresse für entfernte Agenten. Trag sie auf Miras Seite unter „Verbindungen“ ein (zum Beispiel http://192.168.1.10:8787) und starte Mira neu. Der Skill ist da, aber ohne diesen Agenten.",
		"remotescheme":        "%s verlangt eine Anmeldung, die Mira nicht kann (%s). Mira kann einen Token (Bearer), einen API-Schlüssel und OAuth2. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteoauth":         "%s verlangt eine Anmeldung mit OAuth2 (%s), aber der Skill hat keinen Abschnitt oauth für die Verbindung. Der Skill ist da, aber ohne diesen Agenten.",
		"remotetoken":         "%s verlangt einen Token (%s), aber die Einstellung des Skills dafür ist leer. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteskill":         "%s hat keinen Skill „%s“ (a2a_skill). Seine Skills: %s. Der Skill ist da, aber ohne diesen Agenten.",
		"remotetransport":     "%s nennt keine Schnittstelle, die Mira spricht (%s); Mira spricht A2A 1.0 über JSON-RPC und HTTP+JSON. Der Skill ist da, aber ohne diesen Agenten.",
		"remoteelsewhere":     "%s nimmt Anfragen unter %s an, nicht unter seiner Adresse %s; Mira schickt den Token nur an die Adresse des Skills. Trag in den Einstellungen des Skills die Adresse ein, unter der er Anfragen annimmt.",
		"automationid":        "Automation %d unter automations braucht eine id aus kleinen Wörtern mit - oder _; da steht „%s“.",
		"automationtwice":     "Zwei Automationen unter automations heißen „%s“; jede braucht eine eigene id.",
		"automationblueprint": "Die Automation „%s“ nennt den Blueprint „%s“, den der Skill nicht hat; er gehört als Datei mit der Endung .yaml in den Ordner %s des Skills.",
		"sourcedown":          "Die Skills aus dem Abo konnten nicht geladen werden (%[1]s: %[2]s); Mira versucht es beim nächsten Start wieder.",
		"sourcekey":           "Dein Abo ist abgelaufen oder der Schlüssel stimmt nicht (%[1]s antwortet %[2]s). Trag auf Miras Seite unter „Verbindungen“ den gültigen Schlüssel ein.",
		"sourcenot":           "Unter %[1]s ist keine Skill-Quelle (%[2]s). Prüfe die Adresse auf Miras Seite unter „Verbindungen“.",
		"sourceurl":           "Die Adresse der Skill-Quelle „%s“ muss mit https:// beginnen (http:// nur für diesen Rechner selbst).",
		"sourceskill":         "Die Skill-Quelle %[1]s liefert den Skill „%[2]s“ so, dass Mira ihn nicht nimmt: %[3]s.",
		"sourcetwice":         "Den Skill „%[1]s“ gibt es im Ordner %[2]s und im Abo (%[3]s). Mira nimmt den aus dem Ordner; benenne einen der beiden um, um den aus dem Abo zu nutzen.",
		"blueprintunusable":   "Der Blueprint %s des Skills lässt sich nicht verwenden (%s). Der Skill läuft, aber ohne ihn und ohne die Automationen daraus.",
		"blueprintlist":       "Home Assistant konnte nicht nach seinen Blueprints gefragt werden (%s). Die Blueprints und Automationen des Skills kommen beim nächsten Start.",
		"blueprintsave":       "Der Blueprint %s konnte nicht in Home Assistant gespeichert werden (%s). Der Skill läuft; Mira versucht es beim nächsten Start wieder.",
		"blueprintrefused":    "Die neue Version des Blueprints %s passt nicht zu den Automationen, die ihn nutzen: %s. Home Assistant behält die alte Version, und ihre Automationen laufen weiter. Wer den Skill schreibt: Eine geänderte Schnittstelle braucht eine neue Blueprint-Datei.",
		"automationinput":     "Die Automation „%s“ füllt die Eingabe „%s“, die der Blueprint %s nicht hat. Sie wird nicht angelegt.",
		"automationneeds":     "Die Automation „%s“ braucht die Eingabe „%s“ des Blueprints %s, hat aber keinen Wert dafür; trag die Einstellung des Skills ein. Bis dahin wird sie nicht angelegt.",
		"automationread":      "Die Automation „%s“ ließ sich in Home Assistant nicht lesen (%s); Mira versucht es beim nächsten Start wieder.",
		"automationsave":      "Die Automation „%s“ konnte nicht in Home Assistant gespeichert werden (%s); Mira versucht es beim nächsten Start wieder.",
		"scriptid":            "Skript %d unter scripts braucht eine id aus kleinen Wörtern mit - oder _; da steht „%s“.",
		"scripttwice":         "Zwei Skripte unter scripts heißen „%s“; jedes braucht eine eigene id.",
		"scriptblueprint":     "Das Skript „%s“ nennt den Blueprint „%s“, den der Skill nicht hat; er gehört als Datei mit der Endung .yaml in den Ordner %s des Skills.",
		"blueprintkind":       "„%s“ unter %s nennt den Blueprint %s, der für %s ist. Eine Automation braucht einen Automations-Blueprint, ein Skript einen Skript-Blueprint. Es wird nicht angelegt.",
		"scriptinput":         "Das Skript „%s“ füllt die Eingabe „%s“, die der Blueprint %s nicht hat. Es wird nicht angelegt.",
		"scriptneeds":         "Das Skript „%s“ braucht die Eingabe „%s“ des Blueprints %s, hat aber keinen Wert dafür; trag die Einstellung des Skills ein. Bis dahin wird es nicht angelegt.",
		"scriptread":          "Das Skript „%s“ ließ sich in Home Assistant nicht lesen (%s); Mira versucht es beim nächsten Start wieder.",
		"scriptsave":          "Das Skript „%s“ konnte nicht in Home Assistant gespeichert oder geladen werden (%s); Mira versucht es beim nächsten Start wieder.",
		"scriptdesc":          "Das Skript „%s“ unter scripts braucht eine description: ein, zwei Sätze, was es tut und welche Felder es hat. Sie ist die Beschreibung seines Werkzeugs.",
		"scripttool":          "Das Skript „%s“ ist in Home Assistant nicht als Aktion %s da (nicht angelegt, umbenannt oder nicht geladen); der Skill hat dieses Werkzeug deshalb nicht.",
		"scripttools":         "Home Assistant konnte nicht nach seinen Aktionen gefragt werden (%s); die Skripte des Skills sind bis zum nächsten Start keine Werkzeuge.",
	},
	i18n.EN: {
		"nofolder":            "There is no folder \"%[1]s\" with a SKILL.md file in %[2]s. Create it there, or take \"%[1]s\" out from under skills in mira.yaml (or write enabled: false there).",
		"unreadable":          "The SKILL.md file cannot be read (%s).",
		"nofront":             "SKILL.md must begin with a line of ---; the header with name and description follows.",
		"frontopen":           "The header in SKILL.md is not closed: a line with --- is missing after it.",
		"yaml":                "The header of SKILL.md is not valid YAML (%s). Usually an indent or a colon is wrong.",
		"name":                "The name in the header (\"%s\") must match the folder's name and be lower-case words with hyphens.",
		"nodesc":              "The header has no description.",
		"nosettings":          "The setting \"%s\" in mira.yaml does not exist: this skill has no settings.",
		"unknownsetting":      "The setting \"%s\" in mira.yaml does not exist for this skill. Possible: %s.",
		"required":            "The setting \"%[1]s\" is missing in mira.yaml under skills.%[3]s. It is needed: %[2]s",
		"cron":                "Schedule %d: the time (cron) is invalid (%s).",
		"notask":              "Schedule %d has no task.",
		"toperperson":         "Schedule %d has to and per_person; a schedule goes either to one person (to) or to each (per_person).",
		"schedulessetting":    "A setting must not be called schedules: under that name mira.yaml changes the schedules.",
		"scheduledup":         "Two schedules are named \"%s\"; each needs a name of its own.",
		"scheduleentry":       "The entry schedules in mira.yaml cannot be read: %v.",
		"schedulecron":        "The time for the schedule \"%s\" in mira.yaml is invalid (%s).",
		"scheduleunknown":     "This skill has no schedule \"%s\"; take it out from under schedules in mira.yaml.",
		"targetneeds":         "The conversation (target) needs at least one field (fields) and a finish.",
		"targetplan":          "A run with a plan (target with plan: true) has no fields and no finish.",
		"targetaction":        "The action \"%s\" needs a domain before the dot, such as esphome.show_alarm_set.",
		"targetfield":         "Field %d of the conversation (target) needs a name in lower-case words and a description.",
		"targettimeout":       "The timeout \"%s\" is not a duration like 15m.",
		"targetbound":         "bound is \"%s\"; possible are person and room (none: person).",
		"unknownfinish":       "Mira does not know the finish \"%s\". Known: %s. Or call a Home Assistant action instead (finish: action).",
		"mailextract":         "The mail section needs instructions (extract): how Mira reads the mails.",
		"inboxextract":        "The inbox section needs instructions (extract): how Mira reads the documents.",
		"agent":               "The agent \"%s\" under agents cannot be read like this (%s).",
		"unknownagent":        "The conversation (target) names the agent \"%s\", which does not exist. Describe it under agents, in mira.yaml or in the skill.",
		"privacyword":         "privacy is \"%s\"; only local goes here.",
		"language":            "language is \"%s\"; possible are de and en (none: de).",
		"translation":         "The translation %s cannot be read like this (%v).",
		"randomnone":          "%s: there is no axis under random; write axes with their values under it, or take random out.",
		"randomaxis":          "%s: the axis \"%s\" under random needs a list of values, or values with a list under each (no deeper).",
		"randomempty":         "%s: the axis \"%s\" under random has no value, or one of its values has no list under it.",
		"mcpurl":              "The mcp section needs an address (url), usually from a setting like \"{{ url }}\".",
		"mcpcommand":          "The mcp section with transport stdio needs a program (command).",
		"mcptransport":        "transport \"%s\" in the mcp section does not work; possible are http and stdio.",
		"mcpconfirm":          "These tools are under confirm but not under tools: %s. Add them to tools or take them out of confirm.",
		"programfields":       "The section program needs name, version, url and sha256.",
		"mcpsetup":            "The tool %s is under setup and also under tools or confirm. Setup tools are for the page only, never for a model: take it out of tools and confirm.",
		"mcpsetupentry":       "Every entry under setup needs tool and label.",
		"programname":         "name and version in the section program may hold only letters, digits, dot, hyphen and underscore: %q, %q.",
		"programarch":         "%s is not available for this architecture (%s): sha256 lacks it.",
		"programurl":          "The program's address must start with https: %s",
		"programsumformat":    "sha256 for %s is no SHA-256 sum (64 hex characters).",
		"programdownload":     "The program could not be downloaded from %s: %v. Mira tries again in 10 minutes.",
		"programsum":          "The program from %s is not the expected one (SHA-256 expected %s, got %s). It is neither kept nor started.",
		"skilldata":           "The skill's folder could not be created: %v",
		"reservedsetting":     "The setting %q has the name of a placeholder of Mira's (data, arch). Name it differently.",
		"mcpdown":             "The skill's MCP server cannot be reached (%s). The skill is there, but without its tools; Mira keeps trying.",
		"mcptools":            "The skill names these tools under tools, but the server does not have them: %s. The others work.",
		"oauthurl":            "The oauth section needs authorize_url and token_url, both as https addresses.",
		"oauthauth":           "token_auth \"%s\" in the oauth section does not work; possible are post and basic.",
		"oauthapi":            "The address \"%s\" under api in the oauth section must be an https address.",
		"oauthmethod":         "The method \"%s\" for %s under api does not work; possible are GET, POST, PUT, PATCH and DELETE.",
		"oauthmcp":            "The mcp section takes token: oauth, but the skill has no oauth section.",
		"oauthnone":           "The skill is not connected with %s yet. Connect it in Mira on the Connections page.",
		"oauthexpired":        "The connection with %s has expired or was revoked. Connect the skill again in Mira on the Connections page.",
		"noannounce":          "SKILL.md has no section \"## %s\". It says how Mira announces this skill's messages; until it is there, she does not announce them.",
		"takestwice":          "This skill and the skill %[2]s both take the messages of the kind %[1]s. Switch one of them off; until then neither takes them.",
		"notlocal":            "The skill must run locally (privacy: local), but its agent %s runs on the model \"%s\" (%s). Give the agent a local model in mira.yaml under agents.",
		"agentname":           "The agent \"%s\" has a name; that is only for an agent that runs elsewhere (remote).",
		"remotefields":        "The agent \"%s\" runs elsewhere (remote); %s does not go with it: it has its own model, tools and instructions. Leave the field out.",
		"remoteprivacy":       "The skill must run locally (privacy: local), but its agent \"%s\" runs elsewhere (remote); Mira cannot know where it handles the data.",
		"remoteurl":           "The agent \"%s\" under remote needs an address (url) with http:// or https://, usually from a setting like \"{{ url }}\"; it says \"%s\".",
		"remotedown":          "%s cannot be reached (%s). The skill is there, but without this agent; Mira keeps trying.",
		"remoterefused":       "%s does not give out its description (%s). Check the address in the skill's settings; Mira tries again only after a restart.",
		"remotenoway":         "%s can neither send results (push) nor stream them, so Mira gets no result. The skill is there, but without this agent.",
		"remotenoaddress":     "%s reports results only on its own, and for that Mira's address for remote agents is missing. Enter it on Mira's page under \"Connections\" (for example http://192.168.1.10:8787) and restart Mira. The skill is there, but without this agent.",
		"remotescheme":        "%s asks for a login Mira cannot do (%s). Mira can do a token (bearer), an API key and OAuth2. The skill is there, but without this agent.",
		"remoteoauth":         "%s asks for a login with OAuth2 (%s), but the skill has no oauth section for the connection. The skill is there, but without this agent.",
		"remotetoken":         "%s asks for a token (%s), but the skill's setting for it is empty. The skill is there, but without this agent.",
		"remoteskill":         "%s has no skill \"%s\" (a2a_skill). Its skills: %s. The skill is there, but without this agent.",
		"remotetransport":     "%s names no interface Mira speaks (%s); Mira speaks A2A 1.0 over JSON-RPC and HTTP+JSON. The skill is there, but without this agent.",
		"remoteelsewhere":     "%s takes requests at %s, not under its address %s; Mira sends the token only to the skill's address. Enter the address where it takes requests in the skill's settings.",
		"automationid":        "Automation %d under automations needs an id of small words with - or _; it says \"%s\".",
		"automationtwice":     "Two automations under automations are called \"%s\"; each needs an id of its own.",
		"automationblueprint": "The automation \"%s\" names the blueprint \"%s\", which the skill does not have; it belongs as a file ending in .yaml in the skill's folder %s.",
		"sourcedown":          "The skills of the subscription could not be loaded (%[1]s: %[2]s); Mira tries again at the next start.",
		"sourcekey":           "Your subscription has ended or the key is wrong (%[1]s answers %[2]s). Enter the valid key on Mira's page under \"Connections\".",
		"sourcenot":           "There is no skill source at %[1]s (%[2]s). Check the address on Mira's page under \"Connections\".",
		"sourceurl":           "The skill source's address \"%s\" must begin with https:// (http:// only for this machine itself).",
		"sourceskill":         "The skill source %[1]s delivers the skill \"%[2]s\" in a way Mira does not take: %[3]s.",
		"sourcetwice":         "The skill \"%[1]s\" is in the folder %[2]s and in the subscription (%[3]s). Mira takes the folder's; rename one of them to use the subscription's.",
		"blueprintunusable":   "The skill's blueprint %s cannot be used (%s). The skill runs, but without it and the automations made from it.",
		"blueprintlist":       "Home Assistant could not be asked for its blueprints (%s). The skill's blueprints and automations come at the next start.",
		"blueprintsave":       "The blueprint %s could not be saved in Home Assistant (%s). The skill runs; Mira tries again at the next start.",
		"blueprintrefused":    "The new version of the blueprint %s does not fit the automations using it: %s. Home Assistant keeps the old version, and its automations keep running. For the skill's author: a changed interface needs a new blueprint file.",
		"automationinput":     "The automation \"%s\" fills the input \"%s\", which the blueprint %s does not have. It is not made.",
		"automationneeds":     "The automation \"%s\" needs the input \"%s\" of the blueprint %s but has no value for it; enter the skill's setting. Until then it is not made.",
		"automationread":      "The automation \"%s\" could not be read in Home Assistant (%s); Mira tries again at the next start.",
		"automationsave":      "The automation \"%s\" could not be saved in Home Assistant (%s); Mira tries again at the next start.",
		"scriptid":            "Script %d under scripts needs an id of small words with - or _; it says \"%s\".",
		"scripttwice":         "Two scripts under scripts are called \"%s\"; each needs an id of its own.",
		"scriptblueprint":     "The script \"%s\" names the blueprint \"%s\", which the skill does not have; it belongs as a file ending in .yaml in the skill's folder %s.",
		"blueprintkind":       "\"%s\" under %s names the blueprint %s, which is for %s. An automation needs an automation blueprint, a script a script blueprint. It is not made.",
		"scriptinput":         "The script \"%s\" fills the input \"%s\", which the blueprint %s does not have. It is not made.",
		"scriptneeds":         "The script \"%s\" needs the input \"%s\" of the blueprint %s but has no value for it; enter the skill's setting. Until then it is not made.",
		"scriptread":          "The script \"%s\" could not be read in Home Assistant (%s); Mira tries again at the next start.",
		"scriptsave":          "The script \"%s\" could not be saved or loaded in Home Assistant (%s); Mira tries again at the next start.",
		"scriptdesc":          "The script \"%s\" under scripts needs a description: a sentence or two on what it does and its fields. It is its tool's description.",
		"scripttool":          "The script \"%s\" is not in Home Assistant as the action %s (not made, renamed or not loaded); so the skill does not have this tool.",
		"scripttools":         "Home Assistant could not be asked for its actions (%s); the skill's scripts are no tools until the next start.",
	},
}
