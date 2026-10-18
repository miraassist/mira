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
| 5 | **Verlauf** | die bisherigen Turns dieser Sitzung, wörtlich, mit Werkzeugaufrufen und Ergebnissen (Abschnitt 4, 5) | jeder Turn hängt an; Kompaktierung kürzt | 6 |
| 6 | **Aktueller Turn** | Uhrzeit; Kanal und Raum; wer spricht und dessen persönliches Profil; passende Gedächtnis-Fakten; Stand des laufenden Vorgangs; Kontext einer gerade angesagten Nachricht; offene Bestätigung; der Text | jede Anfrage | 7 |

Was nicht im Kontext steht: **Zustände von Geräten.** Die fragt das Modell bei Bedarf über Werkzeuge ab; die Regeln verlangen das.

## 2. Darstellung von Entitäten

Stand 2026-10-17; gebaut sind die Stufen A, B und D, die Grundform „Wert“ und die Zeitachse mit `calendar_events` und `view: upcoming` (0.6.19). `history`, `weather_forecast` und `list_attribute` folgen.

Eine Entität kommt zu verschiedenen Zeiten auf verschiedenen Wegen ins Modell, jeweils in einer eigenen Darstellung. Zu jedem Weg steht hier, ob HA sie liefert (Mira ändert sie nicht) oder Mira sie baut (Mira bestimmt, was das Modell sieht).

### 2.1 Vier Stufen

| Stufe | Wann | Was das Modell sieht | Wer rendert |
|---|---|---|---|
| **A. Struktur** | immer, im festen Anfang (Teil 1, Cache) | dass es die Entität gibt: Name, Bereich, Typ. **Nie Zustände**: Sie würden den Cache bei jeder Änderung brechen. | HA (Assist-Prompt) oder, ohne ihn, Mira |
| **B. Turn** | jede Anfrage (Teil 6) | nur, was zum Ort und zur Person gehört: Bereich des Satelliten, wartende Nachrichten, laufender Vorgang. Keine Zustände von Geräten. | Mira |
| **C. Abfrage** | wenn das Modell ein Werkzeug ruft | **jetzt**: `GetLiveContext` von HA, Zustand und ausgewählte Attribute. **Über die Zeit**: Miras Werkzeuge für Termine, Verlauf, Vorhersage, Listen-Attribute (2.3). | HA bzw. Mira |
| **D. Hintergrund** | Urteil einer Aufgabe (`mira.assess`, `mira.decide`) | eine verdichtete Ansicht für genau eine Frage (`data[].view`, siehe [Aufgaben](aufgaben.md)) | Mira |

Werkzeug-Ergebnisse der Stufe C bleiben im Verlauf (Teil 5) und zählen für die Kompaktierung (Abschnitt 5). Sie sollen deshalb knapp sein.

### 2.2 Zwei Grundformen

**Wert**: ein Zustand mit Einheit und „seit wann“, gegebenenfalls über einen Zeitraum verdichtet (Summe, Mittel, gleiche Stunde wie sonst). Das sind die heutigen Ansichten der Aufgaben (`state`, `same_hour`, `day`).

**Zeitachse**: eine Liste von Einträgen mit Zeitpunkt und Feldern, etwa Termine, Wechsel im Logbuch, Stunden einer Vorhersage, Strompreise je Stunde, Abfahrten. Diese Fälle sind gleich gebaut, deshalb gibt es **eine** Darstellung mit denselben Stellgrößen:

| Stellgröße | Bedeutung | Beispiel |
|---|---|---|
| Fenster | von/bis; Standard je Typ | Kalender: heute bis in 7 Tagen; Verlauf: heute |
| Auswahl | Suchtext, Filter auf Zustand oder Feld | Titel enthält „Arzt“; nur „auf“ |
| Sortierung | nach Zeit (Standard) oder nach Wert | Strompreis aufsteigend |
| Begrenzung | höchstens N Einträge, dann „und N weitere“ | 40 Termine, 10 Wechsel |
| Felder | kurz (Standard) oder ausführlich; Text gekürzt, Links weg | Ort gekürzt, Beschreibung nur ausführlich |
| Gruppierung | nach Tag, mit Wochentag | „Mo 20.10.“ |
| Zeitformat | in der Sprache des Gesprächs ([Sprache](sprache.md)), relativ wo es hilft | „heute 14:12“, „morgen“ |
| Kennungen | kurze Kennungen (`t1`, `t2` …) statt HA-IDs, wo man auf einen Eintrag handeln kann | Termin absagen |

Die HA-IDs zu den Kennungen behält der Code; das Modell sieht sie nicht. Typ-spezifisch bleibt nur, was wirklich anders ist (2.4).

### 2.3 Werkzeuge der Zeitachse

Alle mit denselben Parametern für Fenster, Auswahl, Sortierung, Begrenzung und Felder:

| Werkzeug | Quelle in HA | Spezifikation |
|---|---|---|
| `calendar_events` | REST `/api/calendars/<entität>` (mit Termin-ID) | [Kalender](kalender.md) |
| `history` | Logbuch und History | [Plan](skills-plan.md) |
| `weather_forecast` | `weather.get_forecasts` | [Plan](skills-plan.md) |
| `list_attribute` | Attribut einer Entität, das eine Liste ist | [Plan](skills-plan.md) |

Dieselbe Darstellung nutzen die Ansichten der Aufgaben: `view: upcoming` (Kalender), `view: history`, `view: list`.

### 2.4 Typ × Stufe

| Typ | A. Struktur | C. jetzt | C. über die Zeit | D. Ansicht | Typ-spezifisch |
|---|---|---|---|---|---|
| Kalender | Name | nächster Termin (HA) | `calendar_events` | `upcoming` | ganztägig, Serien, nur lesbar |
| To-do | Name | Anzahl offen (HA) | `todo__get_items` (HA) | – | Fälligkeit |
| Wetter | Name | jetzt (HA) | `weather_forecast` | – | Wetterzustände, Wind nur auffällig |
| Sensor mit Zahl | Name, Einheit | Wert (HA) | `history` | `state`, `same_hour`, `day` | Zähler: Verbrauch statt Stand |
| Sensor mit Listen-Attribut | Name | Zustand (HA) | `list_attribute` | `list` | welche Felder |
| Schalter, Binärsensor, Licht | Name | Zustand (HA) | `history` | `state`, `history` | Zustandsnamen („offen“ statt `on`) |
| Person | Name | zu Hause/weg (HA) | `history` | `state` | – |

### 2.5 `GetLiveContext` bleibt, mit Auslöser

HAs `GetLiveContext` ist pro Entität schon kompakt: Name, Typ, Zustand, Bereich und ausgewählte Attribute, ohne rohe Listen. Gemessen am 2026-10-17 in einem echten Haus: 148 Entitäten, etwa 18 000 Zeichen ohne Filter, größter Eintrag 255 Zeichen. Es gibt deshalb keinen Grund, es zu ersetzen.

- **Was fehlt**: „seit wann“. Das deckt `history` ab.
- **Auslöser für einen Wrapper**: Mira legt erst dann einen dünnen Wrapper darum, wenn das Log zeigt, dass Aufrufe ohne Filter (das ganze Haus) den Verlauf spürbar aufblähen. Der Wrapper würde dann bei einem Aufruf ohne Filter nach Bereichen zusammenfassen oder einen Filter verlangen.

## 3. Warum diese Reihenfolge: Cache

Modelle (lokal wie in der Cloud) rechnen einen **gleichen Anfang** nicht neu, sondern nehmen ihn aus dem Cache. Deshalb:
- **stabil vor volatil**: Teile 1–4 ändern sich selten, der Turn bei jeder Anfrage;
- Teile 1–4 sind für gleiche Eingaben **byte-identisch**: keine Zeitstempel, IDs, Zufallswerte oder unsortierten Listen;
- **der gemeinsame Anfang (1–3) ist für alle Kanäle gleich**, nur der Kanalstil unterscheidet sich; so teilen sich Sprache, Telegram und Hintergrundjobs den Cache;
- **Volatiles nur im Turn**, auch das persönliche Profil (sonst bräche der gemeinsame Anfang je Person);
- der Verlauf wird **nur angehängt**, nie mittendrin geändert; zwischen Verlauf und Turn wird nichts eingefügt;
- ein Fingerabdruck der Teile 1–5 (`prefix_hash`) steht in jedem Log-Eintrag „turn done“: ändert er sich unerwartet, ist der Cache versehentlich gebrochen.

## 4. Verlauf und Vorgang

- **Der Vorgang trägt das Anliegen**, bei Sprache und Chat gleich: Sein Stand steht bei jedem Turn frisch in Teil 6. Was ein Anliegen über viele Turns braucht, hängt nicht am Verlauf.
- **Der Verlauf liefert den Rückbezug**: „und das andere Licht?“, „wie alt ist der?“. Dafür reichen die letzten Turns; niemand bezieht sich einfach so auf eine uralte Nachricht.
- **Haus-Struktur und Gedächtnis** kommen bei jedem Turn aktuell dazu (Teile 1 und 6), unabhängig davon, wie alt der Verlauf ist.

## 5. Kompaktierung

Was das Modell vom Gespräch sieht: **der laufende Vorgang + die letzten Turns.** Älteres fällt heraus. Kein Zusammenfassen durch ein Modell, ein einfacher Schnitt:

- **Gezählt wird in ganzen Turns**, nicht in Nachrichten. Ein Turn ist die Nachricht des Nutzers mit allem, was daraus folgt (Werkzeugaufrufe, Ergebnisse, Antwort). Geschnitten wird nur zwischen Turns, nie zwischen einem Werkzeugaufruf und seinem Ergebnis.
- **In Sprüngen, nicht gleitend**: Erreicht der Verlauf `max_turns`, wird er auf `keep_turns` gekürzt. Dazwischen bleibt sein Anfang gleich, und der Cache greift. (Gleitend würde bei jedem Turn vorne etwas wegfallen und den Cache für den ganzen Verlauf brechen.)
- **Spätestens bei 90 % des Kontextfensters** des jeweiligen Modells wird ebenso gekürzt, auch wenn `max_turns` noch nicht erreicht ist (große Werkzeug-Ergebnisse, kleines lokales Modell).
- **Der laufende Vorgang bleibt immer ganz**, auch wenn er vor dem Schnitt begann.
- **Jeder Schnitt steht im Log** als `history compacted`, weil er den Cache bewusst bricht und Gesagtes wegfällt: Sitzung, Grund (`max_turns` oder `context` mit `prompt_tokens`, `limit`, `context_tokens`), Turns vorher und nachher, entfernte Nachrichten, ab wann entfernt und ab wann behalten wurde, und ob für einen laufenden Vorgang mehr behalten wurde (`kept_for_thread`). Lässt sich nicht weiter kürzen, weil schon nur noch `keep_turns` übrig sind, steht das als Warnung im Log.
- **Sitzungsdauer**: Sprache 5 Minuten (ein neuer Satz eine Viertelstunde später knüpft fast nie an); Telegram **ohne Zeitgrenze**, der Schnitt begrenzt den Verlauf selbst. `/neu` beginnt weiterhin ein neues Gespräch.

## 6. Einstellbare Werte

| Wert | Vorgabe | Bedeutung |
|---|---|---|
| `sessions.max_turns` | 50 | ab hier wird gekürzt |
| `sessions.keep_turns` | 30 | so viele Turns bleiben nach dem Schnitt |
| `llm.compact_at_fraction` (je Modell) | 0,9 | Anteil des Kontextfensters (`context_tokens`), ab dem gekürzt wird |
| `sessions.voice_ttl` | 5m | Sitzungsdauer Sprache |
| `sessions.telegram_ttl` | aus | Sitzungsdauer Telegram (aus = keine Zeitgrenze) |

## 7. Bewusst nicht

- **Keine Zusammenfassung durch ein Modell**: kostet einen Aufruf, ist nicht vorhersehbar, und was bleiben soll, steht im Gedächtnis oder im Ergebnis eines Vorgangs.
- **Alte Turns bleiben wörtlich**, auch mit ihrem damaligen Teil 6 (Uhrzeit, Gedächtnis-Treffer); sie werden nicht verschlankt.

## 8. Beobachtet und Ideen (nicht beschlossen)

Stand 2026-10-18, gemessen mit Gemini Flash über die OpenAI-Schnittstelle.

**Gemessen**
- **Wachstum:** Die erste Anfrage eines Gesprächs hatte etwa 8.900 Tokens, die 27. etwa 15.800: im Schnitt rund 260 Tokens je Turn, bei Turns mit Kalender oder Vorgang bis 1.400. Über 90 % der Anfrage ist der gleichbleibende Anfang, davon etwa 72 % die 51 Werkzeugdefinitionen.
- **Zeit:** Gemini braucht je 10.000 Tokens mehr etwa 0,3 bis 0,4 Sekunden (9.000 Tokens: 2,0 s, 31.000 Tokens: 2,45 s). Das Wachstum macht Mira bei Gemini nicht merklich langsamer.
- **Cache:** Gemini meldet Cache-Treffer nur bei einer vollständig gleichen Anfrage. Wurde nur das Ende geändert, kam in vier Versuchen kein Treffer. Lokale Server cachen den gleichbleibenden Anfang.
- **Log:** `model round`, `tool done`, `context built`, `turn done` (docs/system.md) zeigen Zeit und Größe je Runde.

**Kosten:** Sie hängen an den Eingabe-Tokens, und die begrenzt `context_tokens` (mit `compact_at_fraction`). Eine zweite Kostengrenze gibt es nicht. Bei einem Cloud-Modell ist `context_tokens` also keine Fensterbreite, sondern die Grenze der Anfrage. In der Test-Konfiguration steht es für Gemini auf 24.000 (Kürzung ab etwa 21.600 Tokens).

**Ideen**
1. **Vorgabe `context_tokens` bei Cloud-Modellen prüfen.** Das Einrichtungsformular setzt den Wert nicht, es gilt die Vorgabe 16.384. Der gleichbleibende Anfang hat allein 9.000 bis 11.000 Tokens, die Grenze von 90 % (14.700) erreicht der Verlauf nach etwa 20 Turns. Dann will Mira kürzen, hat aber weniger als `keep_turns` Turns, und `history cannot be cut further` steht bei jedem Turn im Log. Eine passende Vorgabe für Cloud-Modelle wäre zu überlegen.
2. **Alte Werkzeug-Ergebnisse im Verlauf verkürzen** (Kalenderlisten, Zustände), nach den letzten paar Turns durch einen Kurztext ersetzt. Das hielte den Verlauf klein, ohne das Gespräch zu kürzen. Es widerspricht dem Satz in Abschnitt 7 („Alte Turns bleiben wörtlich“) und müsste gegen ihn abgewogen werden.
3. **Kleiner Kontext für die Antwort nach einer Bestätigung.** Beendet ein Werkzeug die Anfrage (`thread_confirm`, `thread_cancel`, `thread_undo`), könnte die zweite Runde nur Persönlichkeit, den Turn und das Ergebnis des Aufrufs bekommen, ohne Werkzeuge, Haus und Verlauf. Die Antwort käme wie bisher ins Protokoll. Offen: Nur bei Cloud-Modellen, weil eine abweichende Anfrage bei einem lokalen Server den Cache verdrängt. Bei Fehlern und bei mehreren Aufrufen nacheinander bleibt es beim vollen Kontext. Die Ergebnistexte der Finisher müssten für sich verständlich sein. Der Gewinn liegt eher bei den Kosten als bei der Zeit. Vorerst nicht gebaut.
4. **Werkzeuge kürzen.** 51 Definitionen sind 72 % jeder Anfrage. Home Assistants MCP-Beschreibungen sind lang, viele Werkzeuge werden nie gebraucht. Ein Filter nach Kanal oder Nutzung würde jede Runde verkleinern, ginge aber gegen den gemeinsamen Anfang aller Kanäle (Abschnitt 3), der lokal den Cache hält.
