# Einrichtung – Spezifikation

Stand 2026-10-17, gebaut in 0.6.22, Koppeln ohne Code, Modellwahl und Assistenten in 0.6.23, Koppeln erst nach der Zusammenfassung in 0.6.25, Übersicht zuerst in 0.6.26, Texte vereinfacht in 0.6.28. Siehe auch [Mira in Home Assistant](ha-anbindung.md) und die Sicherheit der Verbindungen in der [Systemdoku](../system.md).

## Ziel

Heute braucht ein neuer Nutzer neun Schritte:
- in HA: HACS, die Integration „Model Context Protocol Server“, einen eigenen Benutzer samt Token (dafür als dieser Benutzer anmelden), Freigaben;
- auf dem Docker-Rechner: `mira.yaml` und `.env` schreiben, Container starten;
- wieder in HA: denselben Token noch einmal eintragen, Mira als Gesprächsagent wählen.

Künftig sind es drei, und keiner davon verlangt, eine Config-Datei zu verstehen oder einen Code abzutippen:

1. **Mira über HACS installieren**, HA neu starten.
2. **Container starten**, eine Zeile, ohne Config-Datei:
   `docker run -d --name mira --network host -v mira-data:/var/lib/mira ghcr.io/miraassist/mira`
3. **Unter „Entdeckt“ Mira hinzufügen.** Eine Übersicht zeigt, was noch fehlt: das KI-Modell (LLM) und die Assistenten. „Mira jetzt nutzen“ erledigt den Rest.

Für Assist freigeben bleibt, wie bisher, eine Entscheidung des Nutzers in HA.

## 1. Die Integration richtet HA-seitig alles selbst ein

Die Integration läuft in HA mit Admin-Rechten. Bei der Einrichtung erledigt sie, was heute Handarbeit ist:

- **Miras HA-Benutzer:** Sie legt einen Benutzer „Mira“ an, **ohne Admin-Rechte** und ohne Anmeldung. Dazu erzeugt sie einen Long-Lived-Token für ihn. Gibt es den Benutzer schon (eine frühere Einrichtung), nimmt sie ihn und erneuert den Token.
- **HAs MCP-Server:** Fehlt die Integration „Model Context Protocol Server“ mit Assist als API, richtet sie sie ein.
- **Übergabe an den Agenten:** Sie schickt ihm HA-Adresse (HAs interne Adresse) und Token (`POST /v1/setup`). Der Agent speichert beides in seiner Datenbank und verbindet sich.
- **Entfernen:** Entfernt man die Integration, widerruft sie den Token. Den Benutzer lässt sie stehen, weil an seiner ID Miras Automationen hängen.

In der Config des Agenten steht danach nichts mehr über HA (`ha.url`, `ha.token`, der MCP-Eintrag für HA).

## 2. Koppeln statt Token tippen

- **Erster Start** ohne Token in der Config: Der Agent erzeugt einen zufälligen Zugangs-Token (`server.token`) und speichert ihn in seiner Datenbank.
- **Übersicht zuerst, Koppeln zuletzt.** Nach „Mira gefunden“ kommt gleich die Übersicht. Ihr Text zeigt, was gewählt ist und was noch fehlt; darunter die Zweige „KI-Modell (LLM) wählen“, „Mira verwenden in“ (die Assistenten; der bevorzugte ist vorgewählt) und „Mira jetzt nutzen“. Jeder Zweig kehrt zur Übersicht zurück. Einen Zurück-Knopf haben HA-Dialoge nicht; die Übersicht ersetzt ihn. „Mira jetzt nutzen“ erscheint erst, wenn nichts mehr fehlt.
  - Vorher ändert sich nichts, weder am Agenten noch in HA: kein Benutzer, kein Token, keine Kopplung. Ein abgebrochener Dialog lässt nichts halb eingerichtet zurück. HA lässt einen entdeckten Dialog beim Schließen offen und ruft beim Wiederöffnen den Schritt ohne Eingabe noch einmal auf. Daran erkennt die Integration das Wiederöffnen und zeigt die Übersicht, statt in einem Unterformular (etwa dem Schlüssel eines versehentlich gewählten Anbieters) festzustecken.
  - Damit der Dialog vorher Modelle auflisten und ausprobieren kann, beantwortet der Agent `/v1/setup/models` und `/v1/setup/probe` ohne Token, aber nur, solange er noch nie gekoppelt war.
  - „Mira jetzt nutzen“ legt Benutzer und Token an, schaltet HAs MCP-Server ein und schickt dann HA-Adresse, Token und Modell in **einem** Aufruf (`POST /v1/pair`). Der Agent speichert alles, gibt seinen Zugangs-Token heraus und startet neu, oder er tut gar nichts. Geht es schief, widerruft die Integration den Token wieder.
- **Ohne Code beim ersten Mal:** Ein Agent, der noch nie gekoppelt war, koppelt sich mit dem ersten Home Assistant, der „Mira jetzt nutzen“ abschließt.
  - **Warum das reicht:** Ein Angreifer im Heimnetz müsste zwischen Containerstart und Einrichtung zuschlagen. Er bliebe nicht unbemerkt: Der Dialog des Besitzers fragt dann nach einem Code. Die Anleitung sagt: im eigenen Heimnetz einrichten, nicht im Gästenetz.
  - Einen Code im mDNS-Eintrag gibt es bewusst nicht: Den könnte jedes Gerät im Netz lesen.
- **Entfernen gibt frei:** Entfernt man die Integration, meldet sie das dem Agenten (`POST /v1/unpair`). Er macht einen neuen Token, vergisst den HA-Zugang, behält das Modell und wartet wieder auf Einrichtung, das nächste Mal wieder ohne Code; die Zusammenfassung zeigt dann „KI-Modell: bleibt …“.
- **Erneutes Koppeln mit Code:** Ist der Agent noch gekoppelt (die Integration konnte ihn beim Entfernen nicht erreichen, oder ein anderer Home Assistant hat ihn), braucht es einen **Kopplungscode** aus sechs Ziffern: `mira pair` auf dem Rechner des Agenten (`docker exec mira /app/mira pair`). Er steht dann auch auf der Seite des Agenten. Der Code gilt 15 Minuten und 5 Versuche. Die Übersicht zeigt ihn als fehlend, mit dem Zweig „Kopplungscode eingeben“; verwendet wird er bei „Mira jetzt nutzen“, und stimmt er nicht, sagt die Übersicht das. Das Modell behält der Agent; ändern lässt es sich danach unter Konfigurieren.
- **Wie bisher:** Steht ein Token in der Config (`server.token`), gilt der, ohne Kopplung. So bleiben bestehende Installationen unverändert.

## 3. Agent ohne Config-Datei

- **KI-Modell (LLM) im Formular:** Gemeint ist das LLM, mit dem Mira denkt; „Sprachmodell“ heißt es nicht, das verwechselt man in HA mit Spracherkennung. Der Dialog fragt nur, woher es kommt, und meint den häufigsten Fall: ein Cloud-Anbieter, keine teure Hardware.
  - **Aus Home Assistant:** Hat HA schon eine KI-Integration (OpenAI, Google Gemini, OpenRouter, Ollama), steht sie oben in der Liste. Die Integration liest Adresse, Schlüssel und Modell aus deren Eintrag und gibt sie dem Agenten. Der Schlüssel wird nie angezeigt, auch nicht als Passwortfeld. Ändert man ihn später in HA, wählt man unter Konfigurieren → KI-Modell noch einmal „Aus Home Assistant“. Anthropic fehlt, weil seine OpenAI-kompatible Schnittstelle ungetestet ist.
  - **Anbieter:** OpenAI, Google Gemini, OpenRouter, Mistral mit fest eingetragener Adresse; gefragt wird nur der Schlüssel.
  - **Eigener Server:** Adresse (aus Sicht des Agenten) und optional ein Schlüssel. Eine Kategorie „lokal“ mit LM Studio, Ollama, llama.cpp gibt es bewusst nicht, und der Agent sucht das Netz nicht ab.
  - Das Modell wählt man aus der Liste, die der Agent bei dieser Adresse abfragt (`/v1/models`).
  - **Probelauf:** Bevor gespeichert wird, schickt der Agent eine echte kleine Anfrage mit einem Werkzeug. Sie muss beantwortet werden und das Werkzeug aufrufen. Fehler kommen mit Grund zurück und stehen verständlich im Formular: nicht erreichbar, Schlüssel abgelehnt, Modell unbekannt oder nicht geladen, Modell ohne Werkzeuge. Ein lokales Modell wird dabei gleich geladen.
  - Ändern lässt es sich später unter Konfigurieren → KI-Modell (LLM).
- **Vorgaben für alles andere:**
  - Port 8787 auf allen Schnittstellen;
  - mDNS an;
  - die Datenbank im Datenordner;
  - die mitgelieferten Skills;
  - Sprache und Personen aus HA.
- **`mira.yaml` bleibt optional,** für Fortgeschrittenes: weitere Modelle und Routing, weitere MCP-Server, Skills mit Einstellungen, Feinheiten des Notifiers. Was die Integration setzt (HA-Zugang, Sprachmodell), gilt vor der Datei; der Rest der Datei gilt wie bisher.

## 4. Mira als Gesprächsagent

Sonst wäre alles eingerichtet, und Mira antwortet trotzdem nie.
- Der letzte Schritt im Dialog zeigt HAs Assistenten (Assist-Pipelines), der bevorzugte ist angehakt. Satelliten auf „bevorzugt“ folgen ihm.
- Sobald Miras Entität existiert, setzt die Integration sie als Gesprächsagent dieser Assistenten und merkt sich, welchen Agenten jeder vorher hatte.
- Beim Entfernen bekommen die Assistenten, die noch Mira nutzen, ihren vorherigen Agenten zurück, oder HAs eigenen, wenn es den vorherigen nicht mehr gibt.

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
