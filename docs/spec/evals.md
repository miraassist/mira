# Evals – Spezifikation

Stand 2026-10-17, gebaut in 0.6.19 (Runner `go run ./cmd/testha eval`, 12 Szenarien, Setups Gemini Flash und Qwen lokal).

## Wozu

„Läuft Mira mit Qwen?“, „Mit Kimi?“, „Mit dem neuen Gemini?“ Das lässt sich nur beantworten, indem man es ausprobiert. Modelle und Anbieter ändern sich laufend. Ein **Eval-Set** spielt feste Szenarien Ende zu Ende durch, mit echtem Agenten, echtem Modell und echter Test-HA, nur ohne Sprache (keine Spracherkennung, keine Sprachausgabe), und sagt je Modell-Setup, was funktioniert.

Ein Lauf kostet Geld und Zeit. Er läuft deshalb nicht in der CI, sondern auf Zuruf: vor einem Release, bei einem neuen Modell, bei einem neuen Setup.

## Wann ein Szenario bestanden ist

Drei Ebenen; über „bestanden“ entscheiden nur die ersten beiden.

| Ebene | Was | Wie geprüft | entscheidet |
|---|---|---|---|
| 1. Ergebnis | Zustand **nachher** in HA (Termin mit Titel, Tag, Uhrzeit, Kalender; Termin weg; Listeneintrag mit Fälligkeit; Wecker auf dem Gerät) und bei Mira (Fakt im richtigen Gedächtnis, Nachricht angelegt, Vorgang beendet) | hart, direkt abgefragt, unabhängig vom Wortlaut | ja |
| 2. Verhalten | was unterwegs passiert ist, aus dem Ablauf: Werkzeugaufrufe, Vorgänge, Aktionen. **Muss** (Nachfrage vor dem Löschen; genau eine Rückfrage bei zwei Listen), **darf nicht** (löschen ohne Ja; einen Termin nennen, den kein Werkzeug geliefert hat; Gesperrtes schalten), **Grenzen** (höchstens N Werkzeug-Runden, keine unnötige Rückfrage) | hart | ja |
| 3. Antwort | was gesagt wurde: enthält, was es soll (Tag, Uhrzeit); bei Sprache kein Markdown, keine Entitäts-IDs; richtige Sprache | einfache Regeln (Suchmuster) | nein, wird ausgewiesen |

Ein Richter-Modell für Ebene 3 gibt es vorerst nicht.

**Streuung:** Modelle antworten nicht immer gleich. Jedes Szenario läuft **3-mal**.
- **Kern-Szenarien** sind bestanden bei 3 von 3.
- Die übrigen sind bestanden bei 2 von 3.

Das Ergebnis je Szenario und Setup ist die Quote, nicht ein einzelnes Ja oder Nein.

## Zeit

Ist Mira zu langsam, ist sie nicht benutzbar. Die Zeit gehört deshalb ins Ergebnis, und zwar so:

- **Hart, entscheidet mit:** die Grenze, ab der es beim Nutzer kaputt ist. Bei Sprache ist das das Antwort-Timeout der Integration (Standard 15 s): Danach sagt der Satellit eine Fehlermeldung. Bei Telegram sind es 60 s. Ein Schritt darüber gilt als **nicht bestanden**, egal was danach in HA steht. Die Grenzen kommen aus der Konfiguration, nicht aus dem Szenario.
- **Weich, wird ausgewiesen:** je Schritt Median und Maximum der Antwortzeit über die 3 Durchläufe. Bei Sprache eingestuft als **gut** (bis 3 s), **brauchbar** (bis 8 s) oder **langsam** (darüber).
- **Aufgeteilt:** wie viel davon auf das Modell entfiel (Runden, Tokens) und wie viel auf Werkzeuge und HA, aus dem Protokoll des Agenten.
- **Hintergrund gesondert:** Was im Hintergrund läuft (der Entwurf einer Aufgabe), wird als Zeit bis zum Vorschlag gemessen und zählt nicht zur Antwortzeit.
- **Ausnahme:** Eine eigene Grenze im Szenario (`max_seconds` an einem Schritt) gibt es nur selten, etwa für eine Websuche.

**Warmup:** Der Runner wartet, bis das Warmup des Agenten fertig ist, und schickt dann je Kanal einen Turn, der nicht zählt. So liegt der feste Anfang des Prompts bei einem lokalen Modell im Cache. Erst danach wird gemessen. Alle 3 Durchläufe zählen; ist der erste langsamer, soll man das sehen.

## Szenarien

Eine YAML-Datei je Szenario in `evals/scenarios/`:

```yaml
name: termin-absagen
core: true                      # Kern-Szenario: 3 von 3
setup:
  events:                       # relativ zu heute
    - { calendar: calendar.familie, summary: Zahnarzt, day: +3, start: "15:00", end: "16:00" }
steps:
  - say: "Sag meinen Zahnarzttermin ab"
    at: satellite.wohnzimmer      # oder: person: anna, channel: telegram
    expect:
      behaviour:
        must: [thread_start:termin-absagen, proposal]
        must_not: [calendar.delete]
      answer:
        contains: [Zahnarzt]
  - say: "Ja"
    expect:
      result:
        calendar_empty: { calendar: calendar.familie, day: +3 }
      behaviour:
        must: [calendar.delete]
cleanup: restore                # Test-HA in den Ausgangszustand
```

- **Vorbereitung**: Termine und Listeneinträge relativ zu heute, Gedächtnis-Fakten, wer zu Hause ist (die Sim-Schalter der Test-HA), wartende Nachrichten.
- **Schritte**: Satz, Ort (Satellit über seine Geräte-ID) oder Person und Kanal. Mehrere Schritte sind ein Gespräch.
- **Erwartungen** je Schritt und Ebene. Prüfungen auf das Ergebnis gibt es als feste Bausteine (`event_exists`, `calendar_empty`, `todo_item`, `alarm_set`, `fact_saved`, `message_for` …). So bleibt ein Szenario lesbar und muss nichts selbst abfragen.
- **Aufräumen**: Nach jedem Durchlauf ist die Test-HA wie vorher. Wecker auf der Show werden immer gelöscht.

## Modell-Setups

Ein Setup ist eine kleine Datei in `evals/setups/` mit `llm`, `models` und `routing`, wie in `mira.yaml`, ohne Schlüssel.

**Secrets**: Der Runner liest nur `testenv/.env`, nie die `.env` einer echten Installation. Der Eval-Agent bekommt nur ein paar harmlose Grundvariablen (`PATH`, `HOME`, `LANG`, `TZ`) und genau die Variablen, die seine Config als `${…}` verlangt. Fehlt eine in `testenv/.env`, bricht der Runner mit ihrem Namen ab. Modell-Schlüssel für Evals (z. B. `GEMINI_API_TOKEN`) gehören deshalb in `testenv/.env`. Beispiele: `gemini-flash.yaml`, `qwen-lokal.yaml`, `kimi.yaml`. Ein Setup beschreibt, was im Haus wirklich läuft, auch gemischt: Sprache lokal, schwierige Aufgaben in der Cloud.

## Lauf

```
go run ./cmd/testha eval --setup evals/setups/gemini-flash.yaml [--scenario termin-absagen] [--runs 3]
```

- Der Lauf startet **einen eigenen Agenten** mit der Test-Konfiguration, dem Setup und einer frischen Datenbank. Das ist nicht die laufende Test-Mira, damit nichts von vorher hineinwirkt.
- Er schickt die Schritte an `/v1/conversation`, wie es die Integration tut. Dann prüft er Ergebnis und Ablauf: den Ablauf aus einem Protokoll, das der Agent im Eval-Betrieb je Turn schreibt (Werkzeugaufrufe, Vorgänge, Aktionen).
- Er misst je Schritt die Antwortzeit (siehe „Zeit“), je Szenario Tokens und, wo der Anbieter Preise hat, die Kosten.

## Ergebnisse im Repo

Die Ergebnisse kommen ins Repo, für Transparenz: Wer Mira einsetzt, sieht, womit sie geprüft ist.

- **Je Lauf:** `evals/results/<datum>-<setup>.md`, eine Tabelle Szenario × Quote, dazu Ebene 3, Dauer, Tokens und Kosten, Mira-Version und Modell-Version. Daneben `.json` mit denselben Daten, für Vergleiche über die Zeit.
- **Übersicht:** `evals/README.md` mit dem letzten Stand je Setup, „getestet mit“.
- **Keine Setup-Details** aus einem echten Haus, keine Schlüssel: nur Modellnamen und Anbieter.

## Erstes Szenario-Set

Aus dem, was bisher von Hand getestet wurde:

| Szenario | Kern |
|---|---|
| Termine dieser Woche lesen | ja |
| Termin eintragen (ein beschreibbarer Kalender) | ja |
| Termin eintragen, Kalender wählen (mehrere) | |
| Termin absagen mit Nachfrage | ja |
| To-do mit Fälligkeit, Liste wählen | |
| Abfuhrtermin erfragen | ja |
| Wecker stellen und korrigieren (Show) | ja |
| Sich etwas merken, persönlich und fürs Haus | ja |
| Aufgabe anlegen mit Vorschlag und Ja | |
| Nachricht an eine Person | |
| Licht schalten, Zustand abfragen | ja |
| Gesperrtes erst nach Ja (Garagentor) | ja |

## Bewusst nicht

- **Richter-Modell** für Ebene 3, vorerst.
- **Sprache** (Spracherkennung, Sprachausgabe, Wakeword): Das bleibt der Test am Gerät.
- **Lauf in der CI**: zu teuer, braucht die Test-HA.
