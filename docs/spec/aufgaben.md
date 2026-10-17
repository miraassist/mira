# Aufgaben (Tasks): Spezifikation

Stand 2026-10-15. Spezifiziert, noch nicht gebaut. Das Benutzer-Konzept
steht in [benutzer.md](benutzer.md).

## Warum

Bisher gab es zwei Konzepte für dasselbe Anliegen „sag Bescheid, wenn …“:

- **Watcher:** konfigurierte Entitäten. Jede Zustandsänderung geht an das Modell.
- **Aufträge:** im Gespräch angelegt. Das Modell plant einmal Regeln. Mira prüft sie selbst (Zustand, Grenzwert, Zeitfenster, Normalwert) und reicht nur Kandidaten an das Modell.

Für Anwender war nicht klar, wann was gilt. Beide Konzepte bauen außerdem
kleine Teile des Trigger- und Bedingungssystems von Home Assistant nach, nur
um sie in Mira auszuführen.

## Grundsatz

Eine **Aufgabe** ist eine gewöhnliche Home-Assistant-Automation mit dem
Label **„Mira“**.

- **HA** entscheidet *wann*: Trigger, Bedingungen und Templates, im vollen Umfang. HA ist die Quelle der Wahrheit. Bearbeitet wird im Automations-Editor von HA, mit Traces.
- **Mira** liefert, was HA nicht kann, als **Aktionen** für Automationen:
  - Urteilen mit dem Modell;
  - Daten aus HA verdichten, damit das Urteil billig bleibt;
  - Zustellen mit Mira-Semantik: Ansage, Anzeige, „Übrigens“, Kanäle der Person, Ruhezeit, Eskalation.
- Mira hält nur einen **Verweis** (Automations-ID) und ihr Protokoll.

Watcher und die Regel-Engine der Aufträge entfallen. Die Testdaten werden ohne Migration gelöscht.

Aufteilung: **HA sind die Sinne, Mira ist Verstand und Stimme.**

## Aktionen

Alle Aktionen stellt die Mira-Integration bereit. Sie rufen den Agenten über seine API auf.

### `mira.assess`: urteilen und gegebenenfalls melden

| Parameter | Pflicht | Bedeutung |
|---|---|---|
| `goal` | ja | Das Ziel in den Worten des Nutzers, z. B. „Achte auf den Wasserverbrauch.“ |
| `observation` | nein | Was passiert ist, als Text mit Templates. |
| `data` | nein | Liste `{entity_id, view}`: Was Mira selbst nachschlägt und verdichtet (siehe „Ansichten“). |
| `memory` | nein | `relevant` (Standard), `profile` oder `none` (siehe „Kontext“). |
| `cooldown` | nein | Zum Beispiel `6h`. Innerhalb dieser Zeit meldet dieselbe Automation nur, wenn die Wichtigkeit steigt. |
| `notify` | nein | Standard `true`. Bei `false` gibt es nur das Urteil, keine Nachricht (Probelauf). |
| `audience`, `confidentiality`, `dedupe_key`, `expires_in` | nein | Wie bei `send_message`. |

**Ablauf**
1. Mira baut einen **isolierten** Aufruf, also ohne Haus-Prompt. Er enthält:
   - Ziel, Beobachtung und die verdichteten Daten;
   - den Kontext (Gedächtnis, Anwesenheit, Tag und Uhrzeit);
   - die letzten Urteile dieser Automation.
2. Das Modell antwortet mit `{importance 0–3, spoken, details}`.
3. Bei Wichtigkeit > 0 und `notify` (und außerhalb der Sperrzeit) entsteht eine Nachricht. Quelle ist `task`, der Verweis zeigt auf die Automation.

**Antwort** (für `response_variable`): `{importance, spoken, details, message_id | null}`.

### `mira.decide`: Urteil als Bedingung

| Parameter | Pflicht | Bedeutung |
|---|---|---|
| `question` | ja | Eine Ja/Nein-Frage, z. B. „Läuft hier gerade unnötig Wasser?“ |
| `observation`, `data`, `memory` | nein | wie bei `assess` |

**Antwort:** `{answer: true|false, reason}`. Es entsteht keine Nachricht.

Eine native Bedingung (`condition: mira`) ist nicht möglich: HA prüft Bedingungen synchron, das Modell braucht Sekunden. Stattdessen nutzt die Automation die Aktion mit Antwort und danach eine Bedingung:

```yaml
- action: mira.decide
  data:
    question: Läuft hier gerade unnötig Wasser?
    data: [{ entity_id: sensor.wasserzahler, view: same_hour }]
  response_variable: d
- condition: template
  value_template: "{{ d.answer }}"
```

### `mira.send_message`: melden ohne Urteil (gibt es schon)

`text`, `importance`, `details`, `audience`, `confidentiality`, `dedupe_key`, `expires_in` → Nachricht. Für Fälle, in denen das Urteil feststeht, etwa einen Wassermelder.

## Ansichten (`data[].view`)

Mira bereitet Daten so auf, dass das Modell wenige, passende Zahlen sieht statt Rohdaten. Das ist ihr Beitrag gegen den Token-Verbrauch. Zu jeder Entität gehen Name, Bereich und Einheit mit.

| `view` | Inhalt |
|---|---|
| `state` | aktueller Zustand, seit wann |
| `same_hour` | letzte volle Stunde (Verbrauch bei Zählern, Mittel bei Messwerten) neben derselben Stunde der letzten 14 Tage: Median, Bandbreite, Maximum. Quelle: `recorder/statistics_during_period` |
| `day` | heute bis jetzt neben denselben Wochentagen der Vorwochen |
| `upcoming` | nur Kalender: die Termine von jetzt bis Ende morgen, in der Darstellung der Zeitachse ([Kontext](kontext.md), Abschnitt 2) |

Geplant, mit derselben Darstellung: `history` (Verlauf), `list` (Listen-Attribute); dazu `collected` (Gesammeltes, siehe „Später“).

## Kontext jedes Urteils

- **Gedächtnis** (`memory`):
  - `relevant`: das Haushaltsprofil plus höchstens 5 Fakten, die über die Volltextsuche zu Ziel, Frage oder Beobachtung passen.
  - `profile`: nur das Profil.
  - `none`: nichts.
  - Nur lesen, Urteile schreiben nichts ins Gedächtnis.
  - Welches Gedächtnis mitgeht, bestimmt der Besitzer der Aufgabe (`owner`, siehe [benutzer.md](benutzer.md)): immer das Hausgedächtnis, bei persönlichen Aufgaben dazu das der Person.
- **Anwesenheit:** wer zu Hause ist, aus den Personen-Entitäten.
- **Tag:** Wochentag, Uhrzeit, ob Ruhezeit ist.
- **Gedächtnis der Aufgabe:** die letzten Urteile derselben Automation (wann, Wichtigkeit, gesagt). So nervt das Modell nicht mit Wiederholungen und kann auf Verläufe hinweisen.

Größe eines Aufrufs: etwa 500 bis 1.500 Tokens. Stündliche Bewertung einer Aufgabe kostet mit Gemini Flash grob 0,5 Cent pro Tag.

## Anlegen im Gespräch

Seit Agent 0.4.0 ist das Anlegen ein [Vorgang](vorgaenge.md) mit dem Ziel
`aufgabe` (Skill `skills/aufgabe`): Felder watch, when, measure, reaction;
fehlt ein Maßstab, fragt Mira nach; der Vorschlag gehört zum Vorgang, das
Ja ist `thread_confirm`. Der Ablauf unten ist der ursprüngliche Entwurf.

1. „Achte auf den Wasserverbrauch.“ → `task_create(goal)`. Mira antwortet sofort: „Ich plane das und melde mich mit einem Vorschlag.“
2. Im Hintergrund entwirft das Modell die Automation (`alias`, `description` = Ziel, `triggers`, `conditions`, `actions`). Es bekommt den Katalog der Entitäten und diese Spezifikation der Aktionen.
3. Die Integration lässt HA den Entwurf prüfen (`mira/automation/validate`). Bei Fehlern gibt es einen zweiten Entwurf mit dem Fehlertext.
4. Mira stellt den Vorschlag zu, im selben Raum oder Chat: kurz, was beobachtet wird und wann sie sich meldet, und „Soll ich so vorgehen?“.
5. Bei Ja speichert die Integration die Automation (`mira/automation/save`), mit Label „Mira“. Mira merkt sich die ID.

**Wo und von wem** (seit 0.6.17): Der Entwurf erfährt, an welchem Satelliten der Auftrag kam (Bereich, Satellit) und wer gesprochen hat, wenn Mira es weiß (Telegram, HA-App, allein zu Hause). So lösen sich „hier“ und „ich“ auf, und das Gedächtnis dieser Person geht mit. Die Person wird **Besitzer** der Aufgabe: Der Code trägt sie als `owner` in jedes `mira.assess` und `mira.decide` ein, und jedes Urteil sieht dann auch ihr Gedächtnis.

Weitere Werkzeuge:
- `task_list`: Mira-Automationen aus HA, plus offene Vorschläge.
- `task_delete`.
- An/aus über `automation.turn_on/off`.
- Probelauf: die Aktionen der Automation mit `notify: false` ausführen und das Urteil vorlesen.

## Integration

- **Websocket-Befehle**, nur für Miras HA-Benutzer (die Integration erfährt die ID über `/v1/health`) oder Admins:
  - `mira/automation/validate`;
  - `mira/automation/save` (über den Weg des Editors: Eintrag in `automations.yaml` ersetzen, eine Automation neu laden, Label setzen);
  - `mira/automation/delete`;
  - `mira/automation/list`.
- **Sicherheitsregel** (2026-10-15 gelockert):
  - Eine Aufgabe darf auch schalten und benachrichtigen, etwa Radio, Licht oder eine Push-Nachricht mit `notify.*`. Das ist die Entscheidung des Nutzers. Er muss dabei wissen, dass ein Modell entscheidet und nicht deterministisch urteilt.
  - Der Vorschlag nennt diese Aktionen ausdrücklich, vom Code erzeugt und nicht dem Modell überlassen („Dabei schaltet sie außerdem: …“). Das Ja gilt genau dafür.
  - Gesperrt bleibt, was Mira auch im Gespräch nur nach ausdrücklichem Ja schaltet (`security.confirm_domains` und `confirm_entities`, z. B. Schlösser, Alarm, Garagentor). Bei solchen Domains gilt das auch für Ziele über Bereiche oder Geräte.
  - Ebenfalls gesperrt ist alles, was beliebigen Code ausführt oder HA selbst verändert: Skripte, Shell-Befehle, Neustarts, Updates.
  - Wer das trotzdem will, trägt es im Editor von HA selbst ein. Dort greift die Regel nicht.
  - Grund: Am Satelliten kann jeder sprechen, und eine Aufgabe wirkt dauerhaft und unbeaufsichtigt. Außerdem liefen Automationen sonst mit Admin-Rechten, die Mira bewusst nicht hat.
- Voraussetzung: `configuration.yaml` bindet `automations.yaml` ein (Standard).
- Risiko: Für das Speichern von Automationen gibt es keine stabile Schnittstelle für Integrationen. Der Editor-Weg kann bei HA-Updates Nacharbeit brauchen.

## Mira-UI

Reiter „Aufgaben“:
- Mira-Automationen mit an/aus und „zuletzt ausgelöst“, Link in den HA-Editor, Probelauf, löschen.
- Offene Vorschläge.
- Pro Aufgabe das **Protokoll** der Urteile: Zeit, Daten in Kurzform, Wichtigkeit, gesagt, Nachricht. Das ergänzt die Traces von HA, die nur zeigen, *dass* `assess` lief.

## Entfällt

- Watcher (Paket, Config, UI-Reiter, Unter-Einträge der Integration für Watcher und Feeds).
- Die Regel-Engine der Aufträge (usage/level/state/threshold, Fünf-Minuten-Schleife).

Die Statistik-Auswertung lebt als Ansicht `same_hour` weiter.

## Erster Bau

- `assess` mit `data` (`state`, `same_hour`, `day`), `memory`, `cooldown` und `notify`;
- `decide`;
- `send_message`;
- Kontext und Gedächtnis der Aufgabe;
- Protokoll;
- Anlegen im Gespräch;
- Aufgabenliste mit Probelauf;
- Watcher und Aufträge entfernen.

Getestet wird an der Test-HA.

## Später

- **Sammeln:** `mira.collect(bucket, observation)` speichert ohne Modell. `assess` mit `view: collected` bewertet das Gesammelte gebündelt, z. B. täglich, und leert den Eimer. Für Feeds (`feedreader`), Kalender und laute Sensoren. Kommt mit den Skills.
- **Skills:** nutzen HA als Datenquelle, wo HA die Daten nativ führt (Kalender, Feeds), und verdichten clever. Wo HA die Daten nicht oder schlecht modelliert, arbeiten sie wie bisher selbst. Ihren proaktiven Teil bringen sie als HA-Blueprint mit.
- **Handeln mit Bestätigung:** Ein Urteil schlägt eine Handlung vor („Soll ich das Ventil schließen?“). Auf Ja führt Mira sie über ihren normalen Weg aus, mit Schutz und Bestätigung.

## Offen

- **Benutzer-Konzept:** geklärt in [benutzer.md](benutzer.md). Aufgaben haben einen Besitzer (Haus oder Person). `assess` und `decide` bekommen `owner`. Persönliches Gedächtnis geht nur bei persönlichen Aufgaben mit.
- **Besprechungsliste:**
  - Bestätigen per festem Ja im Code (sprachabhängige Phrasen);
  - Bearbeiten von Aufgaben im Gespräch („nimm den Sensor raus“). Mit HA als Editor heißt das: Mira ändert die Automation per `save`.
