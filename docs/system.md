# Mira – Systemdokumentation

Stand 2026-10-15 (nach M8, Aufgaben als HA-Automationen). Diese Seite erklärt,
wie Mira aufgebaut ist und wie die wichtigsten Abläufe laufen. Für
Anwender gibt es die [Anleitung](guide/de.md), für Entscheidungen und
Meilensteine den [Implementierungsplan](../Mira%20%E2%80%93%20Implementierungsplan.md),
für die Aufgaben und das Benutzer-Konzept die Spezifikationen unter
[spec/](spec/).

## 1. Überblick

Mira ist ein Haushalts-Agent für Home Assistant. Sie besteht aus zwei Teilen:

- **Mira-Agent** (Go, `cmd/mira`): Gespräch, Gedächtnis, Urteile, Zustellung
  von Nachrichten, Web-UI. Läuft als eigener Prozess neben Home Assistant.
- **Mira-Integration** (Python, `custom_components/mira`): verbindet HA mit
  dem Agenten. Sie stellt Mira als Konversations-Agent für Assist bereit,
  bietet Miras Aktionen für Automationen an (`mira.assess`, `mira.decide`,
  `mira.send_message`) und verwaltet Miras Automationen.

Grundsatz: **HA sind die Sinne, Mira ist Verstand und Stimme.** HA kennt die
Geräte, Zustände, Statistiken und entscheidet mit Triggern und Bedingungen,
*wann* etwas passiert. Mira entscheidet mit dem Sprachmodell, *was* das
bedeutet und *wie* es gesagt wird.

```mermaid
flowchart LR
    subgraph Haus
        SAT[Sprach-Satellit<br/>z. B. Echo mit echolocal]
        LAMP[Anzeige<br/>LED-Ring, Bildschirm]
    end
    subgraph HA[Home Assistant]
        ASSIST[Assist-Pipeline<br/>Wakeword, STT, TTS]
        INT[Mira-Integration<br/>custom_components/mira]
        AUTO[Automationen<br/>Label „Mira“]
        MCPHA[MCP-Server<br/>/api/mcp/assist]
        WS[WebSocket-API<br/>Zustände, Registries, Statistik]
    end
    subgraph Agent[Mira-Agent]
        API[HTTP-API /v1<br/>haconv]
        ENG[Gesprächs-Engine<br/>conversation]
        TASKS[Aufgaben<br/>tasks]
        NOT[Notifier]
        STORE[(SQLite<br/>mira.db)]
        UI[Web-UI /ui]
    end
    LLM[Sprachmodelle<br/>LM Studio lokal / Gemini]
    TG[Telegram]
    PHONE[Handy<br/>Companion-App]

    SAT <--> ASSIST
    ASSIST <--> INT
    INT <-->|HTTP + Token| API
    AUTO -->|mira.assess / decide / send_message| INT
    API --> ENG
    API --> TASKS
    ENG <--> LLM
    TASKS <--> LLM
    ENG -->|Werkzeuge| MCPHA
    ENG & TASKS & NOT <-->|WebSocket, Nicht-Admin| WS
    TASKS -->|mira/automation/*| INT
    NOT -->|start_conversation, Licht| WS
    NOT --> TG
    NOT -->|notify.mobile_app_*| PHONE
    ENG <--> TG
    ENG & TASKS & NOT --- STORE
    UI --- STORE
```

## 2. Pakete

| Paket | Aufgabe |
|---|---|
| `cmd/mira` | Start, Verdrahtung aller Teile (`app.go`), Befehle `run`, `chat`, `check`, `profile`, `skill` |
| `cmd/testha` | Test-HA in Docker starten, befüllen, Sensoren schalten |
| `internal/channels/haconv` | HTTP-API für die Integration: `/v1/conversation`, `/v1/messages`, `/v1/assess`, `/v1/decide`, `/v1/health`, `/v1/status` |
| `internal/channels/telegram` | Antworten für Telegram formatieren (Telegram-HTML, Aufteilen); Telegram selbst läuft über HAs Integration und den Adapter `custom_components/mira/telegram.py` ([Kanäle](spec/kanaele.md)) |
| `internal/conversation` | Die Gesprächs-Engine: Prompt bauen, Modell fragen, Werkzeuge ausführen, Schutz gegen unbelegte Antworten, „Übrigens“, Abgabe an stärkere Modelle, isolierte Aufrufe |
| `internal/prompt` | Prompt-Schichten, Beispielunterhaltung, Texte je Sprache |
| `internal/llm` | OpenAI-kompatibler Client (LM Studio, llama.cpp, Gemini) |
| `internal/tools` | Werkzeug-Register, Schutz (Guard) für kritische Aktionen, interne Werkzeuge (Gedächtnis, Erinnerungen, Nachrichten) |
| `internal/mcp` | MCP-Host: HA-Assist-Werkzeuge, Websuche |
| `internal/ha/ws` | WebSocket-Client zu HA: Zustände, Registries, Statistik, Dienste, Abos |
| `internal/ha/structure` | Hausstruktur (Bereiche, Entitäten) als Prompt-Schicht 4 |
| `internal/ha/assistprompt` | HAs eigener Assist-Prompt als Alternative für Schicht 4 |
| `internal/tasks` | Aufgaben: Urteile (`assess`, `decide`), Datenansichten, Entwurf und Verwaltung der Automationen, Werkzeuge |
| `internal/notifier` | Zustellung: wann, wo, wie eine Nachricht ankommt; Anzeigen (LED-Ring, Bildschirm); Erinnerungen |
| `internal/skills` | Skills im Agent-Skills-Format (`SKILL.md`), Zeitpläne, Kalender |
| `internal/calendar`, `internal/timeline` | Kalender aus Home Assistant lesen und ändern; die gemeinsame Darstellung der Zeitachse |
| `internal/feeds` | Feeds (RSS/Atom) lesen |
| `internal/store` | SQLite: Sitzungen, Verläufe, Nachrichten, Gedächtnis, Erinnerungen, Aufgaben-Vorschläge, Protokoll der Urteile |
| `internal/profile` | Nächtliche Verdichtung der Fakten zum Haushaltsprofil |
| `internal/ui` | Web-UI und ihre API (`/v1/ui/*`) |
| `internal/announce` | mDNS-Ankündigung (`_mira._tcp`), damit HA Mira findet |
| `internal/config`, `internal/i18n`, `internal/schedule`, `internal/telemetry` | Konfiguration, Sprachen, Cron, OpenTelemetry |

## 3. Ablauf: ein Sprachbefehl

```mermaid
sequenceDiagram
    autonumber
    participant U as Nutzer
    participant S as Satellit
    participant HA as HA Assist
    participant I as Mira-Integration
    participant A as Mira /v1/conversation
    participant E as Engine
    participant M as Modell
    participant T as Werkzeuge (MCP/intern)

    U->>S: „Wie warm ist es im Wohnzimmer?“
    S->>HA: Audio
    HA->>HA: Spracherkennung
    HA->>I: Text, device_id, conversation_id
    I->>A: POST /v1/conversation
    A->>E: Turn (Kanal voice, Bereich aus device_id)
    E->>E: Prompt aus Schichten 1–7
    E->>M: Anfrage mit Werkzeugen
    M-->>E: Werkzeug-Aufruf GetLiveContext
    E->>T: ausführen (über den Guard)
    T-->>E: Zustände
    E->>M: Ergebnis
    M-->>E: „Im Wohnzimmer sind es 24,7 Grad.“
    E->>E: Schutz: Antwort belegt? „Übrigens“ anhängen?
    E-->>A: Text, continue_conversation
    A-->>I: Antwort
    I-->>HA: Sprechtext
    HA-->>S: Sprachausgabe
```

Wichtig:

- **Kontext.** Was das Modell sieht, in fester Reihenfolge von stabil nach
  volatil: Systemnachricht (Regeln, Haus-Struktur, Haus-Profil), Werkzeuge,
  Beispielunterhaltung, Kanalstil, Verlauf, aktueller Turn. Der Anfang ist
  byte-identisch und für alle Kanäle gleich, damit er aus dem Cache kommt.
  Komposition, Cache und Kompaktierung: [Kontext](spec/kontext.md).
  Welche Sprache Mira wann spricht: [Sprache](spec/sprache.md).
  Skills, die eine HA-Aktion aufrufen (z. B. Wecker): [Skill-Aktionen](spec/skill-aktionen.md).
  Kalender lesen, Termine eintragen und absagen, To-do mit Fälligkeit: [Kalender](spec/kalender.md).
  Weitere Werkzeuge und Skills (Verlauf, Wetter, Musik, Kamera, Listen): [Plan](spec/skills-plan.md).
- **Schutz gegen unbelegte Antworten.** Kündigt das Modell eine Aktion nur
  an, meldet es etwas als erledigt oder nennt es Messwerte und Zustände,
  ohne ein Werkzeug aufgerufen zu haben, muss es nachholen. Sonst ersetzt
  der Code die Antwort ehrlich („habe ich nicht nachgesehen“).
- **Kritische Aktionen.** Bei Schlössern, Alarm und Garagentor verlangt der
  Guard ein ausdrückliches Ja im nächsten Turn.
- **„Übrigens“.** Hat der Raum wartende Nachrichten, hängt Mira sie an die
  Antwort an. Sie gelten dann als gehört.
- **Weiterhören.** Endet die Antwort mit einer Frage, bleibt der Satellit im
  Zuhörmodus (`continue_conversation`).
- **Fest im Code.** Ein schlichtes „Ja“ auf einen offenen Aufgabenvorschlag
  bestätigt der Code ohne Modell. Das steht auf der Besprechungsliste.

Telegram läuft genauso über die Integration: HAs Telegram-Integration
meldet eine Nachricht (`telegram_text`), der Adapter der Mira-Integration
ordnet den Chat einer Person zu und schickt sie mit Kanal `telegram` an
`/v1/conversation`; die Antwort (Telegram-HTML) geht mit
`telegram_bot.send_message` zurück. Eigener Stil: Markdown, länger.

## 4. Ablauf: eine Aufgabe anlegen

Eine Aufgabe ist eine HA-Automation mit dem Label „Mira“
([Spezifikation](spec/aufgaben.md)).

```mermaid
sequenceDiagram
    autonumber
    participant U as Nutzer
    participant E as Engine
    participant T as tasks
    participant P as Planer-Modell
    participant I as Mira-Integration
    participant HA as Home Assistant
    participant N as Notifier

    U->>E: „Achte auf den Gasverbrauch.“
    E->>T: Werkzeug task_create(goal)
    T-->>E: „Ich plane das …“ (sofort)
    E-->>U: „Ich plane die Aufgabe und melde mich mit einem Vorschlag.“
    Note over T: im Hintergrund
    T->>P: Ziel, Katalog der Entitäten, Regeln für Miras Aktionen
    P-->>T: Titel, Zusammenfassung, Automation (JSON)
    T->>I: mira/automation/validate
    I->>HA: HAs eigene Prüfung + Sperrliste (Schlösser, Skripte …)
    I-->>T: gültig / Fehler (bei Fehler ein zweiter Entwurf)
    T->>T: als Vorschlag speichern (Ursprung: Raum oder Chat)
    T->>N: Vorschlag zustellen („Soll ich so vorgehen?“)
    N-->>U: Ansage im Raum oder Telegram
    U->>E: „Ja.“
    E->>T: Vorschlag vom selben Ort bestätigen
    T->>I: mira/automation/save (id mira_…)
    I->>HA: automations.yaml schreiben, neu laden, Label „Mira“
    E-->>U: „Gut, die Aufgabe … läuft.“
```

- Das Planen läuft im Hintergrund, weil HA einem Sprach-Turn nur 15 Sekunden gibt.
- Der Planer schreibt HA-Syntax: `triggers`, `conditions`, `actions`. Er nutzt vor allem Miras Aktionen. Andere Aktionen wie Radio oder Push-Nachricht nimmt er nur, wenn du darum bittest, und der Vorschlag nennt sie ausdrücklich.
- Findet er keine passende Entität, sagt er das, statt etwas zu erfinden.
- Bestätigen gilt nur für den jüngsten Vorschlag **vom selben Ort**: Raum bei Sprache, Chat bei Telegram.
- Bearbeitet wird danach im Automations-Editor von HA. Mira hält nur die Automations-ID.

## 5. Ablauf: eine Aufgabe urteilt

```mermaid
sequenceDiagram
    autonumber
    participant HA as Home Assistant
    participant I as Mira-Integration
    participant A as Mira /v1/assess
    participant T as tasks
    participant WS as HA WebSocket
    participant DB as SQLite
    participant M as Modell
    participant N as Notifier

    HA->>HA: Trigger (z. B. stündlich um :15) und Bedingungen
    HA->>I: Aktion mira.assess(goal, data, cooldown, task)
    I->>A: POST /v1/assess
    A->>T: Assess
    T->>WS: Zustände, Statistik (same_hour: letzte Stunde + 14 Tage)
    T->>DB: Profil, Fakten, letzte Urteile dieser Aufgabe
    T->>M: isolierter Aufruf: Ziel, verdichtete Daten, Lage, Gedächtnis
    M-->>T: importance 0–3, spoken, details
    T->>DB: Urteil protokollieren
    alt wichtig und nicht in der Sperrzeit
        T->>DB: Nachricht anlegen (Quelle task)
        Note over N: stellt nach seinen Regeln zu
    end
    T-->>I: Antwort (importance, spoken, message_id)
    I-->>HA: response_variable
```

- **Isoliert.** Ein Urteil geht ohne den Haus-Prompt von rund 10.000 Tokens
  an das Modell, mit etwa 500 bis 1.500 Tokens. Stündliche Aufgaben bleiben
  dadurch billig.
- **Ansichten** verdichten Daten aus HA:
  - `state`: aktueller Zustand;
  - `same_hour`: die letzte Stunde gegen dieselbe Stunde an früheren Tagen;
  - `day`: heute bis jetzt gegen dieselben Wochentage.
- **`mira.decide`** läuft genauso, liefert aber `{answer, reason}` und legt
  nie eine Nachricht an. Eine echte HA-Bedingung ist nicht möglich, weil HA
  Bedingungen synchron prüft. Stattdessen nutzt die Automation die Aktion
  mit `response_variable` und danach `condition: template`.
- **`mira.send_message`** legt ohne Urteil eine Nachricht an, etwa für einen
  Wassermelder.
- **Probelauf** (UI oder Werkzeug `task_try`): Die Aktionen der Aufgabe
  laufen mit `notify: false`. Das Urteil kommt zurück, aber es entsteht keine
  Nachricht.

## 6. Ablauf: eine Nachricht wird zugestellt

```mermaid
flowchart TD
    Q[Nachricht wartet<br/>Wichtigkeit, Empfänger, vertraulich?] --> U{Wichtigkeit ≥ 3?}
    U -->|ja| NOW[Sofort: auf jedem Satelliten<br/>falls nicht vertraulich, und privat an alle]
    U -->|nein| QH{Ruhezeit oder Ruhemodus?}
    QH -->|ja| WAIT[wartet; Anzeige leuchtet trotzdem]
    QH -->|nein| IND[Anzeige leuchtet<br/>LED-Ring pulsiert, Bildschirm blendet ein]
    IND --> MODE{Satellit sagt beim<br/>Hereinkommen an?}
    MODE -->|ja| MOT[Bewegung im Raum → Ansage<br/>start_conversation, danach Zuhören]
    MODE -->|nein| ASK[Vorlesen auf Nachfrage<br/>oder als „Übrigens“]
    MOT --> HEARD[zugestellt]
    ASK --> HEARD
    WAIT --> ESC{Wichtigkeit ≥ 2 und bis 9 Uhr<br/>nicht gehört?}
    ESC -->|ja| PRIV[privat über die Kanäle der Person]
    HEARD --> FU{Antwort direkt nach der Ansage?}
    FU -->|ja| ACK[erledigt; „Was genau?“ bekommt die Details]
```

Weitere Regeln:

- **Vertrauliche Nachrichten** werden nur vorgelesen, wenn die Einstellung es
  erlaubt, etwa wenn der Empfänger allein zu Hause ist. Sonst gehen sie
  privat.
- **Erinnerungen** und **Antworten auf eigene Fragen** (der Vorschlag einer
  Aufgabe) sagt Mira im Raum der Anfrage an. Sie wartet dafür, bis der
  Satellit wieder frei ist, und spielt keinen Gong, wenn die Ansage direkt
  auf eine Antwort folgt.
- **Mehrere Nachrichten** fasst das Modell zu einer Ansage zusammen
  (Wichtiges zuerst); klappt das nicht, gilt eine feste Form.
- **Anzeige während einer Ansage:** Der LED-Ring leuchtet ruhig, solange der
  Satellit spricht, und geht mit dem Ende des Textes aus. Danach zeigt er
  wieder, ob etwas wartet. Satelliten mit Bildschirm (Echo Show mit TECHO5)
  blenden wartende Nachrichten ein; Mira erkennt sie von selbst.
- **Zustände einer Nachricht:** wartet, zugestellt, erledigt, abgelaufen. Die
  UI fasst zugestellt und erledigt zu einem Zustand zusammen.

## 7. Daten

```mermaid
erDiagram
    sessions ||--o{ messages : "enthält"
    messages }o--o| announcements : "angesagt in"
    assessments }o--|| HA_AUTOMATION : "Urteile von"
    tasks ||--o| HA_AUTOMATION : "wird nach Ja zu"
    memory_facts }o--|| memory_profile : "verdichtet zu"
    reminders }o--o| messages : "wird fällig als"

    sessions {
        text key PK
        text channel
        text area_id
    }
    messages {
        int id PK
        text session_key
        text role
        text content
    }
    messages {
        text id PK
        text source
        text dedupe_key
        int importance
        text audience
        text state
    }
    announcements {
        text id PK
        text device_id
        text area_id
        text message_ids
    }
    tasks {
        text id PK
        text goal
        text plan
        text origin
    }
    assessments {
        text id PK
        text task
        text kind
        int importance
        text spoken
        text message_id
    }
    memory_facts {
        text id PK
        text text
    }
    memory_profile {
        int id PK
        text text
    }
    reminders {
        text id PK
        text due_at
        text text
        text audience
    }
    HA_AUTOMATION {
        text id PK "mira_..."
        text label "Mira"
    }
```

- `tasks` enthält nur **offene Vorschläge**. Nach dem Ja lebt die Aufgabe als
  Automation in HA (`automations.yaml`), und der Vorschlag wird gelöscht.
- `assessments` ist das Protokoll der Urteile pro Aufgabe. Schlüssel ist die
  ID der Automation. Mira nutzt es als Gedächtnis der Aufgabe, die UI zeigt
  es an.
- Konfiguration: Grundlage ist `mira.yaml`. In der Datenbank liegen
  Änderungen aus der Web-UI pro Abschnitt darüber. Personen und Räume kann
  stattdessen die HA-Integration verwalten, dann zeigt die Mira-UI sie nur
  an.

## 8. Sicherheit

- **Eingeschränkter HA-Benutzer.** Mira hat keine Admin-Rechte in HA. Sie
  liest Zustände, Registries und die Statistik und schaltet über die
  Assist-Werkzeuge.
- **Automationen nur über die Integration.** Die Befehle
  `mira/automation/*` darf nur Miras eigener HA-Benutzer aufrufen. Die
  Integration erfährt ihn über `/v1/health`. Eine Aufgabe darf schalten und
  benachrichtigen; der Vorschlag nennt diese Aktionen ausdrücklich. Nie
  erlaubt ist, was Mira nur mit ausdrücklichem Ja schaltet (Schlösser,
  Alarm, Garagentor), sowie Skripte, Shell-Befehle und Neustarts. Das trägt
  der Nutzer bei Bedarf selbst im Editor von HA ein.
- **Kritische Aktionen** brauchen ein Ja des Nutzers. Nach Web-Inhalten
  im selben Turn sind sie gesperrt. Hintergrundaufgaben schreiben nie.
- **Fremder Text ist Daten.** Web-Ergebnisse, Feeds und Kalender gelten
  als Daten, nie als Anweisungen.
- **Geheimnisse** stehen nur in `.env`, etwa Tokens oder die Kalender-URL
  mit Adresse. Sie erreichen kein Modell.
- **Integration und Agent** sprechen nur mit Bearer-Token miteinander.

## 9. Testen

- **Go:** `go test ./...`
  - Engine, Prompt-Invarianten (byte-identische Schichten), Notifier-Policy,
    Aufgaben, UI-API;
  - Ende-zu-Ende-Tests mit `fakeha` (WebSocket-HA), `fakellm` (Modell)
    und `fakemcp`.
- **Python:** `.venv/bin/python -m pytest` für Config-Flow, Konversation,
  Entitäten, Unter-Einträge sowie Regeln und Rechte der Automationen.
- **Test-HA:** `go run ./cmd/testha up` startet HA 2026.10 in Docker. Es hat
  Demo-Geräte, einen Nicht-Admin-Benutzer, die Integration und
  `automations.yaml`. Dazu läuft eine Test-Mira mit `testenv/mira.yaml` auf
  Port 8788. So lassen sich ganze Abläufe prüfen, ohne das Haus anzufassen.
