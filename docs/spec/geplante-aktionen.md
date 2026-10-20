# Geplante Aktionen (Einschlaf-Timer): Spezifikation

Stand 2026-10-18, gebaut in 0.6.45. Beschlossen mit dem Nutzer: Der Zustand und der Scheduler liegen in
**Home Assistant**; Erinnerungen bleiben vorerst bei Mira. Einmalige Automationen tragen ein eigenes **Label**; das
**Aufräumen übernimmt die Integration**.

## Ziel

„Schlaf-Timer 30 Minuten.“ – „Schalte in einer Stunde das Licht aus.“ – „Noch zehn Minuten.“ – „Doch abbrechen.“

Mira soll zu einer Zeit **etwas tun**, nicht nur etwas sagen: die Wiedergabe stoppen (Hörbuch, Radio, Musik), auf
Wunsch langsam leiser, und das Licht im Raum ausschalten. Das gleiche tragen Szenen mit Verzögerung („Heimkino in
fünf Minuten“). Siehe [Skill-Ideen](skill-ideen.md).

## Abgrenzung zu Erinnerungen

| | Erinnerung / Timer („erinnere mich in einer Minute an die Wäsche“) | Geplante Aktion (dieses Papier) |
|---|---|---|
| Was zur Zeit passiert | Mira **sagt** etwas | HA **führt Aktionen aus** |
| Wo es lebt | Miras Datenbank (`reminder_set`, geplante Nachricht) | Automation in Home Assistant |
| Wer löst aus | Der Notifier von Mira | Home Assistant |
| Läuft, wenn die andere Seite aus ist | nein (Mira muss laufen) | ja (Mira muss nicht laufen) |

Beides bleibt nebeneinander. „Welche Timer habe ich?“ fragt beide Listen ab.

## Countdown-Timer sind etwas Drittes

„Timer auf zehn Minuten für die Nudeln“ baut Mira **nicht** nach. Das sind die Timer von Home Assistant
(`HassStartTimer`, `HassCancelTimer`, `HassTimerStatus`, `HassIncreaseTimer`, `HassDecreaseTimer`, `HassPauseTimer`,
`HassUnpauseTimer`): Der Satellit, der sie bekommt, zählt herunter, zeigt den Rest und klingelt (TECHO5 tut das auf
Show wie auf Echo; der Show hat zusätzlich den Bildschirm). Mira nutzt diese Werkzeuge selbst, so wie HA sie seinen
eigenen Konversations-Agenten gibt.

Dass sie bei Mira ankommen, liegt an einer Zeile: HAs MCP-Server baut die Werkzeugliste **für ein Gerät**, wenn die
Anfrage dessen Kennung in den Metadaten trägt (`_meta` mit `io.home-assistant/device_id`). Ohne Gerät fehlen die
Timer-Werkzeuge (26 statt 33 gemessen), und HA bevorzugt den Raum des Satelliten bei Befehlen ohne Raum nicht. Mira
schickt die Kennung des Satelliten daher bei **jedem** Aufruf eines HA-Werkzeugs im Sprachgespräch mit
(`device_context`, für HAs Assist-Server eingeschaltet) und fragt die Liste einmal ohne und einmal als jeder Satellit ab
und vereinigt sie, damit ihr nichts fehlt, was ein Satellit angeboten bekommt. Die Liste wird alle fünf Minuten und bei
jeder Änderung der Satelliten neu geholt, denn HA meldet nicht, dass ein Satellit mit Timern zurückkam.

Regel dahinter: **Was HA anbietet, bauen wir nicht nach**, auch nicht, um Token zu sparen. Und wir lassen Werkzeuge nicht
dadurch verschwinden, dass der Kontext (hier das Gerät) nicht mitkommt. Wer das je anders entscheidet, tut es bewusst.

Nichts wird bei Mira gespeichert. Ohne Satelliten (Telegram) kennt HA keine Timer, und Mira sagt das ehrlich. Die Timer
von Home Assistant liegen im Speicher von Home Assistant und überstehen dessen Neustart nicht.

Damit sind es drei Dinge: der Countdown-Timer (HA, am Satelliten), die Erinnerung (Mira sagt einen Text) und der
Schlaf-Timer (HA-Automation, tut etwas).

## Die Automation

Eine **einmalige Automation** ist eine gewöhnliche HA-Automation mit dem Label **„Mira: einmalig“**
(`mira_once`, Beschreibung „Läuft einmal und räumt sich dann auf“). Sie trägt **nicht** das Label „Mira“ der
[Aufgaben](aufgaben.md), damit sie dort nicht in der Aufgabenliste erscheint.

```yaml
alias: "Schlaf-Timer Wohnzimmer 837deda7"   # das Ende ist für jeden Timer anders, siehe unten
description: "Mira: Schlaf-Timer Wohnzimmer"
variables:                      # was die Integration beim Aufräumen und Mira beim Auflisten liest
  mira_title: "Schlaf-Timer Wohnzimmer"
  mira_due: "2026-10-18T22:35:00+02:00"
  mira_owner: anna
  mira_area: wohnzimmer
  mira_kind: sleep
triggers:
  - trigger: time
    at: "22:35:00"
  - trigger: homeassistant      # war HA zur Fälligkeit aus: jetzt nachholen
    event: start
conditions:                     # frühestens zur Fälligkeit, nur einmal
  - condition: template
    value_template: >-
      {{ now() >= as_datetime('2026-10-18T22:35:00+02:00') - timedelta(seconds=2)
         and this.attributes.last_triggered is none }}
actions:
  - action: media_player.volume_set     # Ausblenden: mehrere Schritte mit Warten
    ...
  - action: media_player.media_stop
    target: { entity_id: media_player.show_speaker }
  - action: light.turn_off
    target: { area_id: wohnzimmer }
mode: single
```

- **Auslöser:** eine Uhrzeit auf die Sekunde, dazu der Start von Home Assistant. Das reicht auch über Mitternacht
  und für Zeiten in mehr als 24 Stunden. Ein Vorlagen-Auslöser mit `now()` wäre nur auf die Minute genau.
- **Bedingung: frühestens zur Fälligkeit und nur einmal.** `now() >= mira_due` (zwei Sekunden Spiel für die Uhr)
  und noch nie ausgelöst (`last_triggered` ist leer). Ist HA zur Fälligkeit aus, löst der Start von HA die
  Automation aus, und sie wird nachgeholt: sonst brennt das Licht die ganze Nacht. Es gibt **keine Obergrenze**
  und keinen Verfall; was fällig ist und noch nicht lief, läuft, sobald HA wieder da ist.
- **Jeder Timer hat einen eigenen Namen.** HA macht die Entitäts-ID aus dem Namen und stellt den Zustand einer
  neuen Entität aus der letzten mit derselben ID wieder her, auch aus einer gelöschten. Ein zweiter „Schlaf-Timer
  Wohnzimmer“ hätte „schon gelaufen“ vom ersten geerbt und nie ausgelöst. Der Test gegen eine echte HA hat das
  gezeigt. Darum hängt der Name eine Kennung an (die letzten acht Zeichen der Automations-ID). Der lesbare Titel
  steht in `mira_title`.
- **Fälligkeit und Besitzer** stehen in `variables`. Das ist strukturiert, überlebt Änderungen im Editor von HA und
  braucht keinen eigenen Speicher.
- **„Schon gelaufen?“** liest die Integration aus der Automation selbst: `last_triggered` und die Zahl der laufenden
  Durchläufe (`current`). Gelaufen und noch am Ausführen (etwa beim Ausblenden) ist nicht fertig.

## Aufräumen durch die Integration

Die Integration verwaltet die Automationen von Mira schon (`automations.py`). Neu: eine Kehrrunde, beim Start und
danach jede Minute.

Für jede Automation mit dem Label `mira_once`:

1. **Gelaufen und fertig** (`last_triggered` gesetzt, keine laufenden Durchläufe): löschen.
2. **Alles andere:** nichts tun. Fällig und nicht gelaufen holt die Automation selbst nach (Start von HA),
   noch nicht Fälliges wartet. Wer einen Timer in HA abschaltet, behält ihn, bis er ihn selbst löscht.

Das Nachholen steckt damit in der Automation selbst und nicht in der Kehrrunde. Die Kehrrunde räumt nur auf.

Ändert jemand `mira_due` im Editor, ohne die Zeit des Auslösers anzupassen, gilt weiter die Zeit des Auslösers; der
Editor von HA ist dafür nicht gedacht. Verlängern und Abbrechen geschehen im Gespräch.

## Anlegen, Ändern, Abbrechen

Es läuft über die Integration (Websocket-Befehle, wie bei Aufgaben):

- **`once_save`:** prüft (`check_actions`, im Zweifel ablehnend), setzt Label und Variablen, schreibt und lädt die
  Automation. Grenze: **höchstens zehn offene Timer je Person und Raum**. Sonst keine, weder in der Zeit noch in der Dauer.
- **Kritische Aktionen** (Schlösser, Alarmanlage, Garagentor, Entitäten mit dem Label „Mira: nachfragen“) sind in
  geplanten Aktionen **verboten**: Eine Bestätigung von heute Abend soll nachts um drei nichts aufmachen.
- **`once_list`, `once_delete`:** wie bei Aufgaben, nach Label und Variablen gefiltert.
- **Verlängern** schreibt `at`, die Datumsbedingung und `mira_due` neu.

## Werkzeuge im Agenten

Zuerst nur der Fall, den man wirklich braucht (kein Modell schreibt Aktionen):

- **`sleep_timer(action, minutes?, room?, lights_off?, id?)`** (ein Werkzeug, `action`: `set` (Standard), `list`, `extend`, `cancel`):
  `set` ist der Schlaf-Timer. Mira baut die Aktionen selbst: die Player des Raums stoppen
  (die freigegebenen Player im Bereich, wie bei `radio_play`), Licht im Raum aus. Raum ist der des Satelliten.
  `list` zeigt die Schlaf-Timer der Person im Raum mit „wie lange noch“.
  `extend` und `cancel` brauchen keine Kennung, wenn die Person im Raum nur einen hat; gibt es mehrere, fragt Mira nach.

Später, nach dem ersten Fall: ein allgemeines `action_schedule` mit Aktionen, die das Modell aus den freigegebenen
Entitäten wählt („Mach in einer Stunde das Licht aus“). Dieselbe Prüfung, dieselben Grenzen.

## Offen

- **Ausblenden:** Lautstärke in Schritten senken (mehrere Aktionen mit Warten). Erst nach der einfachen Version.
- **Das Hörbuch:** Beim Stoppen soll die Stelle bleiben. Das ist Sache von Music Assistant (siehe Skill-Ideen), nicht
  des Timers.
- **Erinnerungen später auch in HA?** Möglich, aber nicht nötig, solange sie funktionieren. Verpasste Erinnerungen
  bei laufendem HA und ausgeschaltetem Mira holt Mira heute vermutlich nicht nach; geprüft ist das nicht.
- **Ohne Integration** gibt es keine geplanten Aktionen, wie bei den Aufgaben.
