package tasks

import (
	"fmt"
	"strings"

	"github.com/sker65/mira/internal/i18n"
)

type texts struct {
	// Views.
	viewFailed, viewUnknown, viewState, viewNoData string
	viewMean, viewChange, viewHour, viewNoBaseline string
	viewBaseline, viewToday, viewWeekdays          string
	// Judgements.
	judgeSystem, assessRules, decideRules         string
	secGoal, secQuestion, secObservation, secData string
	secSituation, secMemory, secEarlier           string
	yes, no, nobody                               string
	earlierDecide, earlierQuiet, earlierSaid      string
	// Drafting.
	draftSystem, draftIntro, draftRules, redraft string
	draftForbidden, alsoSwitches                 string
	planning, proposal, planFailed, saveFailed   string
	confirmed, tryObservation, cannot            string
	// yesPhrases lists plain agreement, "|"-separated, lower case.
	yesPhrases string
	// Tools.
	createDesc, createGoal, confirmDesc, listDesc        string
	deleteDesc, enableDesc, enableOn, tryDesc, idDesc    string
	noProposal, none, notFound, deleted, enabledOn       string
	enabledOff, triedNothing, triedVerdict, triedDecided string
}

// maxCatalog bounds the entity list in the drafting prompt.
const maxCatalog = 250

func (m *Manager) draftInstruction(goal string, cat []Entity) string {
	t := m.texts()
	var b strings.Builder
	fmt.Fprintf(&b, t.draftIntro, goal)
	for i, e := range cat {
		if i == maxCatalog {
			break
		}
		fmt.Fprintf(&b, "\n- %s | %s | %s | %s | %s | %s | %s", e.ID, e.Name, e.Area, e.DeviceClass, e.StateClass, e.Unit, e.State)
	}
	forbidden := strings.Join(append(append([]string{}, m.opts.ConfirmDomains...), m.opts.ConfirmEntities...), ", ")
	if forbidden == "" {
		forbidden = t.draftForbidden
	}
	rules := strings.Replace(t.draftRules, "<forbidden>", forbidden, 1)
	b.WriteString(rules)
	return b.String()
}

// The drafting rules are plain strings, not format strings: they contain
// Home Assistant templates with % signs.
const draftRulesDE = `

Schreib eine Home-Assistant-Automation, die diese Aufgabe erfüllt. Home Assistant entscheidet mit triggers und conditions, WANN etwas geschieht; Mira entscheidet mit ihren Aktionen, WAS gesagt wird.

Miras Aktionen:
- mira.assess: Mira beurteilt mit dem Modell, gemessen am Ziel, ob etwas meldenswert ist, und meldet es dann. data:
  goal: das Ziel in den Worten des Nutzers
  observation: optional, was passiert ist, gern mit Templates, z. B. "{{ trigger.to_state.name }} ist seit {{ now().strftime('%H:%M') }} {{ trigger.to_state.state }}"
  data: optional, Liste von {entity_id, view}; Mira schlägt nach und verdichtet:
    view state: aktueller Zustand
    view same_hour: letzte volle Stunde gegen dieselbe Stunde der letzten 14 Tage (Verbrauch bei Zählern mit state_class total/total_increasing, Mittel bei Messwerten)
    view day: heute bis jetzt gegen dieselben Wochentage der Vorwochen
  cooldown: optional, z. B. "6h": innerhalb dieser Zeit meldet die Aufgabe nur, wenn es wichtiger wird
- mira.add_notice: feste Meldung ohne Urteil, wenn klar ist, dass es immer gesagt werden soll (z. B. Wassermelder). data: text, importance (1 nebenbei, 2 wichtig, 3 dringend)
- mira.decide: Ja/Nein-Urteil mit response_variable, danach condition: template auf {{ d.answer }}, wenn erst Mira entscheiden soll, ob geschaltet wird.

Andere Aktionen (Licht, Radio, Push-Nachricht mit notify.*) nur, wenn der Nutzer ausdrücklich darum bittet, und nur mit ausdrücklich genannten Entitäten. Niemals: Skripte, Shell-Befehle, Neustarts und was Mira nur mit ausdrücklichem Ja im Gespräch schaltet: <forbidden>.
Faustregeln:
- Steht das Urteil fest (Melder löst aus, Tür geht auf), nimm mira.add_notice. Braucht es Abwägung (ungewöhnlich? relevant?), nimm mira.assess.
- "Ungewöhnlich viel/wenig" eines Zählers oder Messwerts: einmal pro Stunde, also trigger time_pattern mit minutes: 15 (die Zahl 15, nicht "/15": das wäre alle 15 Minuten und vier Bewertungen pro Stunde), und mira.assess mit view same_hour, cooldown "6h".
- Zeitfenster wie "nachts" sind conditions (condition: time, after/before), keine eigene Logik.
- Nimm nur Entitäten aus der Liste, und nur so viele, wie die Aufgabe braucht.
- Home-Assistant-Syntax: triggers mit "trigger:", conditions mit "condition:", actions mit "action:".

Gibt es in der Liste keine Entität, mit der sich die Aufgabe erfüllen lässt, erfinde nichts und baue keine Ersatz-Automation: antworte dann nur {"error": ein Satz an den Nutzer, was fehlt}.

Sonst antworte nur mit einem JSON-Objekt:
{"title": ein bis drei Wörter,
 "summary": ein bis zwei Sätze an den Nutzer in der Du-Form, was du beobachtest und wann du dich meldest,
 "automation": {"alias": ..., "description": das Ziel, "triggers": [...], "conditions": [...], "actions": [...], "mode": "single"}}`

const draftRulesEN = `

Write a Home Assistant automation that fulfils this task. Home Assistant decides WHEN with triggers and conditions; Mira decides WHAT to say with her actions.

Mira's actions:
- mira.assess: Mira judges with the model, against the goal, whether something is worth a notice, and then reports it. data:
  goal: the goal in the user's words
  observation: optional, what happened, with templates if useful, e.g. "{{ trigger.to_state.name }} has been {{ trigger.to_state.state }} since {{ now().strftime('%H:%M') }}"
  data: optional, a list of {entity_id, view}; Mira looks it up and condenses it:
    view state: the current state
    view same_hour: the last full hour against the same hour on the previous 14 days (usage for counters with state_class total/total_increasing, mean for measurements)
    view day: today so far against the same weekdays of earlier weeks
  cooldown: optional, e.g. "6h": within it the task only reports again when it gets more important
- mira.add_notice: a fixed notice without judgement, when it should always be said (e.g. a leak sensor). data: text, importance (1 in passing, 2 important, 3 urgent)
- mira.decide: a yes/no judgement with response_variable, then condition: template on {{ d.answer }}, when Mira should first decide whether to switch.

Other actions (lights, radio, a push with notify.*) only when the user asks for them explicitly, and only with entities named explicitly. Never: scripts, shell commands, restarts and what Mira only switches after an explicit yes in conversation: <forbidden>.
Rules of thumb:
- If the verdict is fixed (a sensor fires, a door opens), use mira.add_notice. If it needs weighing (unusual? relevant?), use mira.assess.
- "Unusually much/little" of a counter or measurement: once an hour, i.e. trigger time_pattern with minutes: 15 (the number 15, not "/15": that would be every 15 minutes and four judgements an hour), and mira.assess with view same_hour, cooldown "6h".
- Time windows like "at night" are conditions (condition: time, after/before), not logic of your own.
- Use only entities from the list, and only as many as the task needs.
- Home Assistant syntax: triggers with "trigger:", conditions with "condition:", actions with "action:".

If no entity in the list can fulfil the task, invent nothing and build no substitute automation: then answer only {"error": one sentence to the user saying what is missing}.

Otherwise answer only with a JSON object:
{"title": one to three words,
 "summary": one or two sentences to the user about what you watch and when you speak up,
 "automation": {"alias": ..., "description": the goal, "triggers": [...], "conditions": [...], "actions": [...], "mode": "single"}}`

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		viewFailed:     "nicht verfügbar (%v)",
		viewUnknown:    "unbekannte Entität",
		viewState:      "%s, seit %s",
		viewNoData:     "keine Statistik für diesen Zeitraum",
		viewMean:       "Mittel",
		viewChange:     "Verbrauch",
		viewHour:       "%s–%s Uhr %s %s %s.",
		viewNoBaseline: "Keine Vergleichswerte.",
		viewBaseline:   "Dieselbe Stunde an %d früheren Tagen: Median %s, Bereich %s–%s %s.",
		viewToday:      "Heute bis %s Uhr %s %s %s.",
		viewWeekdays:   "Dieselben Wochentage bis zu dieser Uhrzeit (%d): Median %s, Bereich %s–%s %s.",

		judgeSystem: "Du bist Mira, die Assistentin eines Haushalts. Du beurteilst Beobachtungen aus dem Smart Home für eine Aufgabe des Nutzers. Behaupte nur, was in Beobachtung, Daten und Kontext steht. Antworte nur mit JSON.",
		assessRules: `
## Antwort
JSON-Objekt:
- importance: 0 = nicht melden (normal, erklärbar oder schon gesagt), 1 = nebenbei, 2 = wichtig, sollte heute jemand ansehen, 3 = dringend, Schaden droht
- spoken: ein bis zwei kurze Sätze zum Vorlesen, Du-Form, Werte als Ziffern, mit Uhrzeit und in der Vergangenheit (die Meldung wird vielleicht später vorgelesen); leer bei 0
- details: die Langfassung mit den Vergleichswerten
Was du kürzlich schon gesagt hast, meldest du nur wieder, wenn es schlimmer geworden ist.`,
		decideRules: `
## Antwort
JSON-Objekt:
- answer: true oder false, die Antwort auf die Frage
- reason: ein Satz Begründung mit den entscheidenden Werten`,
		secGoal:        "## Aufgabe\n%s\n\n",
		secQuestion:    "## Frage\n%s\n\n",
		secObservation: "## Beobachtung\n%s\n\n",
		secData:        "## Daten\n%s\n\n",
		secSituation:   "## Lage\nJetzt: %s, %s Uhr. Ruhezeit: %s. Zu Hause: %s. Unterwegs: %s.\n\n",
		secMemory:      "## Was Mira über den Haushalt weiß\n%s\n\n",
		secEarlier:     "## Frühere Urteile dieser Aufgabe (neueste zuerst)\n%s\n\n",
		yes:            "ja",
		no:             "nein",
		nobody:         "niemand",
		earlierDecide:  "- %s: Antwort %s – %s\n",
		earlierQuiet:   "- %s: nichts gemeldet\n",
		earlierSaid:    "- %s: Wichtigkeit %d – %s\n",

		draftSystem:    "Du schreibst Home-Assistant-Automationen für Mira, die Assistentin eines Haushalts. Antworte nur mit JSON.",
		draftIntro:     "Aufgabe des Nutzers: %q\n\nEntitäten (id | Name | Bereich | device_class | state_class | Einheit | Zustand jetzt):",
		draftRules:     draftRulesDE,
		redraft:        "\n\nDein letzter Entwurf wurde abgelehnt: %s. Mach einen neuen, der das behebt.",
		draftForbidden: "nichts weiter",
		alsoSwitches:   "Dabei schaltet sie außerdem: %s.",
		planning:       "Ich plane die Aufgabe jetzt im Hintergrund; der Vorschlag kommt von selbst. Sag dem Nutzer in einem kurzen Satz, dass du sie planst und dich mit einem Vorschlag meldest, ohne Zeitangabe.",
		proposal:       "Mein Vorschlag für die Aufgabe %s: %s Soll ich so vorgehen?",
		planFailed:     "Für die Aufgabe „%s“ habe ich keine brauchbare Automation gefunden.",
		cannot:         "Die Aufgabe „%s“ kann ich so nicht übernehmen: %s",
		saveFailed:     "Ich konnte die Aufgabe nicht in Home Assistant anlegen: %v",
		confirmed:      "Gut, die Aufgabe %s läuft.",
		tryObservation: "Probelauf: kein Auslöser, beurteile die Daten, wie sie jetzt sind.",
		yesPhrases:     "ja|ja bitte|ja gerne|gerne|passt|das passt|ja passt|ja das passt|mach das|ja mach das|mach|okay|ok|ja okay|genau|jawohl|einverstanden|leg los|ja leg los|starte sie|ja starte sie|klingt gut",

		createDesc:   "Legt eine dauerhafte Aufgabe an, wenn der Nutzer möchte, dass du auf etwas achtest, z. B. \"Achte auf den Wasserverbrauch\", \"Sag Bescheid, wenn nachts ungewöhnlich viel Strom läuft\". Die Aufgabe wird eine Home-Assistant-Automation. Das Planen läuft im Hintergrund, der Vorschlag kommt von selbst.",
		createGoal:   "Die Aufgabe in den Worten des Nutzers.",
		confirmDesc:  "Legt den zuletzt hier vorgeschlagenen Auftrag in Home Assistant an. Hast du gerade eine Aufgabe vorgeschlagen (auch im zusätzlichen Kontext: \"Soll ich so vorgehen?\") und der Nutzer stimmt zu, rufe task_confirm auf, statt den Plan zu wiederholen. Danach nur kurz bestätigen.",
		listDesc:     "Listet die Aufgaben: offene Vorschläge und Miras Automationen in Home Assistant, mit an/aus.",
		deleteDesc:   "Löscht eine Aufgabe oder einen Vorschlag anhand der id aus task_list.",
		enableDesc:   "Schaltet eine Aufgabe an oder aus (pausieren, fortsetzen), anhand der id aus task_list.",
		enableOn:     "true schaltet an, false aus.",
		tryDesc:      "Probelauf einer Aufgabe: Mira beurteilt die Daten jetzt, ohne zu melden, und sagt, was sie sagen würde. id aus task_list.",
		idDesc:       "Die id aus task_list.",
		noProposal:   "hier wartet kein Vorschlag auf Bestätigung; frag den Nutzer, welche Aufgabe er meint, oder lege sie neu an",
		none:         "Keine Aufgaben.",
		notFound:     "keine Aufgabe mit dieser id",
		deleted:      "Aufgabe gelöscht.",
		enabledOn:    "Aufgabe läuft wieder.",
		enabledOff:   "Aufgabe pausiert.",
		triedNothing: "Die Aufgabe enthält kein mira.assess oder mira.decide; sie meldet fest, ohne Urteil.",
		triedVerdict: "Probelauf: Wichtigkeit %d. %s",
		triedDecided: "Probelauf: Antwort %v. %s",
	},
	i18n.EN: {
		viewFailed:     "not available (%v)",
		viewUnknown:    "unknown entity",
		viewState:      "%s, since %s",
		viewNoData:     "no statistics for this period",
		viewMean:       "mean",
		viewChange:     "usage",
		viewHour:       "%s–%s %s %s %s.",
		viewNoBaseline: "No values to compare.",
		viewBaseline:   "The same hour on %d earlier days: median %s, range %s–%s %s.",
		viewToday:      "Today until %s %s %s %s.",
		viewWeekdays:   "The same weekdays up to this time (%d): median %s, range %s–%s %s.",

		judgeSystem: "You are Mira, the assistant of a household. You judge observations from the smart home for a task of the user. Only claim what observation, data and context say. Answer only with JSON.",
		assessRules: `
## Answer
JSON object:
- importance: 0 = do not report (normal, explainable or already said), 1 = in passing, 2 = important, someone should look today, 3 = urgent, damage is imminent
- spoken: one or two short sentences to be read aloud, values as digits, with the time and in the past tense (the notice may be read out later); empty for 0
- details: the long form with the comparison values
What you said recently you only report again if it got worse.`,
		decideRules: `
## Answer
JSON object:
- answer: true or false, the answer to the question
- reason: one sentence with the deciding values`,
		secGoal:        "## Task\n%s\n\n",
		secQuestion:    "## Question\n%s\n\n",
		secObservation: "## Observation\n%s\n\n",
		secData:        "## Data\n%s\n\n",
		secSituation:   "## Situation\nNow: %s, %s. Quiet hours: %s. At home: %s. Away: %s.\n\n",
		secMemory:      "## What Mira knows about the household\n%s\n\n",
		secEarlier:     "## Earlier judgements of this task (newest first)\n%s\n\n",
		yes:            "yes",
		no:             "no",
		nobody:         "nobody",
		earlierDecide:  "- %s: answer %s – %s\n",
		earlierQuiet:   "- %s: nothing reported\n",
		earlierSaid:    "- %s: importance %d – %s\n",

		draftSystem:    "You write Home Assistant automations for Mira, the assistant of a household. Answer only with JSON.",
		draftIntro:     "The user's task: %q\n\nEntities (id | name | area | device_class | state_class | unit | state now):",
		draftRules:     draftRulesEN,
		redraft:        "\n\nYour last draft was refused: %s. Make a new one that fixes it.",
		draftForbidden: "nothing else",
		alsoSwitches:   "It also switches: %s.",
		planning:       "I am planning the task in the background now; the proposal arrives by itself. Tell the user in one short sentence that you are planning it and will come back with a proposal, without a time.",
		proposal:       "My proposal for the task %s: %s Shall I go ahead?",
		planFailed:     "I found no usable automation for the task \"%s\".",
		cannot:         "I cannot take on the task \"%s\" like this: %s",
		saveFailed:     "I could not create the task in Home Assistant: %v",
		confirmed:      "Good, the task %s is running.",
		tryObservation: "Dry run: no trigger, judge the data as they are now.",
		yesPhrases:     "yes|yes please|yeah|yep|sure|ok|okay|fine|sounds good|go ahead|do it|yes do it|start it",

		createDesc:   "Creates a standing task when the user wants you to watch something, e.g. \"Keep an eye on the water usage\", \"Tell me if a lot of power is used at night\". The task becomes a Home Assistant automation. Planning runs in the background; the proposal arrives by itself.",
		createGoal:   "The task in the user's words.",
		confirmDesc:  "Creates the task most recently proposed here in Home Assistant. If you just proposed a task (also in the additional context: \"Shall I go ahead?\") and the user agrees, call task_confirm instead of repeating the plan. Afterwards confirm only briefly.",
		listDesc:     "Lists the tasks: open proposals and Mira's automations in Home Assistant, with on/off.",
		deleteDesc:   "Deletes a task or a proposal by its id from task_list.",
		enableDesc:   "Switches a task on or off (pause, resume), by its id from task_list.",
		enableOn:     "true switches on, false off.",
		tryDesc:      "Dry run of a task: Mira judges the data now, without reporting, and says what she would say. id from task_list.",
		idDesc:       "The id from task_list.",
		noProposal:   "no proposal waits for confirmation here; ask the user which task they mean, or create it anew",
		none:         "No tasks.",
		notFound:     "no task with this id",
		deleted:      "Task deleted.",
		enabledOn:    "Task running again.",
		enabledOff:   "Task paused.",
		triedNothing: "The task has no mira.assess or mira.decide; it reports fixed, without judgement.",
		triedVerdict: "Dry run: importance %d. %s",
		triedDecided: "Dry run: answer %v. %s",
	},
}
