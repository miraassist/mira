# Agenten – Spezifikation

Stand 2026-10-21, Entwurf. Nichts davon ist gebaut.

## Ziel

Mira ruft ihre Modelle heute an acht Stellen auf, jede auf ihre Art: das Gespräch, die Zeitpläne der Skills, das
Delegieren, das Beurteilen und Planen von Aufgaben, das Lesen von Mails, das Verdichten des Profils, das Formulieren
von Ansagen. Jede davon ist in Wahrheit ein eigener **Agent**: ein Modell mit einem bestimmten Kontext, bestimmten
Werkzeugen, eigenen Regeln und einer eigenen Form der Antwort. Davon steht nur das Modell in der Konfiguration
(`routing`); alles andere steckt verteilt im Code (Bestand am Ende).

Der **Agent** wird ein Baustein im Code. Die acht bestehenden werden **interne Agenten**, im Code beschrieben, ohne
dass sich ihr Verhalten ändert. Ein Agent ist **aufrufbar wie ein Werkzeug**: Er hat eine Beschreibung, ein
Eingabe-Schema und eine Antwort. So kann ihn ein anderer Agent als Werkzeug rufen, Home Assistant als Aktion, und
der Code direkt. Zuletzt, optional, beschreibt eine **Factory** Agenten ohne Code, in YAML.

Zwei Dinge werden dabei getrennt: was zu einem Agenten **fest** gehört (seine Beschreibung), und was mit **jedem
Aufruf** mitkommt und an aufgerufene Agenten weitergereicht wird (der Aufruf-Kontext).

## Die statische Beschreibung

Was zu einem Agenten gehört, gleich bei jedem Aufruf:

| Teil | Bedeutung | Beispiele |
|---|---|---|
| **Name** | wie er im Log, in `routing` und als Werkzeug heißt | `conversation.voice`, `mail.read`, `tasks.plan` |
| **Beschreibung** | wann man ihn ruft; für das aufrufende Modell, wie bei einem Werkzeug | „Liest eine Versandmail und gibt Titel, Tag und Kennungen zurück.“ |
| **Eingabe** | Schema dessen, was er beim Aufruf bekommt | `{text}`, `{mail: {sender, subject, date, text}}` |
| **Ausgabe** | Form der Antwort | Text, oder JSON nach einem Schema |
| **Modell** | die Rolle in `routing` | `routing.mail`; ohne Eintrag `main` |
| **Kontext** | was das Modell sieht, als Profil | *Gespräch* (Schichten 1–7, Verlauf), *Hintergrund* (fester Anfang mit Haus, ohne Verlauf), *isoliert* (nur eine eigene Systemnachricht und die Eingabe) |
| **Werkzeuge** | welche Werkzeuge und welche anderen Agenten er rufen kann | *keine*, *nur lesend*, *alle*, oder eine Liste |
| **Rechte** | was der Agent höchstens braucht (siehe Rechte) | `mail.read`: keine; `conversation.voice`: alle |
| **Grenzen** | Vorgaben für Werkzeugrunden, Zeit je Versuch, Wiederholung | höchstens 3 Runden; 60 s, einmal wiederholen |

## Der Aufruf-Kontext

Was mit jedem Aufruf mitkommt. Ruft ein Agent einen anderen, reicht er seinen Aufruf-Kontext weiter, enger, nie
weiter.

| Teil | Bedeutung |
|---|---|
| **Aufruf-ID** | eindeutig je Aufruf; dazu die ID des aufrufenden Aufrufs, falls es einen gibt |
| **Herkunft** | wer ruft: Home Assistant (Automation, Assist, Telegram, IMAP), Miras Zeitgeber, die Mira-Seite, ein anderer Agent |
| **Person, Ort, Sprache** | für wen und wo, soweit bekannt |
| **Rechte** | was dieser Aufruf darf (siehe Rechte) |
| **Tiefe** | wie viele Agenten über diesem stehen; 0 bei einem äußeren Aufruf |
| **Fremder Text** | ob im Aufruf schon fremder Text im Spiel ist (Web-Inhalt, ein fremder Plan) |
| **Modus** | synchron mit Timeout, oder asynchron mit Zustellweg (siehe Synchron und asynchron) |

## Rechte

Ein kleines Rechtemodell, nur was Mira heute schon unterscheidet:

| Recht | erlaubt |
|---|---|
| **Haus lesen** | Zustände, Kalender, Listen lesen |
| **Haus schalten** | Aktionen in Home Assistant, die etwas ändern |
| **Merken** | ins Gedächtnis schreiben |
| **Nachrichten** | Nachrichten an Personen oder das Haus senden |
| **Netz** | Websuche und Webseiten lesen |

- Ein Lauf darf, was **beide** erlauben: die Rechte im Aufruf-Kontext und die Rechte in der Beschreibung des
  Agenten.
- Ruft ein Agent einen anderen, gibt er im Aufruf-Kontext höchstens seine eigenen wirksamen Rechte weiter. Ein
  Agent, der nicht schalten darf, kann auch über einen anderen nicht schalten.
- Die Rechte kommen zu den bestehenden Regeln dazu, sie ersetzen sie nicht: Der Wächter fragt bei kritischen
  Aktionen weiter nach, nach Web-Inhalt sperrt er sie, und nach fremdem Text fragt Mira vor dem Behalten und
  Weitergeben.
- Hat ein aufgerufener Agent fremden Text gelesen, steht das im Ergebnis; für den Aufrufer gilt es dann, als hätte
  er ihn selbst gelesen.

## Aufrufen

Alle Wege gehen durch dieselbe Schnittstelle: Agent, Eingabe, Aufruf-Kontext; zurück kommt die Antwort, oder bei
asynchronen Aufrufen die Aufruf-ID.

- **Als Werkzeug eines anderen Agenten:** Steht ein Agent in der Werkzeugliste eines anderen, sieht dessen Modell
  ihn als Werkzeug: Name, Beschreibung, Eingabe-Schema. Der Aufruf-Kontext kommt vom aufrufenden Lauf. So
  funktioniert `delegate` heute schon, nur fest eingebaut.
- **Aus Home Assistant:** eine Aktion, etwa am Ende einer Automation, mit Agent, Eingabe und Modus; synchron mit
  Antwort für die Automation (`response_variable`), so wie `mira.assess` heute. Vorschlag für den Namen:
  `mira.agent`.
- **Aus dem Code:** direkt, so wie heute der Notifier `notify.compose` ruft.

## Synchron und asynchron

Der **Aufrufer** entscheidet, im Modus des Aufruf-Kontexts.

- **Synchron mit Timeout:** Der Aufrufer wartet auf die Antwort, höchstens bis zum Timeout. Was beim Timeout
  geschieht, sagt der Aufrufer: abbrechen, oder weiterlaufen lassen und das Ergebnis asynchron zustellen. Das
  Gespräch am Satelliten macht es so (wie heute die Vorgänge: kurz warten, sonst „ich melde mich“).
- **Asynchron:** Der Aufruf gibt sofort die Aufruf-ID zurück, der Agent läuft im Hintergrund.

### Der Lauf-Datensatz

Jeder asynchrone Lauf (und jeder synchrone, der nach dem Timeout weiterläuft) bekommt einen Datensatz:
- Aufruf-ID, Agent, ID des aufrufenden Aufrufs;
- Status: läuft, fertig, fehlgeschlagen, abgelaufen;
- die Ausgabe nach dem Schema des Agenten, oder der Fehler;
- Beginn und Ende.

Nach einer Frist wird er gelöscht. Abfragen lässt er sich jederzeit über die Aufruf-ID, auch aus Home Assistant
(Vorschlag: `mira.agent_result`).

### Zustellen

Ist der Lauf fertig, wird der Aufrufer benachrichtigt, auf dem Weg, den er im Aufruf-Kontext angegeben hat:
1. **Home-Assistant-Ereignis** mit Aufruf-ID, Agent, Status und Ausgabe; eine Automation kann daran weitermachen
   (Vorschlag: `mira_agent_done`).
2. **Nachricht** an eine Person oder das Haus. Das Gespräch formuliert sie daraus („So hab ich mir das gedacht“);
   das ist der Weg für Menschen.
3. **Fortsetzung** des aufrufenden Agenten oder Vorgangs mit dem Ergebnis. Dieser Weg kommt zuletzt.

Den Kern davon gibt es schon: Der Aufgaben-Planer läuft asynchron, sein Entwurf liegt als JSON im Vorgang (die
Vorgangs-ID ist die Aufruf-ID), und daraus wird eine Ansage.

## Tiefe

Ein Agent kann einen anderen rufen, der wieder einen ruft. Die Tiefe ist begrenzt, mit einer Einstellung
(Vorschlag: `agents.max_depth`). Wird sie überschritten, scheitert der Aufruf mit diesem Grund.

## Protokoll

Jeder Lauf schreibt einheitlich eine Zeile `agent done`: Agent, Aufruf-ID, ID des aufrufenden Aufrufs, Tiefe,
Herkunft, Modus, Status, Modell, Dauer, Modellaufrufe, Tokens. Über die Aufruf-IDs lässt sich ein ganzer Baum von
Aufrufen nachverfolgen. Zeilen, die sagen, was ein Lauf fachlich bewirkt hat (etwa `mail read`), bleiben dazu.

## Der Baustein im Code

Ein Paket `internal/agent`:
- **`agent.Spec`:** die statische Beschreibung, im Code als Werte geschrieben.
- **`agent.CallContext`:** der Aufruf-Kontext.
- **`agent.Call(ctx, spec, input, callContext)`:** führt einen Lauf aus. Stellt den Kontext nach dem Profil
  zusammen, ruft das Modell, führt erlaubte Werkzeuge mit dem Wächter und den wirksamen Rechten aus, prüft die
  Ausgabe gegen das Schema, wiederholt nach den Grenzen, schreibt den Lauf-Datensatz (bei asynchron) und die
  Protokollzeile, und stellt zu.
- Eine **Liste der Agenten**, über die sie als Werkzeug und aus Home Assistant gefunden werden.

Das Gespräch behält seinen eigenen Ablauf (Verlauf, Kürzung, Vorgang, Bestätigungen); es bekommt eine `agent.Spec`
und einen Aufruf-Kontext, und dieselbe Protokollzeile.

## Umstellung in Schritten

Die Schritte 1 bis 5 ändern kein Verhalten; die vorhandenen Tests müssen unverändert grün bleiben, dazu kommen Tests
je Agent.

1. **Baustein:** `Spec`, `CallContext`, Rechte, synchroner `Call`, Tiefe, Protokollzeile. Noch kein Nutzer.
2. **Isolierte Agenten:** `mail.read`, `mail.match`, `tasks.plan`, `tasks.assess`, `tasks.decide`,
   `profile.compact`. Die Wiederholung des Mail-Kalenders wandert in die Grenzen von `mail.*`.
3. **Hintergrund ohne Werkzeuge:** `notify.compose`.
4. **Hintergrund mit Werkzeugen:** `skill.task`, `delegate`. `delegate` wird ein gewöhnlicher Agent als Werkzeug.
5. **Gespräch:** `conversation.voice`, `conversation.telegram`.
6. **Asynchron:** Lauf-Datensatz, Abfrage, Zustellen als HA-Ereignis und als Nachricht; die HA-Aktion.
7. **Fortsetzung** als Zustellweg.
8. **Factory:** Agenten in YAML.

## Modellwahl

`routing` bleibt; die Schlüssel sind die Rollen der Agenten (`voice`, `telegram`, `background`, `delegate`, `tasks`,
`mail`). Ohne Eintrag für seine Rolle nimmt jeder Agent `main`. Neue Rollen kommen nur, wenn jemand sie braucht.
Ob es statt fester Rollen beliebig viele benannte Modelle geben soll, ist ein eigenes, späteres Thema.

## Factory: Agenten in YAML (optional, zuletzt)

Ein Agent ohne Code, mit derselben statischen Beschreibung, in der `mira.yaml` und in Skills (beides):

```yaml
agents:
  rezept:
    description: Macht aus einer Rezeptseite eine Liste von Schritten.
    model: smart            # Modell (Name unter models, oder main)
    context: isolated       # isolated | background
    tools: read_only        # none | read_only | eine Liste von Werkzeugen und Agenten
    input:                  # Schema der Eingabe
      type: object
      properties: { url: { type: string } }
    instructions: >-
      Du bekommst eine Rezeptseite und gibst die Schritte als Liste zurück …
    output:                 # optional: JSON-Schema der Antwort
      type: object
      properties: { steps: { type: array, items: { type: string } } }
```

Aufgerufen wird er wie jeder Agent: als Werkzeug eines anderen Agenten, aus Home Assistant oder aus dem Code.

## Bestand

### Die acht internen Agenten

| Agent | heute im Code | Modell | Kontext | Werkzeuge | Ausgabe |
|---|---|---|---|---|---|
| `conversation.voice`, `conversation.telegram` | `Engine.turn` | `routing.voice` / `telegram`, sonst `main` | Gespräch | alle, mit Wächter | Text |
| `skill.task` (Zeitpläne, Briefing) | `Engine.RunTask` → `toolLoop` | `background` | Hintergrund | nur lesend | JSON-Nachricht |
| `delegate` | `Engine.Delegate` → `toolLoop` | `routing.delegate`; ohne Angabe gibt es ihn nicht | Hintergrund | nur lesend | Text |
| `tasks.assess`, `tasks.decide` | `tasks` über `isolatedModel` | `background` | isoliert | keine | JSON |
| `tasks.plan` | `tasks` über `isolatedModel` | `routing.tasks`, sonst Hintergrund | isoliert | keine | JSON |
| `mail.read`, `mail.match` | `mailcal` über `isolatedModel` | `routing.mail`, sonst Hintergrund | isoliert | keine | JSON, im Code geprüft; 60 s je Versuch, einmal wiederholen |
| `profile.compact` | `profile` über `AssessIsolated` | `background` | isoliert | keine | JSON |
| `notify.compose` (Ansagen) | `notifier` über `Engine.Assess` | `background` | Hintergrund ohne Werkzeuge | keine | JSON |

Dabei fällt auf:
- Die Rückfallregel ist uneinheitlich: Gespräche fallen auf `main`, alles andere auf `background`, `delegate` gibt es
  ohne Angabe gar nicht.
- Protokolliert werden Zeit und Tokens an drei Stellen verschieden (`model round` im Gespräch, `task done` /
  `delegate done` im Werkzeuglauf, `mail read` beim Mail-Kalender), bei Profil, Aufgaben und Ansagen gar nicht.
- Die Wiederholung bei hängendem Modell gibt es nur beim Mail-Kalender.

### Aufruf und Austausch heute

| Agent | ausgelöst von | Weg | Ergebnis geht an |
|---|---|---|---|
| `conversation.voice` | HA: Assist-Pipeline eines Satelliten | Integration → `POST /v1/conversation` | zurück an HA (gesprochen); Gedächtnis, Vorgänge, Nachrichten |
| `conversation.telegram` | HA: Ereignis von HAs Telegram-Integration | Integration (`telegram.py`) → `POST /v1/conversation` | zurück an HA (Telegram) |
| `skill.task` | Mira selbst: Zeitplan eines Skills | interner Zeitgeber → `RunTask` | Nachricht in die Warteschlange → Notifier |
| `delegate` | ein anderer Agent: das Gespräch ruft das Werkzeug `delegate` | direkter Aufruf, wartet auf die Antwort | als Werkzeug-Ergebnis zurück an das Gespräch |
| `tasks.assess`, `tasks.decide` | HA: eine Automation mit Label „Mira“ ruft `mira.assess` / `mira.decide` | Integration → `POST /v1/assess` / `/v1/decide` | zurück an die Automation (Antwort der Aktion); `assess` legt auf Wunsch eine Nachricht an |
| `tasks.plan` | ein Vorgang „aufgabe“, den das Gespräch eröffnet und gefüllt hat | Abschluss des Vorgangs, im Hintergrund | in den Vorgang (Datenbank) → als Nachricht angesagt → das Ja kommt in einem späteren Gespräch |
| `mail.read`, `mail.match` | HA: Ereignis `imap_content` | Integration (`mail.py`) → `POST /v1/mail` | in einen Kalender in HA; gelesen von Gespräch und Briefing |
| `profile.compact` | Mira selbst: Zeitplan (`memory.profile_schedule`); oder die Mira-Seite | interner Zeitgeber / `POST /v1/ui/memory/compact` | ins Profil (Datenbank); gelesen von den Gesprächen als Kontext |
| `notify.compose` | der Notifier, wenn mehrere Nachrichten zugleich anzusagen sind | direkter Aufruf | Text zurück an den Notifier → Ansage über HA |

Daraus:
- **Äußere Auslöser** kommen alle über Home Assistant (Assist, Telegram, IMAP, Automationen mit `mira.*`) und die
  Integration als HTTP-Aufruf an den Agenten; dazu die Mira-Seite.
- **Innere Auslöser** sind Miras eigene Zeitgeber (Skills, Profil) und der Notifier.
- **Ein Agent ruft einen anderen direkt** an zwei Stellen: das Gespräch den `delegate` (als Werkzeug) und der
  Notifier `notify.compose`. Beide warten auf die Antwort.
- **Sonst tauschen Agenten nichts direkt aus, sondern über gemeinsamen Zustand:** die Warteschlange der
  Nachrichten, die Vorgänge, das Gedächtnis und Profil (alle in Miras Datenbank) und Kalender in Home Assistant.
- **Kein Agent ruft heute einen anderen über einen HA-Service auf.** Die Services `mira.assess`, `mira.decide` und
  `mira.send_message` gibt es, sie werden aber nur von Automationen benutzt.

## Entschieden

1. **Gespräch:** zwei Agenten, `conversation.voice` und `conversation.telegram`; die Kanäle sind zu verschieden.
2. **YAML-Agenten** stehen in der `mira.yaml` und in Skills, beides.
3. **Modell:** ohne Eintrag für die Rolle nimmt jeder Agent `main`.
4. **Ein Agent ist aufrufbar wie ein Werkzeug** (Beschreibung, Eingabe-Schema, Antwort): als Werkzeug eines anderen
   Agenten, aus Home Assistant, aus dem Code.
5. **Statische Beschreibung und Aufruf-Kontext sind getrennt.** Rechte gehören in den Aufruf-Kontext; ein kleines
   Rechtemodell mit nur dem, was gebraucht wird.
6. **Die Tiefe ist begrenzt**, mit einer Einstellung.
7. **Das Protokoll ist einheitlich**, durch den Baustein.
8. **Synchron oder asynchron entscheidet der Aufrufer**, synchron mit Timeout.
9. **Asynchrone Ergebnisse** liegen als Lauf-Datensatz mit Aufruf-ID und Ausgabe nach Schema; der Aufrufer wird
   benachrichtigt. Eine Nachricht ist einer der Wege, nicht der einzige.

## Offene Fragen

1. **Namen:** die HA-Aktionen (`mira.agent`, `mira.agent_result`), das HA-Ereignis (`mira_agent_done`) und die
   Einstellung für die Tiefe (`agents.max_depth`) sind Vorschläge.
2. **Vorgaben:** wie tief Agenten einander höchstens rufen dürfen, und wie lange ein Lauf-Datensatz bleibt.
3. **Welche internen Agenten aus Home Assistant aufrufbar sind:** alle, oder nur ausgewählte?
