# Mira in Home Assistant: dekorieren statt eigener Listen – Spezifikation

Stand 2026-10-16, gebaut in 0.5.9. Ergänzt [Benutzer](benutzer.md) und [Aufgaben](aufgaben.md).

## Grundsatz

Was es in Home Assistant schon gibt, legt Mira nicht noch einmal an. Sie hängt ihre Einstellungen an die Dinge in HA:

| Was Mira braucht | Wo es liegt |
|---|---|
| Personen | HAs Personen; Miras Einstellungen als Entity-Registry-Optionen `options["mira"]` an der Person ([Benutzer](benutzer.md)) |
| Räume | die **Satelliten**; Miras Einstellungen als `options["mira"]` am Satelliten, der Bereich ist der des Satelliten |
| Aufgaben | Automationen mit dem Label „Mira“ ([Aufgaben](aufgaben.md)) |
| „erst nach Bestätigung schalten“ | das Label „Mira: nachfragen“ an der Entität |
| was Mira sieht | HAs Freigabe für Assist, wie gehabt |

**Faustregel:** Ein Merkmal, das da ist oder nicht, ist ein **Label** (sichtbar und setzbar in HAs normaler Oberfläche, filterbar). Eine Einstellung mit Wert ist eine **Option an der Entität**. Eigene Listen nur für das, was es in HA nicht gibt (Ruhezeiten, Eskalation …).

Lesen und schreiben:
- **Geschrieben** wird über die Integration (Admin): Formulare unter Geräte & Dienste → Mira → **Konfigurieren**. Der Agent prüft jede Eingabe, bevor sie gespeichert wird.
- **Gelesen** werden Labels direkt vom Agenten: Sein HA-Benutzer (kein Admin) darf Entity- und Label-Registry lesen, und er bekommt Änderungen mit. Personen- und Satelliten-Einstellungen schickt die Integration bei jeder Änderung an den Agenten, wie bisher.
- Eine gelöschte Entität nimmt ihre Optionen mit; es bleibt nichts liegen.

## Räume sind Satelliten

Fast alles, was heute ein „Raum“ ist, ist eine Eigenschaft des Satelliten:

| Einstellung am Satelliten (`options["mira"]`) | Bedeutung |
|---|---|
| `presence` | **Liste** von Bewegungsmeldern; jeder davon löst „jemand kommt herein“ aus (z. B. Flur-PIR und Wohnzimmer-PIR für das Wohnzimmer-Echo) |
| `morning` | **beim Hereinkommen ansagen**: Merkt einer der Bewegungsmelder jemanden, sagt Mira von selbst an, was wartet (nach der Ruhezeit mit „Guten Morgen“) |
| `indicator` | die Anzeige, z. B. der LED-Ring des Echo: leuchtet, solange etwas wartet, unabhängig von Bewegungsmeldern. Satelliten mit Bildschirm brauchen keine Einstellung, Mira erkennt sie von selbst |

- Der **Bereich** ist der des Satelliten in HA (Entität, sonst ihr Gerät). Ein Satellit ohne Bereich kann nicht eingestellt werden.
- Satelliten sind `assist_satellite.*`, für Lautsprecher ohne Assist auch `media_player.*` (mit `notifier.tts_entity`).
- Vorerst **ein Satellit pro Bereich**: Ein zweiter im selben Bereich wird abgelehnt. Mehrere pro Bereich wären ein eigener Schritt (wer spricht dann?).
- Die Unter-Einträge „Raum“ der Integration entfallen. Bereiche selbst lassen sich in HA nicht dekorieren (die Bereichs-Registry hat keine Optionen); auch deshalb ist der Satellit der Anker.
- Ohne Integration bleibt alles in `mira.yaml` (`satellites`, `notifier.morning_trigger`, `indicators`); `presence` darf dort eine Entität oder eine Liste sein.

## Was Mira sieht: die Freigabe für Assist

Was Mira sehen darf, bestimmt HAs „Für Assist freigeben“. HAs eigene Werkzeuge über MCP halten das selbst ein. Miras eigene Teile sprechen HA aber direkt an und müssen dieselbe Grenze einhalten: Kalender lesen, die Prüfungen von Kalender und Liste, der Katalog für Aufgaben-Entwürfe, der eigene Strukturblock und der Wächter.

- **Die Integration liefert die Liste.** Bei jedem Abgleich (`PUT /v1/ui/ha`) schickt sie neben Personen und Satelliten `exposed`: die IDs aller Entitäten, die HA für Assist freigibt. Sie bestimmt das wie HA selbst (`async_should_expose`). Ändert sich eine Freigabe, schickt sie neu.
- **Der Agent merkt sich die Liste** dauerhaft und nimmt sie als maßgeblich.
- **Ohne diese Liste gilt nichts als freigegeben,** und Mira handelt nicht.
  - Der Agent fragt HA nicht selbst: Die Freigabeliste kann nur ein Admin lesen, und Miras Benutzer hat bewusst keine Admin-Rechte. Auch die Entitäts-Optionen und ein „alles“ als Ersatz gibt es nicht.
  - Der Hausplan ist ohne Liste leer, ebenso die Auswahl für Aufgaben, Kalender und Listen.
  - Ein Gespräch beantwortet Mira ohne Liste mit: „Ich warte noch auf die Verbindung mit Home Assistant …“ Sie ruft kein Werkzeug und fragt das Modell nicht. Sonst würde HAs MCP-Server schalten, was dort freigegeben ist, während der Wächter diese Namen im leeren Plan nicht kennt.
  - Das betrifft nur den Zeitraum vor dem ersten Abgleich der Integration. Danach merkt sich der Agent die Liste auch über Neustarts.
- **Warum nicht aus dem ableiten, was Mira sieht:** HAs Assist-Prompt und `GetLiveContext` nennen nur Namen, keine IDs, und lassen Kalender ganz weg. Eine Grenze für den Datenschutz darf nicht auf Namen raten.

## Nachfragen vor dem Schalten

Mira schaltet eine Entität erst nach ausdrücklichem „Ja“, wenn
- ihre Domain in `security.confirm_domains` steht (Standard: Schlösser, Alarmanlagen), **oder**
- sie in `security.confirm_entities` steht (YAML), **oder**
- sie das Label **„Mira: nachfragen“** trägt.

Feiner wird es nicht: Was Mira überhaupt sieht, regelt HAs Freigabe; „nur lesen“ oder Rollen gibt es nicht. Hintergrundjobs dürfen wie bisher nur lesen.

## Labels und Sprache

Labels haben in HA eine feste `label_id` (beim Anlegen einmal aus dem Namen gebildet) und einen änderbaren Namen. Entitäten speichern nur die ID.

- Mira erkennt ihre Labels **nur an der ID**: `mira` (Aufgaben) und `mira_confirm` (nachfragen).
- Die Integration legt sie beim Start an, falls sie fehlen, und gibt ihnen Namen und Beschreibung in HAs Sprache:
  - `mira`: „Mira“ – „Aufgaben von Mira“ / „Mira's tasks“
  - `mira_confirm`: „Mira: nachfragen“ / „Mira: confirm first“ – „Mira schaltet das erst, nachdem du es bestätigt hast.“ / „Mira only switches this after you confirmed.“
- Umbenennen durch den Nutzer stört nicht. Ein Sprachwechsel ändert vorhandene Namen nicht.
