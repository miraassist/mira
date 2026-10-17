# Kalender und To-do-Listen – Spezifikation

Stand 2026-10-17, gebaut in 0.6.19. Baut auf [Skill-Aktionen](skill-aktionen.md) auf. Wie Daten dargestellt werden (Stufen, Zeitachse): [Kontext](kontext.md), Abschnitt 2.

## Grundsatz

Kalender und To-do-Listen sind Konzepte von Home Assistant; Mira nutzt sie, statt eigene zu führen. Mehrere Kalender sind der Normalfall (Familie, eigener, Google, Feiertage, Abfuhr …).

- **Was Mira sieht**, regelt HAs Freigabe für Assist, wie bei allen Entitäten: nur freigegebene Kalender und Listen.
- **Beschreibbar** ist ein Kalender, wenn HA es angibt (`supported_features`: Eintragen, Löschen). Feiertage oder Kalenderwochen sind nur zum Lesen da; Mira bietet sie zum Eintragen nicht an.

## Lesen: eigenes Werkzeug `calendar_events`

HAs Werkzeug `calendar_get_events` liefert alle Termine eines Zeitraums roh, mit langen Beschreibungen (Einladungen, Links). Mira nutzt es nicht. Sie hat ein eigenes Werkzeug und bestimmt selbst, wie das Modell den Kalender sieht.

| Parameter | Bedeutung |
|---|---|
| `from`, `to` | Zeitraum; Standard: heute bis in 7 Tagen |
| `calendar` | ein Kalender; Standard: alle freigegebenen |
| `query` | Suchtext im Titel, z. B. „Arzt“ |
| `details` | auch Beschreibungen; Standard nein |

Darstellung, knapp:

```
Mo 20.10.
- [t1] 10:00–10:30 Arzt Dr. Berger · Praxis Dr. Berger, Hauptstraße 12, … (Familie)
Fr 24.10.
- [t2] ganztägig Geburtstag Oma (Familie)
```

- Nach Tag gruppiert, mit Wochentag; ganztägige Termine als „ganztägig“.
- Der Ort wird gekürzt. Die Beschreibung kommt nur mit `details` und auch dann gekürzt und ohne Links.
- Höchstens 40 Termine, dann „und N weitere“.
- Jeder Termin hat eine kurze Kennung (`t1`, `t2` …) zum Absagen. Die Termin-ID von HA (`uid`, bei Serien `recurrence_id`) behält der Code für sich; das Modell sieht sie nicht.
- Quelle ist HAs REST-API `/api/calendars/<entität>`. Sie liefert die Termin-ID, Miras Benutzer darf sie lesen.

**Für Aufgaben** kommt dieselbe Darstellung als Ansicht `view: upcoming` für Kalender-Entitäten in `mira.assess` und `mira.decide` hinzu (Standard: die nächsten 24 Stunden), z. B. für „Sag mir abends, was morgen ansteht“.

## Eintragen: Skill `termin`

Das ist der Haupt-Anwendungsfall: „Trag mir Freitag um 15 Uhr Zahnarzt ein.“ Er ist eine Skill-Aktion ohne eigenen Code.

- **Felder:**
  - Titel;
  - Beginn (Datum mit Uhrzeit, oder nur Datum für ganztägig);
  - Ende oder Dauer, optional (Standard 1 Stunde);
  - Ort, optional;
  - Kalender.
- **Abschluss:** `calendar.create_event`.
- **Kein Nachfragen**, ob es passt. Mira sagt, was sie eingetragen hat: Titel, Tag, Uhrzeit, Kalender.
- **Korrektur** („Nein, 16 Uhr“): Mira sagt den eben eingetragenen Termin ab, wie unten, und trägt neu ein. HA gibt beim Eintragen keine Termin-ID zurück; Mira findet ihn über `calendar_events` wieder.

## Welcher Kalender: fragen und merken

Eine Einstellung dafür gibt es nicht.
- Steht im Gedächtnis der Person ein Standard („Mein Kalender ist Familie“), nimmt Mira ihn.
- Gibt es nur einen beschreibbaren Kalender, nimmt sie den.
- Sonst fragt sie: „In welchen Kalender – Familie oder Anna?“ Nach der Antwort bietet sie an, sich das als Standard zu merken.
- Nennt der Nutzer einen Kalender ausdrücklich („in den Familienkalender“), gilt der.

## Absagen: Skill `termin-absagen`

„Sag meinen Termin morgen beim Arzt ab.“

1. Mira sucht mit `calendar_events` (Zeitraum und Suchtext aus dem Satz).
2. Gibt es genau einen Treffer, wird der abgesagt. Bei mehreren fragt Mira, welcher; bei keinem sagt sie es.
3. **Nachfragen vor dem Löschen** („Arzt Dr. Berger, Montag 10 Uhr – absagen?“): Löschen lässt sich nicht zurückholen (`confirm: true`, beschlossen).
4. Abschluss ist ein Baustein im Kern, `calendar.delete`. HA hat dafür keine Aktion, nur den WebSocket-Befehl `calendar/event/delete` mit Termin-ID. Miras Benutzer darf ihn aufrufen (geprüft).
5. **Bei Serien** wird nur dieser eine Termin abgesagt, außer der Nutzer sagt ausdrücklich „alle“.

## To-do-Listen

- **Lesen, Hinzufügen, Abhaken, Entfernen** über HAs Assist-Werkzeuge (`todo__get_items`, `HassListAddItem`, `HassListCompleteItem`, `HassListRemoveItem`). Die gibt es schon.
- **Neu ist der Skill `todo`** für Einträge mit **Fälligkeit oder Beschreibung** („Setz Batterien auf die Liste, bis Freitag“). Das können HAs Assist-Werkzeuge nicht.
  - Felder: Eintrag, Liste, fällig (optional), Beschreibung (optional).
  - Abschluss: `todo.add_item`.
  - Ohne Fälligkeit und Beschreibung reicht HAs `HassListAddItem`.
- **Welche Liste:** wie beim Kalender. Gibt es nur eine, nimmt Mira die; sonst steht der Standard im Gedächtnis, oder sie fragt.

## Abfall-Skill

Der Skill `abfall` liest seine Termine künftig über `calendar_events` aus einem Kalender in HA (z. B. Abfuhrtermine) statt selbst eine iCal-Datei zu holen. Miras eigene Kalender-Logik (`internal/ical`) entfällt. Statt `calendar_url` nennt der Skill die Kalender-Entität.

## Test-Umgebung

Die Test-HA hat einen lokalen Kalender „Familie“ (`calendar.familie`, beschreibbar) und eine lokale Liste „Einkauf“ (`todo.einkauf`, mit Fälligkeit und Beschreibung). Beide sind für Assist freigegeben und haben Beispieleinträge, darunter ein Termin mit langer Beschreibung und Link. Beim Bau kommt das ins Seed-Programm (`cmd/testha`), damit es sich wiederherstellen lässt.

## Bewusst nicht

- **HAs `calendar_get_events`**: zu viel und zu roh.
- **Termine ändern**: vorerst absagen und neu eintragen.
- **Eine Einstellung für Standard-Kalender oder -Liste**: Das steht im Gedächtnis.
