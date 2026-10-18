# Geplante Aktionen (Einschlaf-Timer): Spezifikation

Stand 2026-10-18. Entworfen, nicht gebaut. Beschlossen mit dem Nutzer: Der Zustand und der Scheduler liegen in
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

Beides bleibt nebeneinander. „Welche Timer habe ich?“ zeigt beide Arten zusammen.

## Die Automation

Eine **einmalige Automation** ist eine gewöhnliche HA-Automation mit dem Label **„Mira: einmalig“**
(`mira_once`, Beschreibung „Läuft einmal und räumt sich dann auf“). Sie trägt **nicht** das Label „Mira“ der
[Aufgaben](aufgaben.md), damit sie dort nicht in der Aufgabenliste erscheint.

```yaml
alias: "Schlaf-Timer Wohnzimmer"
description: "Mira: Schlaf-Timer, Anna, Wohnzimmer"
variables:                      # was die Integration beim Aufräumen und Mira beim Auflisten liest
  mira_due: "2026-10-18T22:35:00+02:00"
  mira_owner: anna
  mira_area: wohnzimmer
  mira_kind: sleep
triggers:
  - trigger: time
    at: "22:35:00"
  - trigger: homeassistant      # war HA zur Fälligkeit aus: jetzt nachholen
    event: start
conditions:                     # frühestens zur Fälligkeit, nur einmal, nicht ewig später
  - condition: template
    value_template: >-
      {{ now() >= as_datetime('2026-10-18T22:35:00+02:00')
         and now() < as_datetime('2026-10-18T22:35:00+02:00') + timedelta(hours=12)
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
- **Bedingung: frühestens zur Fälligkeit und nur einmal.** `now() >= mira_due` und noch nie ausgelöst
  (`last_triggered` ist leer). Ist HA zur Fälligkeit aus, löst der Start von HA die Automation aus, und sie
  wird nachgeholt: sonst brennt das Licht die ganze Nacht. Eine **Obergrenze** (zwölf Stunden nach der
  Fälligkeit) verhindert, dass ein Timer von gestern Abend heute Mittag etwas ausschaltet; danach verfällt er.
- **Fälligkeit und Besitzer** stehen in `variables`. Das ist strukturiert, überlebt Änderungen im Editor von HA und
  braucht keinen eigenen Speicher.
- **„Schon gelaufen?“** liest die Integration aus der Automation selbst: `last_triggered` und die Zahl der laufenden
  Durchläufe (`current`). Gelaufen und noch am Ausführen (etwa beim Ausblenden) ist nicht fertig.

## Aufräumen durch die Integration

Die Integration verwaltet die Automationen von Mira schon (`automations.py`). Neu: eine Kehrrunde, beim Start und
danach jede Minute.

Für jede Automation mit dem Label `mira_once`, mit `mira_due` aus den Variablen:

1. **Gelaufen und fertig** (`last_triggered` gesetzt, keine laufenden Durchläufe): löschen.
2. **Verfallen** (mehr als zwölf Stunden nach der Fälligkeit und nie gelaufen, etwa weil jemand die Automation
   abgeschaltet hat): löschen und den Besitzer benachrichtigen („Der Schlaf-Timer um 22:35 ist nicht
   ausgeführt worden“), als Nachricht über die Agenten-API.
3. **Fällig, nicht gelaufen und nicht verfallen:** nichts tun, die Automation holt es selbst nach (Start von HA).
4. **Noch nicht fällig:** nichts tun.

Das Nachholen steckt damit in der Automation selbst und nicht in der Kehrrunde. Die Kehrrunde räumt nur auf.

Ändert jemand `mira_due` im Editor, gilt der neue Wert. Fehlt die Variable, ist es keine Mira-Automation mehr, und
die Kehrrunde lässt sie in Ruhe.

## Anlegen, Ändern, Abbrechen

Es läuft über die Integration (Websocket-Befehle, wie bei Aufgaben):

- **`once_save`:** prüft (`check_actions`, im Zweifel ablehnend), setzt Label und Variablen, schreibt und lädt die
  Automation. Grenzen: höchstens sieben Tage in die Zukunft und höchstens zehn offene je Person.
- **Kritische Aktionen** (Schlösser, Alarmanlage, Garagentor, Entitäten mit dem Label „Mira: nachfragen“) sind in
  geplanten Aktionen **verboten**: Eine Bestätigung von heute Abend soll nachts um drei nichts aufmachen.
- **`once_list`, `once_delete`:** wie bei Aufgaben, nach Label und Variablen gefiltert.
- **Verlängern** schreibt `at`, die Datumsbedingung und `mira_due` neu.

## Werkzeuge im Agenten

Zuerst nur der Fall, den man wirklich braucht (kein Modell schreibt Aktionen):

- **`sleep_timer(minutes, room?, lights_off?)`:** Mira baut die Aktionen selbst: die Player des Raums stoppen
  (die freigegebenen Player im Bereich, wie bei `radio_play`), Licht im Raum aus. Raum ist der des Satelliten.
- **`timer_list`:** die einmaligen Automationen der Person und, aus Miras Datenbank, ihre Erinnerungen. Mit „wie
  lange noch“.
- **`timer_extend(id?, add_minutes)`, `timer_cancel(id?)`:** ohne Kennung gilt der Timer der Person im Raum; gibt es
  mehrere, fragt Mira nach.

Später, nach dem ersten Fall: ein allgemeines `action_schedule` mit Aktionen, die das Modell aus den freigegebenen
Entitäten wählt („Mach in einer Stunde das Licht aus“). Dieselbe Prüfung, dieselben Grenzen.

## Offen

- **Start von HA:** Beim Nachholen sind Player und Lampen eventuell noch nicht erreichbar. Die Aktionen warten
  deshalb kurz (bis zu zwei Minuten) auf die Zielgeräte und machen dann weiter, auch wenn eines fehlt.
- **Ausblenden:** Lautstärke in Schritten senken (mehrere Aktionen mit Warten). Erst nach der einfachen Version.
- **Das Hörbuch:** Beim Stoppen soll die Stelle bleiben. Das ist Sache von Music Assistant (siehe Skill-Ideen), nicht
  des Timers.
- **Erinnerungen später auch in HA?** Möglich, aber nicht nötig, solange sie funktionieren. Verpasste Erinnerungen
  bei laufendem HA und ausgeschaltetem Mira holt Mira heute vermutlich nicht nach; geprüft ist das nicht.
- **Ohne Integration** gibt es keine geplanten Aktionen, wie bei den Aufgaben.
