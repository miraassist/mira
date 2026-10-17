# Einrichtung – Spezifikation

Stand 2026-10-17, gebaut in 0.6.22. Siehe auch [Mira in Home Assistant](ha-anbindung.md) und die Sicherheit der Verbindungen in der [Systemdoku](../system.md).

## Ziel

Heute braucht ein neuer Nutzer neun Schritte:
- in HA: HACS, die Integration „Model Context Protocol Server“, einen eigenen Benutzer samt Token (dafür als dieser Benutzer anmelden), Freigaben;
- auf dem Docker-Rechner: `mira.yaml` und `.env` schreiben, Container starten;
- wieder in HA: denselben Token noch einmal eintragen, Mira als Gesprächsagent wählen.

Künftig sind es fünf, und keiner davon verlangt, eine Config-Datei zu verstehen:

1. **Container starten**, eine Zeile, ohne Config-Datei:
   `docker run -d --name mira --network host -v mira-data:/var/lib/mira ghcr.io/miraassist/mira`
2. **Mira über HACS installieren**, HA neu starten.
3. **Unter „Entdeckt“ Mira hinzufügen** und den **Kopplungscode** eingeben, den der Agent zeigt.
4. **Im Formular das Sprachmodell wählen**: Adresse, Modell, gegebenenfalls Schlüssel.
5. **In der Sprachassistenten-Pipeline Mira wählen.**

Für Assist freigeben bleibt, wie bisher, eine Entscheidung des Nutzers in HA.

## 1. Die Integration richtet HA-seitig alles selbst ein

Die Integration läuft in HA mit Admin-Rechten. Bei der Einrichtung erledigt sie, was heute Handarbeit ist:

- **Miras HA-Benutzer:** Sie legt einen Benutzer „Mira“ an, **ohne Admin-Rechte** und ohne Anmeldung. Dazu erzeugt sie einen Long-Lived-Token für ihn. Gibt es den Benutzer schon (eine frühere Einrichtung), nimmt sie ihn und erneuert den Token.
- **HAs MCP-Server:** Fehlt die Integration „Model Context Protocol Server“ mit Assist als API, richtet sie sie ein.
- **Übergabe an den Agenten:** Sie schickt ihm HA-Adresse (HAs interne Adresse) und Token (`POST /v1/setup`). Der Agent speichert beides in seiner Datenbank und verbindet sich.
- **Entfernen:** Entfernt man die Integration, widerruft sie den Token. Den Benutzer lässt sie stehen, weil an seiner ID Miras Automationen hängen.

In der Config des Agenten steht danach nichts mehr über HA (`ha.url`, `ha.token`, der MCP-Eintrag für HA).

## 2. Koppeln statt Token tippen

- **Erster Start** ohne Token in der Config: Der Agent erzeugt einen zufälligen Zugangs-Token (`server.token`) und speichert ihn in seiner Datenbank. Dazu erzeugt er einen **Kopplungscode** aus sechs Ziffern und zeigt ihn im Log und auf seiner Startseite (`http://<rechner>:8787`), solange er nicht gekoppelt ist.
- **Koppeln:** Die Integration findet den Agenten per mDNS. Das Formular fragt nur nach dem Code. Mit dem Code holt sich die Integration den Token (`POST /v1/pair`).
- **Danach** ist der Code verbraucht. Neu gekoppelt wird nur mit einem neuen Code: Der Agent zeigt einen, wenn man ihn neu startet, solange er nicht gekoppelt ist, oder auf Wunsch über `mira pair`.
- **Schutz:**
  - Der Code gilt 15 Minuten.
  - Nach 5 falschen Versuchen ist er verbraucht.
  - Den Code sieht nur, wer an das Log oder die Seite des Agenten kommt, also wer ihn betreibt.
- **Wie bisher:** Steht ein Token in der Config (`server.token`), gilt der, ohne Kopplung. So bleiben bestehende Installationen unverändert.

## 3. Agent ohne Config-Datei

- **Sprachmodell im Formular:** Bei der Einrichtung fragt die Integration nach Adresse, Modell und gegebenenfalls Schlüssel.
  - Vorschläge für die Adresse: LM Studio, Ollama, llama.cpp, ein Cloud-Anbieter mit OpenAI-Schnittstelle.
  - Das Modell wählt man aus der Liste, die der Agent bei dieser Adresse abfragt (`/v1/models`).
  - Der Agent prüft die Verbindung, bevor gespeichert wird, und speichert alles in seiner Datenbank.
  - Ändern lässt es sich später unter Konfigurieren → Sprachmodell.
- **Vorgaben für alles andere:**
  - Port 8787 auf allen Schnittstellen;
  - mDNS an;
  - die Datenbank im Datenordner;
  - die mitgelieferten Skills;
  - Sprache und Personen aus HA.
- **`mira.yaml` bleibt optional,** für Fortgeschrittenes: weitere Modelle und Routing, weitere MCP-Server, Skills mit Einstellungen, Feinheiten des Notifiers. Was die Integration setzt (HA-Zugang, Sprachmodell), gilt vor der Datei; der Rest der Datei gilt wie bisher.

## Datenbankpfad

Der Datenbankpfad hat eine Vorgabe, die für fast alle Installationen passt:
- Das Image setzt seinen Datenordner (`MIRA_DATA=/var/lib/mira`), die Vorgabe ist dann `/var/lib/mira/mira.db`.
- Ohne `MIRA_DATA` (das Programm direkt auf dem Rechner) bleibt es `mira.db` im Arbeitsordner.

`store.path` muss man nur noch setzen, wenn man es anders will.

## Bestehende Installationen

- **Mit Config-Datei:** Eine Installation mit `server.token`, `ha.url` und `ha.token` in `mira.yaml` läuft unverändert weiter. Die Integration richtet keinen neuen Benutzer ein, solange der Agent meldet, dass er schon einen HA-Zugang hat (`/v1/health`).
- **Umstellen:** Wer auf den neuen Weg will, nimmt die Einträge aus der Datei und koppelt neu.

## Bewusst nicht

- **HA-Add-on:** ein eigener, späterer Schritt.
- **Verschlüsselung zwischen Integration und Agent:** Der Normalfall ist derselbe Rechner (siehe Systemdoku, Verbindungen).
