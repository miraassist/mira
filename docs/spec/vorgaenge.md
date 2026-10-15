# Vorgänge und Zieldefinitionen – Spezifikation

Stand 2026-10-15, gebaut (Agent 0.4.0). Gilt für [Aufgaben](aufgaben.md) und kommende Anliegen
wie Kalender oder Nachrichten an andere ([Benutzer](benutzer.md)).

## Warum

Ein Anliegen läuft oft über mehrere kurze Unterhaltungen. Ein Beispiel:

1. Du: „Achte auf das Licht im Schlafzimmer.“ Mira: „Ich plane das.“
2. Mira, als Ansage: „Ab wann ist es ungewöhnlich?“ Du: „Zwischen 1 und 5.“
3. Mira, als Ansage: „Mein Vorschlag … Soll ich so vorgehen?“ Du: „Ja.“

Home Assistant macht daraus drei getrennte Unterhaltungen. Bisher sah die
Antwort jeweils nur die angesagte Meldung, nicht den Verlauf davor. Live ist
dabei das Licht verloren gegangen: Aus „Licht zu ungewöhnlicher Zeit“ wurde
„etwas Ungewöhnliches“.

## Begriffe

- **Turn:** eine kurze Unterhaltung, wie Home Assistant sie führt. Zum
  Beispiel ein Wake-Word-Gespräch oder die Antwort nach einer Ansage. Er hat
  einen eigenen Verlauf. Im Code heißt er weiter `session`.
- **Vorgang** (im Code `thread`): ein Anliegen, das über mehrere Turns läuft. Er hat
  - ein **Ziel** (aus einer Zieldefinition),
  - einen **Ort** (Raum bei Sprache, Chat bei Telegram),
  - einen **Zustand**,
  - **Felder**, die das Ziel braucht,
  - ein kurzes **Protokoll** (was gefragt, geantwortet, vorgeschlagen wurde),
  - optional einen **Inhalt** des Ziels, zum Beispiel den Entwurf einer
    Automation.
- **Zieldefinition:** Sie steht in einem Skill (`metadata.mira.target`) und
  legt fest, welche Felder ein Vorgang füllen muss, wie sie geprüft werden
  und womit sie endet.

## Lebenszyklus

```
eröffnet ─► sammelt ─► wartet auf dich ─┬─► sammelt …        (Antwort)
                │                        ├─► abgebrochen      (thread_cancel)
                │                        └─► abgelaufen       (Timeout, Standard 15 min)
                ▼ (alle Felder da)
            arbeitet ─► schlägt vor ─► wartet auf Ja ─┬─► erledigt (thread_confirm, Abschluss gelungen)
                                                      ├─► sammelt  (Änderungswunsch)
                                                      ├─► abgebrochen
                                                      └─► abgelaufen
```

1. **Eröffnen.** Das Modell kennt die Zieldefinitionen wie Skills: Name und
   eine Zeile Beschreibung im festen Teil des Prompts. Erkennt es ein
   passendes Anliegen, ruft es `thread_start(target, request)` auf. Mira
   legt den Vorgang an und gibt die Anleitung des Ziels und die Felder
   zurück.
2. **Sammeln.** Das Modell trägt mit `thread_fill(fields)` ein, was es weiß.
   - Der Code prüft jedes Feld und antwortet mit dem Stand: was fehlt, was
     falsch ist, oder dass alles vollständig ist.
   - Fehlt etwas, fragt das Modell nach, und der Vorgang wartet auf dich.
3. **Arbeiten.** Sobald alle Felder da sind, startet der Code den
   Arbeitsschritt des Ziels. Braucht das Ziel ein Ja, wird daraus ein
   Vorschlag. Vorschlag und Rückfrage sind Meldungen **des Vorgangs**.
4. **Abschließen.** Es gibt drei Wege:
   - Auf das Ja ruft das Modell `thread_confirm` auf. Der Abschluss läuft
     (die Automation wird angelegt), und der Vorgang ist erledigt.
   - Ein Ziel ohne Bestätigung (etwa „merken“) wird direkt abgeschlossen,
     sobald es vollständig ist.
   - Ein Änderungswunsch („ohne die Tür“) führt mit `thread_fill` zurück
     ins Sammeln.
5. **Ende.** Ein Vorgang ist erledigt, abgebrochen (du sagst es, das
   Modell ruft `thread_cancel` auf) oder abgelaufen (Timeout ohne
   Antwort). Seine offenen Meldungen werden dabei zurückgezogen. Ein alter
   Vorschlag landet also nie später im „Übrigens“.

## Zuordnung: Welcher Turn gehört zu welchem Vorgang?

1. **Fest verknüpft (Code).** Eine Antwort direkt nach einer Ansage gehört
   zum Vorgang dieser Meldung. Weitere Turns derselben HA-Unterhaltung
   gehören zu demselben Vorgang.
2. **Wartend am selben Ort (Modell).** Wartet an diesem Ort ein Vorgang
   auf dich, sieht das Modell ihn im Kontext und entscheidet: weiterführen
   (`thread_fill`, `thread_confirm`), abbrechen (`thread_cancel`) oder
   ein anderes Thema. Das sind keine festen Phrasen.
3. **Sonst** gehört der Turn zu keinem Vorgang.

Es gibt keine Fortsetzung über Kanäle hinweg: Ein am Satelliten begonnener
Vorgang lässt sich nicht per Telegram weiterführen.

Im Kontext (Schicht 7, nicht im gecachten Teil) steht für den zugeordneten
oder wartenden Vorgang:
- das Ziel und der Wunsch in deinen Worten,
- die Felder mit ihrem Stand,
- das Protokoll,
- worauf der Vorgang wartet.

## Zieldefinition

Die Zieldefinition steht im Skill-Format, deklarativ und ohne eigenen Code:

```yaml
metadata:
  mira:
    target:
      timeout: 15m                 # Standard 15m
      confirm: true                # vor dem Abschluss ein Ja
      fields:                      # Liste, damit die Reihenfolge feststeht
        - name: watch
          description: Was beobachtet wird (Entitäten aus dem Haus, id oder Name/Alias)
          check: entities          # Prüfung im Kern; löst Namen zu ids auf
        - name: measure
          description: Ab wann es zählt. "- " mit Grund, wenn der Wunsch nichts Relatives enthält.
        - name: reaction
          description: fest melden, beurteilen lassen oder etwas schalten
          enum: [notice, assess, switch]
      finish: tasks.automation     # Abschluss im Kern
```

Regeln:

- **Jedes Feld muss beantwortet sein.** Ein Wert, der mit „-“ beginnt
  („- weil der Wunsch nichts Relatives enthält“), heißt „bewusst nicht
  nötig“ und zählt als Antwort. Das Zeichen ist sprachunabhängig. Das Modell legt sich ausdrücklich fest, und der Code sieht, dass
  eine Lücke bewusst ist. So lässt sich „Pflicht nur, wenn relativ“
  abbilden, ohne dass der Code Sprache verstehen muss.
- **Prüfungen (`check`) und Abschlüsse (`finish`) sind Bausteine im
  Kern.** Skills führen keinen eigenen Code aus.

Bausteine zum Start:

| Baustein | Art | Tut |
|---|---|---|
| `entities` | Prüfung | Jede genannte Entität muss im Haus existieren. |
| `tasks.automation` | Abschluss mit Arbeitsschritt | Aus Wunsch und Feldern eine Automation entwerfen, von HA prüfen lassen und den Vorschlag ansagen. Auf das Ja wird sie gespeichert. |
| `memory.save` | Abschluss | Den Fakt im Hausgedächtnis speichern. |

## Werkzeuge

Diese Werkzeuge gibt es immer:

| Werkzeug | Wirkung |
|---|---|
| `thread_start(target, request)` | eröffnet einen Vorgang am Ort des Turns und liefert Anleitung und Felder |
| `thread_fill(fields)` | trägt Felder ein, liefert den Stand und startet den Arbeitsschritt, wenn alles da ist |
| `thread_confirm()` | schließt einen Vorgang ab, der auf ein Ja wartet |
| `thread_cancel()` | bricht den Vorgang ab |

Die Werkzeuge wirken immer auf den Vorgang des Turns. Hat der Turn keinen
Vorgang, gilt der wartende am Ort.

## Erste Ziele

- **aufgabe:** ersetzt den bisherigen Weg (`task_create` und `task_confirm`).
  - Felder: `watch`, `when`, `measure`, `reaction`.
  - Abschluss: `tasks.automation`, mit Bestätigung.
- **merken:** Beispiel: „Merk dir mal was.“ → „Was denn?“ → „… ist notiert.“
  - Feld: `fact`.
  - Abschluss: `memory.save`, ohne Bestätigung.

Später:
- **nachricht:** „Erinnere Anna daran, dass …“ → „Soll ich es gleich
  schicken?“ Kommt mit dem Benutzer-Konzept (Absender, Empfänger).
- **kalender:** „Such den Termin raus“ → „Der zum Friseur.“ → „Nein, der
  nächste.“ Kommt mit dem Kalender-Skill.

## Datenmodell

- **`sessions`** (Turns, wie bisher): neue Spalte `thread_id` mit dem zugeordneten Vorgang.
- **`threads`** (neu):
  - `id`, `target`, `place`, `state`, `request`,
  - `fields` (JSON), `log` (JSON), `content` (JSON),
  - `created_at`, `updated_at`, `expires_at`.
- **`notices`:** neue Spalte `thread_id`. Endet ein Vorgang, werden seine
  offenen Meldungen zurückgezogen.
- Die Tabelle `tasks` (Vorschläge) entfällt. Der Entwurf ist jetzt der
  Inhalt des Vorgangs.
