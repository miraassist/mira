# Websuche – Spezifikation

Stand 2026-10-18, beschlossen und gebaut in 0.6.29. Siehe auch die Sicherheitsregeln in der [Systemdoku](../system.md).

## Ziel

Im Internet nachschauen gehört zu dem, was Mira von einer Sprachsteuerung unterscheidet: Öffnungszeiten, Ergebnisse, Neuigkeiten, „Wie lange kocht ein Ei?“. Bisher ging das nur mit einem eigenen MCP-Server für die Suche. Der lief als Node-Programm neben Mira und im Container gar nicht, weil das Image kein Node hat.

Künftig bringt Mira die Suche selbst mit. Sie ist **an**, ohne Einrichtung und ohne Schlüssel. Abschalten kann man sie, etwa wenn man eine andere Suche als MCP-Server einbindet.

## Die Werkzeuge

- **`web_search`**: eine Suchanfrage, Antwort: bis zu 5 Treffer mit Titel, Adresse und Kurztext.
- **`web_page`**: eine Adresse, Antwort: Titel und Haupttext der Seite, gekürzt auf eine handliche Länge. Damit liest Mira einen Treffer genauer, wenn der Kurztext nicht reicht.

„Suchen und gleich die Seiten lesen“ ist kein eigenes Werkzeug. Das Modell ruft beide nacheinander auf und liest nur, was es braucht.

## Wie gesucht wird

- **Quelle:** die HTML-Ergebnisseite von DuckDuckGo (`html.duckduckgo.com`), ein einfacher HTTP-Abruf.
  - Kein Schlüssel und kein Konto nötig.
  - Region nach Miras Sprache (`de-de`, `us-en`).
- **Bewusst ohne Browser:** Bing oder Brave über einen ferngesteuerten Browser (wie im bisherigen MCP-Server) sind langsam, machen das Image um Hunderte MB größer und brechen bei jeder Änderung der Seiten.
- **Seitentext:**
  - Mira lädt die Seite (höchstens 2 MB, nur HTML oder Text) und lässt Skripte, Navigation, Kopf- und Fußzeilen, Formulare und Ähnliches weg.
  - Gibt es `<article>` oder `<main>` mit genug Text, nimmt sie das, sonst den ganzen Inhalt.
- **Grenzen:**
  - Suche und Seite haben je 10 Sekunden.
  - Nur `http` und `https`.
  - Adressen im eigenen Netz (privat, Loopback, link-local) lehnt `web_page` ab, auch nach einer Weiterleitung. Eine Webseite soll Mira nicht dazu bringen, Geräte im Heimnetz abzufragen.
- **Gesperrt:**
  - Zeigt DuckDuckGo statt Treffern eine Bot-Prüfung, sagt das Werkzeug das. Das Modell sagt dann ehrlich, dass die Suche gerade nicht geht, statt zu raten.
  - Für einen Haushalt mit ein paar Suchen am Tag ist das selten.

## Sicherheit

Beide Werkzeuge haben dieselben zwei Markierungen wie bisher der MCP-Server für die Suche:
- **Liest nur:** Sie ändern nichts. Deshalb dürfen sie auch Hintergrundarbeiten nutzen, etwa das Beurteilen von Aufgaben oder schwierige Fragen, die an ein stärkeres Modell abgegeben werden.
- **Fremder Webtext:** Was sie liefern, kann versteckte Anweisungen enthalten.
  - Nach ihrem Aufruf sind für den Rest der Anfrage kritische Aktionen gesperrt: Schlösser, Alarmanlage und alles, was nachfragen muss.
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
