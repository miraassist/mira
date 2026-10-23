---
name: fussball
description: "Ergebnisse, nächste Spiele und Tabelle des eigenen Fußballvereins („Wie hat der KSC gespielt?“, „Wann spielt der KSC?“, „Wo steht der KSC in der Tabelle?“)."
license: MIT
metadata:
  mira:
    settings:
      verein:
        description: Der Verein des Hauses, ein Wort aus seinem Namen, wie er bei OpenLigaDB heißt, z. B. Karlsruher oder Bayern. Hat eine Person einen eigenen Verein in Miras Gedächtnis, gilt für sie der.
---
# Fußball

Die Spiele holst du von OpenLigaDB, einer freien Fußball-Datenbank, mit `web_page`.
Du bekommst JSON.

**Welcher Verein:** Nennt der Nutzer einen, nimm den. Sonst den Verein der Person,
die fragt, wenn er in dem steht, was Mira über sie weiß („Ben ist KSC-Fan“). Sonst
den des Hauses: „{{verein}}“. Ist keiner bekannt, frag, welcher es ist, und biete an,
ihn dir zu merken (memory_save, etwa „Ben ist KSC-Fan“). In die Adressen unten
kommt ein Wort aus dem Vereinsnamen („Karlsruher“, „Bayern“), im Folgenden VEREIN.

- **Letzte und nächste Spiele des Vereins:**
  `https://api.openligadb.de/getmatchesbyteam/VEREIN/1/1` (eine Woche zurück,
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
