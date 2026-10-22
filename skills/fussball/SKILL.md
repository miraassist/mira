---
name: fussball
description: "Ergebnisse, nächste Spiele und Tabelle des eigenen Fußballvereins („Wie hat der KSC gespielt?“, „Wann spielt der KSC?“, „Wo steht der KSC in der Tabelle?“)."
license: MIT
metadata:
  mira:
    settings:
      verein:
        description: Ein Wort aus dem Namen deines Vereins, wie er bei OpenLigaDB heißt, z. B. Karlsruher oder Bayern.
        required: true
---
# Fußball

Die Spiele holst du von OpenLigaDB, einer freien Fußball-Datenbank, mit `web_page`.
Du bekommst JSON.

- **Letzte und nächste Spiele des Vereins:**
  `https://api.openligadb.de/getmatchesbyteam/{{verein}}/1/1` (eine Woche zurück,
  eine voraus). Je Spiel: `matchDateTime`, `team1` und `team2` (`teamName`),
  `leagueName`, `matchIsFinished` und in `matchResults` das „Endergebnis“
  (`pointsTeam1` : `pointsTeam2`). Manche Spiele stehen doppelt (zwei Einträge der
  Liga); nimm den mit Ergebnis. Für weiter zurück oder voraus erhöhe die Zahlen
  (`/4/0` heißt vier Wochen zurück).
- **Tabelle:** `https://api.openligadb.de/getbltable/LIGA/SAISON`, LIGA ist `bl1`,
  `bl2` oder `bl3`, SAISON das Jahr, in dem die Saison begann (Oktober 2026: 2026).
  Die Liga steht in `leagueName` der Spiele.

So antwortest du:
- **„Wie hat er gespielt?“:** das letzte beendete Spiel mit Gegner, Ergebnis aus Sicht
  des Vereins und Wochentag („Der KSC hat am Samstag 4 zu 0 gegen Darmstadt
  gewonnen.“).
- **„Wann spielt er?“:** das nächste Spiel mit Gegner, Tag, Uhrzeit, Heim oder
  auswärts und Wettbewerb, wenn es nicht die Liga ist (Pokal).
- **Tabelle:** Platz und Punkte des Vereins, auf Nachfrage die Plätze darum.
- Nenne nur, was OpenLigaDB liefert. Läuft ein Spiel gerade, sag den Stand, den es
  gibt, und dass es noch läuft.
