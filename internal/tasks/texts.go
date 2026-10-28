package tasks

import (
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
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
	draftSystem, draftIntro, draftRules, redraft                           string
	draftFields, draftArea, draftPerson                                    string
	draftForbidden, alsoSwitches                                           string
	conditionsAre, judgesItself, noCondition                               string
	condBetween, condAfter, condBefore, condAbove, condBelow, condTemplate string
	proposal, planFailed                                                   string
	confirmed, tryObservation, cannot                                      string
	noEntityNamed, unknownEntities                                         string
	// Tools.
	listDesc                                             string
	deleteDesc, enableDesc, enableOn, tryDesc, idDesc    string
	none, notFound, deleted, enabledOn                   string
	enabledOff, triedNothing, triedVerdict, triedDecided string
}

// maxCatalog bounds the entity list in the drafting prompt.
const maxCatalog = 250

func (m *Manager) draftInstruction(goal string, cat []Entity, memory string) string {
	t := m.texts()
	var b strings.Builder
	fmt.Fprintf(&b, t.draftIntro, goal)
	for i, e := range cat {
		if i == maxCatalog {
			break
		}
		fmt.Fprintf(&b, "\n- %s | %s | %s | %s | %s | %s | %s", e.ID, e.Name, e.Area, e.DeviceClass, e.StateClass, e.Unit, e.State)
	}
	domains, entities := m.blocked()
	forbidden := strings.Join(append(append([]string{}, domains...), entities...), ", ")
	if forbidden == "" {
		forbidden = t.draftForbidden
	}
	if memory != "" {
		fmt.Fprintf(&b, "\n\n"+t.secMemory, memory)
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
    view upcoming: nur für Kalender (calendar.*): die Termine von jetzt bis Ende morgen
  cooldown: optional, z. B. "6h": innerhalb dieser Zeit meldet die Aufgabe nur, wenn es wichtiger wird
- mira.send_message: feste Nachricht ohne Urteil, wenn klar ist, dass es immer gesagt werden soll (z. B. Wassermelder). data: text, importance (1 nebenbei, 2 wichtig, 3 dringend). Der Text wird oft erst später vorgelesen: nenne die Uhrzeit per Vorlage und sprich in der Vergangenheit, z. B. "Um {{ now().strftime('%H:%M') }} ging das Licht im Schlafzimmer an."
- mira.decide: Ja/Nein-Urteil mit response_variable, danach condition: template auf {{ d.answer }}, wenn erst Mira entscheiden soll, ob geschaltet wird.

Andere Aktionen (Licht, Radio, Push-Nachricht mit notify.*) nur, wenn der Nutzer ausdrücklich darum bittet, und nur mit ausdrücklich genannten Entitäten. Ein Skript nur, wenn es für Assist freigegeben ist (aus dem Katalog). Niemals: Aktionen ohne Entität als Ziel (mqtt.publish, rest_command.*, scene.apply), Shell-Befehle, Neustarts und was Mira nur mit ausdrücklichem Ja im Gespräch schaltet: <forbidden>.
Faustregeln:
- Steht das Urteil fest (Melder löst aus, Tür geht auf), nimm mira.send_message. Braucht es Abwägung (ungewöhnlich? relevant?), nimm mira.assess.
- "Ungewöhnlich viel/wenig" eines Zählers oder Messwerts: einmal pro Stunde, also trigger time_pattern mit minutes: 15 (die Zahl 15, nicht "/15": das wäre alle 15 Minuten und vier Bewertungen pro Stunde), und mira.assess mit view same_hour, cooldown "6h".
- Zeitfenster wie "nachts" sind conditions (condition: time, after/before), keine eigene Logik.
- Nimm nur Entitäten aus der Liste, und nur so viele, wie die Aufgabe braucht.
- Home-Assistant-Syntax: triggers mit "trigger:", conditions mit "condition:", actions mit "action:".

Gibt es in der Liste keine Entität, mit der sich die Aufgabe erfüllen lässt, erfinde nichts und baue keine Ersatz-Automation: antworte dann nur {"error": ein Satz an den Nutzer, was fehlt}.

Ist die Aufgabe unklar, weil ein Maßstab fehlt („ungewöhnlich“, „zu viel“, „spät“, „lange“) und steht er auch nicht in dem, was Mira über den Haushalt weiß, dann rate nicht und baue nichts: antworte nur {"question": eine kurze Rückfrage an den Nutzer mit einem Vorschlag, z. B. "Was ist für dich eine ungewöhnliche Zeit, etwa zwischen Mitternacht und 6 Uhr?"}. Steht der Maßstab dort oder in der Aufgabe, setze ihn als feste Bedingung oder festen Wert um (condition: time, numeric_state above/below …), nicht als Urteil von mira.assess.

Sonst antworte nur mit einem JSON-Objekt:
{"title": ein bis drei Wörter,
 "summary": ein Satz an den Nutzer in der Du-Form: was du beobachtest und dass du Bescheid sagst, z. B. "Wenn die Nachttischlampe angeht, sage ich dir Bescheid." Nenne darin keine Bedingung, Uhrzeit oder Grenzwerte: die sagt Mira selbst dazu.,
 "automation": {"alias": ..., "description": das Ziel, "triggers": [...], "conditions": [...], "actions": [...], "mode": "single"}}`

const draftRulesEN = `

Write a Home Assistant automation that fulfils this task. Home Assistant decides WHEN with triggers and conditions; Mira decides WHAT to say with her actions.

Mira's actions:
- mira.assess: Mira judges with the model, against the goal, whether something is worth a message, and then reports it. data:
  goal: the goal in the user's words
  observation: optional, what happened, with templates if useful, e.g. "{{ trigger.to_state.name }} has been {{ trigger.to_state.state }} since {{ now().strftime('%H:%M') }}"
  data: optional, a list of {entity_id, view}; Mira looks it up and condenses it:
    view state: the current state
    view same_hour: the last full hour against the same hour on the previous 14 days (usage for counters with state_class total/total_increasing, mean for measurements)
    view day: today so far against the same weekdays of earlier weeks
    view upcoming: calendars only (calendar.*): the events from now to the end of tomorrow
  cooldown: optional, e.g. "6h": within it the task only reports again when it gets more important
- mira.send_message: a fixed message without judgement, when it should always be said (e.g. a leak sensor). data: text, importance (1 in passing, 2 important, 3 urgent). The text is often read out later: give the time with a template and use the past tense, e.g. "At {{ now().strftime('%H:%M') }} the bedroom light went on."
- mira.decide: a yes/no judgement with response_variable, then condition: template on {{ d.answer }}, when Mira should first decide whether to switch.

Other actions (lights, radio, a push with notify.*) only when the user asks for them explicitly, and only with entities named explicitly. A script only if it is exposed to Assist (from the catalog). Never: actions without an entity as their target (mqtt.publish, rest_command.*, scene.apply), shell commands, restarts and what Mira only switches after an explicit yes in conversation: <forbidden>.
Rules of thumb:
- If the verdict is fixed (a sensor fires, a door opens), use mira.send_message. If it needs weighing (unusual? relevant?), use mira.assess.
- "Unusually much/little" of a counter or measurement: once an hour, i.e. trigger time_pattern with minutes: 15 (the number 15, not "/15": that would be every 15 minutes and four judgements an hour), and mira.assess with view same_hour, cooldown "6h".
- Time windows like "at night" are conditions (condition: time, after/before), not logic of your own.
- Use only entities from the list, and only as many as the task needs.
- Home Assistant syntax: triggers with "trigger:", conditions with "condition:", actions with "action:".

If no entity in the list can fulfil the task, invent nothing and build no substitute automation: then answer only {"error": one sentence to the user saying what is missing}.

If the task is unclear because a measure is missing ("unusual", "too much", "late", "long") and it is not in what Mira knows about the household either, do not guess and build nothing: answer only {"question": a short question back to the user with a suggestion, e.g. "What is an unusual time for you, say between midnight and 6 am?"}. If the measure is given there or in the task, implement it as a fixed condition or value (condition: time, numeric_state above/below …), not as a judgement of mira.assess.

Otherwise answer only with a JSON object:
{"title": one to three words,
 "summary": one sentence to the user: what you watch and that you tell them, e.g. "When the bedside lamp goes on, I'll tell you." Name no condition, time or limit in it: Mira adds that herself.,
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
- spoken: ein bis zwei kurze Sätze zum Vorlesen, Du-Form, Werte als Ziffern, mit Uhrzeit und in der Vergangenheit (die Nachricht wird vielleicht später vorgelesen); leer bei 0
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

		draftSystem:     "Du schreibst Home-Assistant-Automationen für Mira, die Assistentin eines Haushalts. Antworte nur mit JSON.",
		draftIntro:      "Aufgabe des Nutzers: %q\n\nEntitäten (id | Name | Bereich | device_class | state_class | Einheit | Zustand jetzt):",
		draftRules:      draftRulesDE,
		redraft:         "\n\nDein letzter Entwurf wurde abgelehnt: %s. Mach einen neuen, der das behebt.",
		draftFields:     "\n\nWas im Gespräch dazu geklärt ist (hat Vorrang vor deinen Annahmen):",
		draftArea:       "\n\nDer Auftrag kam am Satelliten im Bereich %s (%s, Satellit %s). „Hier“ heißt dieser Bereich.",
		draftPerson:     "\n\nGesprochen hat %s. „Ich“, „mich“ und „mir“ meinen diese Person; was Mira über sie weiß, steht oben mit.",
		draftForbidden:  "nichts weiter",
		alsoSwitches:    "Dabei schaltet sie außerdem: %s.",
		conditionsAre:   "Bedingung: %s.",
		judgesItself:    "Ob es sich lohnt, entscheide ich jedes Mal selbst; eine feste Bedingung gibt es nicht.",
		noCondition:     "Ohne weitere Bedingung.",
		condBetween:     "nur zwischen %s und %s Uhr",
		condAfter:       "nur ab %s Uhr",
		condBefore:      "nur bis %s Uhr",
		condAbove:       "über %s",
		condBelow:       "unter %s",
		condTemplate:    "eine Vorlage muss zutreffen",
		proposal:        "Mein Vorschlag für die Aufgabe %s: %s Soll ich so vorgehen?",
		planFailed:      "Für die Aufgabe „%s“ habe ich keine brauchbare Automation gefunden.",
		cannot:          "Die Aufgabe „%s“ kann ich so nicht übernehmen: %s",
		noEntityNamed:   "keine Entität des Hauses erkannt: nenne sie mit ihrer id (z. B. light.schlafzimmer) oder so, wie sie im Haus heißt; schau sie bei Bedarf nach",
		unknownEntities: "diese Entitäten gibt es nicht oder sind nicht freigegeben: %s",
		confirmed:       "Gut, die Aufgabe %s läuft.",
		tryObservation:  "Probelauf: kein Auslöser, beurteile die Daten, wie sie jetzt sind.",

		listDesc:     "Listet die Aufgaben: offene Vorschläge und Miras Automationen in Home Assistant, mit an/aus.",
		deleteDesc:   "Löscht eine Aufgabe oder einen Vorschlag anhand der id aus task_list.",
		enableDesc:   "Schaltet eine Aufgabe an oder aus (pausieren, fortsetzen), anhand der id aus task_list.",
		enableOn:     "true schaltet an, false aus.",
		tryDesc:      "Probelauf einer Aufgabe: Mira beurteilt die Daten jetzt, ohne zu melden, und sagt, was sie sagen würde. id aus task_list.",
		idDesc:       "Die id aus task_list.",
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
- spoken: one or two short sentences to be read aloud, values as digits, with the time and in the past tense (the message may be read out later); empty for 0
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

		draftSystem:     "You write Home Assistant automations for Mira, the assistant of a household. Answer only with JSON.",
		draftIntro:      "The user's task: %q\n\nEntities (id | name | area | device_class | state_class | unit | state now):",
		draftRules:      draftRulesEN,
		redraft:         "\n\nYour last draft was refused: %s. Make a new one that fixes it.",
		draftFields:     "\n\nWhat the conversation settled (takes precedence over your assumptions):",
		draftArea:       "\n\nThe task came at the satellite in the area %s (%s, satellite %s). \"Here\" means this area.",
		draftPerson:     "\n\n%s was speaking. \"I\", \"me\" and \"my\" mean this person; what Mira knows about them is included above.",
		draftForbidden:  "nothing else",
		alsoSwitches:    "It also switches: %s.",
		conditionsAre:   "Condition: %s.",
		judgesItself:    "Whether it is worth it, I decide each time myself; there is no fixed condition.",
		noCondition:     "No further condition.",
		condBetween:     "only between %s and %s",
		condAfter:       "only from %s",
		condBefore:      "only until %s",
		condAbove:       "above %s",
		condBelow:       "below %s",
		condTemplate:    "a template must hold",
		proposal:        "My proposal for the task %s: %s Shall I go ahead?",
		planFailed:      "I found no usable automation for the task \"%s\".",
		cannot:          "I cannot take on the task \"%s\" like this: %s",
		noEntityNamed:   "no entity of the house recognised: name it by its id (e.g. light.bedroom) or as it is called in the house; look it up if needed",
		unknownEntities: "these entities do not exist or are not exposed: %s",
		confirmed:       "Good, the task %s is running.",
		tryObservation:  "Dry run: no trigger, judge the data as they are now.",

		listDesc:     "Lists the tasks: open proposals and Mira's automations in Home Assistant, with on/off.",
		deleteDesc:   "Deletes a task or a proposal by its id from task_list.",
		enableDesc:   "Switches a task on or off (pause, resume), by its id from task_list.",
		enableOn:     "true switches on, false off.",
		tryDesc:      "Dry run of a task: Mira judges the data now, without reporting, and says what she would say. id from task_list.",
		idDesc:       "The id from task_list.",
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
