package tasks

import (
	"fmt"
	"strings"

	"github.com/sker65/mira/internal/i18n"
)

type texts struct {
	plan, planRules                    string
	usageEvidence, levelEvidence       string
	stateEvidence                      string
	judge, judgeRules                  string
	describe                           string
	createDesc, createGoal             string
	confirmDesc, listDesc, deleteDesc  string
	proposed, activated, deleted, none string
	noTask                             string
	replan                             string
	planning, proposal, planFailed     string
}

// maxCatalog bounds the entity list in the planning prompt.
const maxCatalog = 250

func (m *Manager) planInstruction(goal string, cat []Entity) string {
	t := m.texts()
	var b strings.Builder
	fmt.Fprintf(&b, t.plan, goal)
	for i, e := range cat {
		if i == maxCatalog {
			break
		}
		fmt.Fprintf(&b, "\n- %s | %s | %s | %s | %s | %s | %s", e.ID, e.Name, e.Area, e.DeviceClass, e.StateClass, e.Unit, e.State)
	}
	b.WriteString(t.planRules)
	return b.String()
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		plan: "Der Nutzer gibt dir einen dauerhaften Auftrag: %q\n\nPlane, wie du ihn erfüllst. Wähle aus diesen Entitäten (id | Name | Bereich | device_class | state_class | Einheit | Zustand jetzt):",
		planRules: `

Regeln, aus denen ein Plan besteht (rules, höchstens 8):
- kind "usage": Zähler mit state_class total oder total_increasing (Wasser, Strom, Gas). Gemeldet wird, wenn in einer Stunde viel mehr verbraucht wird als sonst zu dieser Stunde.
- kind "level": Messwert mit state_class measurement (Temperatur, Leistung, Feuchte). Gemeldet wird ein Stundenmittel außerhalb dessen, was zu dieser Stunde üblich ist; direction high, low oder both.
- kind "state": Zustand, der auffällt, mit states, z. B. Tür "on" (offen) oder Alarm "triggered".
- kind "threshold": Zahl über above oder unter below.
- when: "night" (Ruhezeit), "day" oder "always".
- why: ein kurzer Satz, warum diese Regel zum Auftrag gehört.

Nimm nur Entitäten aus der Liste und nur so viele, wie der Auftrag wirklich braucht. Lieber wenige treffende Regeln als viele.
title: ein bis drei Wörter. summary: ein bis zwei Sätze an den Nutzer, was du beobachtest und wann du dich meldest, in der Du-Form.
Antworte nur mit dem JSON-Objekt.`,
		usageEvidence: "%s (%s) ist zwischen %s und %s um %s %s gestiegen. Zu dieser Stunde an den letzten %d Tagen: typisch %s %s, höchstens %s %s.",
		levelEvidence: "%s (%s) lag zwischen %s und %s im Mittel bei %s %s. Zu dieser Stunde an den letzten %d Tagen: zwischen %s %s und %s %s.",
		stateEvidence: "%s (%s) ist jetzt %s %s (seit der letzten Prüfung, %s Uhr).",
		judge:         "Ein dauerhafter Auftrag des Nutzers: %q\nDein Plan dafür: %s\nDiese Regel gehört dazu, weil: %s\n\nBeobachtung: %s\n\n",
		judgeRules: `Beurteile, ob das den Nutzer im Sinne seines Auftrags interessiert. Antworte nur mit einem JSON-Objekt:
- importance: 0 = nicht melden (etwa erklärbar oder harmlos), 1 = nebenbei, 2 = wichtig, sollte heute jemand ansehen, 3 = dringend, Schaden droht (etwa Wasser läuft nachts ungewöhnlich lange)
- spoken: ein bis zwei kurze Sätze zum Vorlesen, sachlich, Du-Form, mit den konkreten Werten als Ziffern; leer bei 0
- details: die Langfassung mit den Vergleichswerten
Behaupte nur, was in der Beobachtung steht.`,
		describe:    "Auftrag %s, %q (%s): %s",
		createDesc:  "Legt einen dauerhaften Auftrag an, wenn der Nutzer möchte, dass du auf etwas achtest, z. B. \"Achte auf den Wasserverbrauch\", \"Sag Bescheid, wenn nachts ungewöhnlich viel Strom läuft\", \"Pass auf das Haus auf\". Du bekommst einen Plan zurück: sag ihn dem Nutzer kurz und frag, ob du so vorgehen sollst. Erst nach seinem Ja rufst du task_confirm auf. Wenn das Planen im Hintergrund läuft, kommt der Vorschlag später von selbst.",
		createGoal:  "Der Auftrag in den Worten des Nutzers.",
		confirmDesc: "Startet einen geplanten Auftrag, nachdem der Nutzer dem Plan zugestimmt hat, etwa mit \"Starte den Auftrag\". Ohne id den zuletzt vorgeschlagenen.",
		listDesc:    "Listet die Aufträge mit ihren Plänen.",
		deleteDesc:  "Löscht einen Auftrag anhand seiner id (aus task_list).",
		proposed:    "Plan erstellt, noch nicht aktiv. Sag dem Nutzer kurz, was du beobachten würdest, und frag, ob du so vorgehen sollst.\n",
		activated:   "Auftrag aktiv: ",
		deleted:     "Auftrag gelöscht.",
		none:        "Keine Aufträge.",
		noTask:      "keinen Auftrag mit dieser id",
		replan:      "\n\nDein letzter Plan wurde abgelehnt: %s. Mach einen neuen, der die Regeln einhält.",
		planning:    "Ich plane den Auftrag jetzt im Hintergrund; der Vorschlag kommt in etwa einer halben Minute von selbst. Sag dem Nutzer das in einem kurzen Satz.",
		proposal:    "Mein Vorschlag für den Auftrag %s: %s Wenn das passt, sag: Starte den Auftrag.",
		planFailed:  "Für den Auftrag „%s“ habe ich keinen brauchbaren Plan gefunden.",
	},
	i18n.EN: {
		plan: "The user gives you a standing order: %q\n\nPlan how to fulfil it. Choose from these entities (id | name | area | device_class | state_class | unit | state now):",
		planRules: `

A plan consists of rules (at most 8):
- kind "usage": a counter with state_class total or total_increasing (water, electricity, gas). Reported when an hour uses much more than usual at that hour.
- kind "level": a measurement with state_class measurement (temperature, power, humidity). Reported when an hourly mean is outside what is usual at that hour; direction high, low or both.
- kind "state": a state worth noticing, with states, e.g. a door "on" (open) or an alarm "triggered".
- kind "threshold": a number above above or below below.
- when: "night" (quiet hours), "day" or "always".
- why: one short sentence why this rule belongs to the order.

Use only entities from the list and only as many as the order really needs. Rather a few fitting rules than many.
title: one to three words. summary: one or two sentences to the user about what you watch and when you speak up.
Answer only with the JSON object.`,
		usageEvidence: "%[1]s (%[2]s) rose by %[5]s %[6]s between %[3]s and %[4]s. At this hour on the last %[7]d days: typically %[8]s %[9]s, at most %[10]s %[11]s.",
		levelEvidence: "%[1]s (%[2]s) averaged %[5]s %[6]s between %[3]s and %[4]s. At this hour on the last %[7]d days: between %[8]s %[9]s and %[10]s %[11]s.",
		stateEvidence: "%s (%s) is now %s %s (since the last check, %s).",
		judge:         "A standing order of the user: %q\nYour plan for it: %s\nThis rule belongs to it because: %s\n\nObservation: %s\n\n",
		judgeRules: `Judge whether this interests the user in the sense of the order. Answer only with a JSON object:
- importance: 0 = do not report (explainable or harmless), 1 = in passing, 2 = important, someone should look today, 3 = urgent, damage is imminent (e.g. water running unusually long at night)
- spoken: one or two short sentences to be read aloud, factual, with the concrete values as digits; empty for 0
- details: the long form with the comparison values
Only claim what the observation says.`,
		describe:    "Order %s, %q (%s): %s",
		createDesc:  "Creates a standing order when the user wants you to watch something, e.g. \"Keep an eye on the water usage\", \"Tell me if a lot of power is used at night\", \"Watch the house\". You get a plan back: tell it to the user briefly and ask whether to go ahead. Only after their yes call task_confirm. If planning runs in the background, the proposal arrives later by itself.",
		createGoal:  "The order in the user's words.",
		confirmDesc: "Starts a planned order after the user agreed to the plan, e.g. with \"Start the order\". Without an id, the most recently proposed one.",
		listDesc:    "Lists the orders with their plans.",
		deleteDesc:  "Deletes an order by its id (from task_list).",
		proposed:    "Plan made, not active yet. Tell the user briefly what you would watch and ask whether to go ahead.\n",
		activated:   "Order active: ",
		deleted:     "Order deleted.",
		none:        "No orders.",
		noTask:      "no order with this id",
		replan:      "\n\nYour last plan was refused: %s. Make a new one that keeps the rules.",
		planning:    "I am planning the order in the background now; the proposal arrives by itself in about half a minute. Tell the user so in one short sentence.",
		proposal:    "My proposal for the order %s: %s If that suits you, say: Start the order.",
		planFailed:  "I found no usable plan for the order \"%s\".",
	},
}
