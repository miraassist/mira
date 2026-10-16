# Kontext – Spezifikation

Stand 2026-10-16, gebaut in 0.6.9. Gilt für alle Kanäle. Siehe auch [Vorgänge](vorgaenge.md) und [Kanäle](kanaele.md).

Der **Kontext** ist alles, was das Modell bei einer Anfrage sieht. Zwei Dinge macht Mira damit:
- **Komposition**: was in welcher Reihenfolge ins Kontextfenster kommt;
- **Kompaktierung**: was herausfällt, wenn der Verlauf zu lang wird.

## 1. Komposition: was das Modell sieht

In dieser Reihenfolge (`internal/prompt`, `prompt.Build`):

| # | Teil | Inhalt | ändert sich bei | Schicht |
|---|---|---|---|---|
| 1 | **Systemnachricht** | Rolle und Regeln; Haus-Struktur (HAs Assist-Prompt, sonst Miras eigener Block: Bereiche, Geräte, Entitäten ohne Zustände); Haus-Profil (verdichtetes Haus-Gedächtnis) | Regeln: nie; Struktur: Gerät/Bereich neu oder umbenannt; Profil: nächtliche Verdichtung | 1, 4, 5 |
| 2 | **Werkzeuge** (eigenes Feld der Anfrage) | HAs Werkzeuge über MCP, Miras eigene (Gedächtnis, Nachrichten, Erinnerungen, Aufgaben, Skills …), weitere MCP-Server | Werkzeugliste ändert sich | 3 |
| 3 | **Beispielunterhaltung** | ein fester kurzer Austausch, der Ton und Werkzeuggebrauch zeigt | nie | – |
| 4 | **Kanalstil** | wie auf diesem Kanal geantwortet wird (Sprache: kurz, ohne Markdown; Telegram: länger, Listen, Links), als Nachricht mit fester Bestätigung | nie (pro Kanal fest) | 2 |
| 5 | **Verlauf** | die bisherigen Turns dieser Sitzung, wörtlich, mit Werkzeugaufrufen und Ergebnissen (Abschnitt 3, 4) | jeder Turn hängt an; Kompaktierung kürzt | 6 |
| 6 | **Aktueller Turn** | Uhrzeit; Kanal und Raum; wer spricht und dessen persönliches Profil; passende Gedächtnis-Fakten; Stand des laufenden Vorgangs; Kontext einer gerade angesagten Nachricht; offene Bestätigung; der Text | jede Anfrage | 7 |

Was nicht im Kontext steht: **Zustände von Geräten.** Die fragt das Modell bei Bedarf über Werkzeuge ab; die Regeln verlangen das.

## 2. Warum diese Reihenfolge: Cache

Modelle (lokal wie in der Cloud) rechnen einen **gleichen Anfang** nicht neu, sondern nehmen ihn aus dem Cache. Deshalb:
- **stabil vor volatil**: Teile 1–4 ändern sich selten, der Turn bei jeder Anfrage;
- Teile 1–4 sind für gleiche Eingaben **byte-identisch**: keine Zeitstempel, IDs, Zufallswerte oder unsortierten Listen;
- **der gemeinsame Anfang (1–3) ist für alle Kanäle gleich**, nur der Kanalstil unterscheidet sich; so teilen sich Sprache, Telegram und Hintergrundjobs den Cache;
- **Volatiles nur im Turn**, auch das persönliche Profil (sonst bräche der gemeinsame Anfang je Person);
- der Verlauf wird **nur angehängt**, nie mittendrin geändert; zwischen Verlauf und Turn wird nichts eingefügt;
- ein Fingerabdruck der Teile 1–5 (`prefix_hash`) steht in jedem Log-Eintrag „turn done“: ändert er sich unerwartet, ist der Cache versehentlich gebrochen.

## 3. Verlauf und Vorgang

- **Der Vorgang trägt das Anliegen**, bei Sprache und Chat gleich: Sein Stand steht bei jedem Turn frisch in Teil 6. Was ein Anliegen über viele Turns braucht, hängt nicht am Verlauf.
- **Der Verlauf liefert den Rückbezug**: „und das andere Licht?“, „wie alt ist der?“. Dafür reichen die letzten Turns; niemand bezieht sich einfach so auf eine uralte Nachricht.
- **Haus-Struktur und Gedächtnis** kommen bei jedem Turn aktuell dazu (Teile 1 und 6), unabhängig davon, wie alt der Verlauf ist.

## 4. Kompaktierung

Was das Modell vom Gespräch sieht: **der laufende Vorgang + die letzten Turns.** Älteres fällt heraus. Kein Zusammenfassen durch ein Modell, ein einfacher Schnitt:

- **Gezählt wird in ganzen Turns**, nicht in Nachrichten. Ein Turn ist die Nachricht des Nutzers mit allem, was daraus folgt (Werkzeugaufrufe, Ergebnisse, Antwort). Geschnitten wird nur zwischen Turns, nie zwischen einem Werkzeugaufruf und seinem Ergebnis.
- **In Sprüngen, nicht gleitend**: Erreicht der Verlauf `max_turns`, wird er auf `keep_turns` gekürzt. Dazwischen bleibt sein Anfang gleich, und der Cache greift. (Gleitend würde bei jedem Turn vorne etwas wegfallen und den Cache für den ganzen Verlauf brechen.)
- **Spätestens bei 90 % des Kontextfensters** des jeweiligen Modells wird ebenso gekürzt, auch wenn `max_turns` noch nicht erreicht ist (große Werkzeug-Ergebnisse, kleines lokales Modell).
- **Der laufende Vorgang bleibt immer ganz**, auch wenn er vor dem Schnitt begann.
- **Jeder Schnitt steht im Log** als `history compacted`, weil er den Cache bewusst bricht und Gesagtes wegfällt: Sitzung, Grund (`max_turns` oder `context` mit `prompt_tokens`, `limit`, `context_tokens`), Turns vorher und nachher, entfernte Nachrichten, ab wann entfernt und ab wann behalten wurde, und ob für einen laufenden Vorgang mehr behalten wurde (`kept_for_thread`). Lässt sich nicht weiter kürzen, weil schon nur noch `keep_turns` übrig sind, steht das als Warnung im Log.
- **Sitzungsdauer**: Sprache 5 Minuten (ein neuer Satz eine Viertelstunde später knüpft fast nie an); Telegram **ohne Zeitgrenze**, der Schnitt begrenzt den Verlauf selbst. `/neu` beginnt weiterhin ein neues Gespräch.

## 5. Einstellbare Werte

| Wert | Vorgabe | Bedeutung |
|---|---|---|
| `sessions.max_turns` | 50 | ab hier wird gekürzt |
| `sessions.keep_turns` | 30 | so viele Turns bleiben nach dem Schnitt |
| `llm.compact_at_fraction` (je Modell) | 0,9 | Anteil des Kontextfensters (`context_tokens`), ab dem gekürzt wird |
| `sessions.voice_ttl` | 5m | Sitzungsdauer Sprache |
| `sessions.telegram_ttl` | aus | Sitzungsdauer Telegram (aus = keine Zeitgrenze) |

## 6. Bewusst nicht

- **Keine Zusammenfassung durch ein Modell**: kostet einen Aufruf, ist nicht vorhersehbar, und was bleiben soll, steht im Gedächtnis oder im Ergebnis eines Vorgangs.
- **Alte Turns bleiben wörtlich**, auch mit ihrem damaligen Teil 6 (Uhrzeit, Gedächtnis-Treffer); sie werden nicht verschlankt.
