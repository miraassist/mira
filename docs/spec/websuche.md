# Websuche – Spezifikation

Stand 2026-10-18, beschlossen und gebaut in 0.6.29, Rückfall auf Bing in 0.6.30, Seitenanfang bei den Treffern in 0.6.31. Siehe auch die Sicherheitsregeln in der [Systemdoku](../system.md).

## Ziel

Im Internet nachschauen gehört zu dem, was Mira von einer Sprachsteuerung unterscheidet: Öffnungszeiten, Ergebnisse, Neuigkeiten, „Wie lange kocht ein Ei?“. Bisher ging das nur mit einem eigenen MCP-Server für die Suche. Der lief als Node-Programm neben Mira und im Container gar nicht, weil das Image kein Node hat.

Künftig bringt Mira die Suche selbst mit. Sie ist **an**, ohne Einrichtung und ohne Schlüssel. Abschalten kann man sie, etwa wenn man eine andere Suche als MCP-Server einbindet.

## Die Werkzeuge

- **`web_search`**: eine Suchanfrage, Antwort: bis zu 5 Treffer mit Titel, Adresse und Kurztext. Dazu der **Seitenanfang** (etwa 1.500 Zeichen Haupttext) des ersten lesbaren Treffers.
- **`web_page`**: eine Adresse, Antwort: Titel und Haupttext der Seite, gekürzt auf eine handliche Länge. Damit liest Mira einen Treffer genauer, wenn der Kurztext nicht reicht.

Den Seitenanfang liest `web_search` selbst mit, weil der Kurztext eines Treffers die Antwort oft nicht enthält und kleine Modelle von sich aus nicht nachlesen. Es versucht die ersten zwei Treffer (höchstens 6 Sekunden zusammen) und nimmt den ersten, der sich lesen lässt und mindestens 200 Zeichen Text hat. Seiten, die ihren Inhalt erst per Skript aufbauen, scheiden so aus. Mehr will das Modell, liest es einen Treffer mit `web_page` ganz.

## Wie gesucht wird

- **Quelle:** die HTML-Ergebnisseite von DuckDuckGo (`html.duckduckgo.com`), ein einfacher HTTP-Abruf.
  - Kein Schlüssel und kein Konto nötig.
  - Region nach Miras Sprache (`de-de`, `us-en`).
- **Rückfall:** Zeigt DuckDuckGo eine Bot-Prüfung (es schickt sie als HTTP 202, schon nach wenigen Suchen hintereinander von einer Adresse) oder antwortet nicht, fragt Mira Bing über dessen Ergebnisseite (`bing.com/search`, auch ohne Schlüssel). Bings verpackte Links werden entpackt.
  - Mojeek und Brave Search wurden ausprobiert: Beide sperren einfache Abrufe (Captcha, HTTP 429).
  - Gehen beide nicht, sagt das Werkzeug dem Modell ausdrücklich, dass es nicht nachschauen konnte und nicht aus dem Gedächtnis raten soll.
- **Bewusst ohne Browser:** Bing oder Brave über einen ferngesteuerten Browser (wie im bisherigen MCP-Server) sind langsam, machen das Image um Hunderte MB größer und brechen bei jeder Änderung der Seiten.
- **Seitentext:**
  - Mira lädt die Seite (höchstens 2 MB, nur HTML oder Text) und lässt Skripte, Navigation, Kopf- und Fußzeilen, Formulare und Ähnliches weg.
  - Gibt es `<article>` oder `<main>` mit genug Text, nimmt sie das, sonst den ganzen Inhalt.
- **Grenzen:**
  - Suche und Seite haben je 10 Sekunden.
  - Nur `http` und `https`.
  - Adressen im eigenen Netz (privat, Loopback, link-local) lehnt `web_page` ab, auch nach einer Weiterleitung. Eine Webseite soll Mira nicht dazu bringen, Geräte im Heimnetz abzufragen.
- **Grenze dieser Lösung:** Es sind Abrufe der Ergebnisseiten ohne offizielle Schnittstelle. Sie können sich ändern oder sperren. Mehr Aufwand ist hier bewusst nicht vorgesehen; wer eine verlässlichere Suche will, bindet eine mit Schlüssel als MCP-Server ein (etwa Brave Search) und schaltet die eingebaute ab.
- **Grenzen des Seitenanfangs:** Tagesaktuelles (Spielergebnisse, Nachrichten) steht oft nicht auf der ersten lesbaren Seite oder weiter unten. Ein kleines Modell (getestet: qwen3-30b-a3b) vertauscht dann Einzelheiten oder rät aus dem Gedächtnis; eine strengere Beschreibung des Werkzeugs änderte das nicht. Ein stärkeres Modell (getestet: Gemini Flash) antwortet ehrlich, wenn nichts da ist.

## Sicherheit

Beide Werkzeuge haben dieselben zwei Markierungen wie bisher der MCP-Server für die Suche:
- **Liest nur:** Sie ändern nichts. Deshalb dürfen sie auch Hintergrundarbeiten nutzen, etwa das Beurteilen von Aufgaben oder schwierige Fragen, die an ein stärkeres Modell abgegeben werden.
- **Fremder Webtext:** Was sie liefern, kann versteckte Anweisungen enthalten.
  - Nach ihrem Aufruf sind für den Rest der Anfrage kritische Aktionen gesperrt: Schlösser, Alarmanlage und alles, was nachfragen muss.
  - Etwas zu merken, eine Nachricht zu schicken oder eine Aufgabe anzulegen führt Mira in derselben Anfrage nur nach dem Ja des Nutzers aus (`AskAfterWeb`).
  - Das regelt der Code im Guard, nicht der Prompt.

## Abschalten

- **Wo:** In den Optionen der Integration unter **Allgemein**, Schalter **Internetsuche**, Vorgabe: an.
- **Wie:**
  - Die Integration schickt den Schalter als Config-Abschnitt `web_search` an den Agenten (`PUT /v1/ui/config`), wie die Persönlichkeit.
  - Der Agent speichert ihn in seiner Datenbank. Die Werkzeuge verschwinden oder erscheinen sofort, ohne Neustart.
- **`mira.yaml`:** `web_search: {enabled: false}` stellt die Vorgabe um. Was in HA eingestellt ist, gilt davor.
- **Ohne die eingebaute Suche:** Eine andere Suche bindet man als MCP-Server ein (`mcp_servers`, mit `read_only` und `web_content`). Beide gleichzeitig zu nutzen geht, ist aber selten sinnvoll.

## Später, wenn nötig

Eine Quelle mit offizieller Schnittstelle, etwa die Brave Search API (Schlüssel, kostenloses Kontingent) oder ein eigener SearXNG-Server. Das wäre dann eine Auswahl neben dem Schalter. Gebaut wird es erst, wenn DuckDuckGo im Alltag nicht reicht.
