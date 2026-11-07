---
name: musikquiz
description: "Ein Musik-Quiz wie „Hitster“: Mira spielt kurz Songs an, die Spieler raten Interpret, Titel, Jahr oder Jahrzehnt („Lass uns ein Musik-Quiz spielen“, „Musik-Quiz mit 80er-Hits“); danach geht es mit der Antwort, „nochmal“, „weiter“ oder „Wie steht es?“. Nur an einem Satelliten mit Lautsprecher."
license: MIT
metadata:
  mira:
    target:
      plan: true
      bound: room
      timeout: 1h
    random:
      zeit:
        60er: [1960, 1961, 1962, 1963, 1964, 1965, 1966, 1967, 1968, 1969]
        70er: [1970, 1971, 1972, 1973, 1974, 1975, 1976, 1977, 1978, 1979]
        80er: [1980, 1981, 1982, 1983, 1984, 1985, 1986, 1987, 1988, 1989]
        90er: [1990, 1991, 1992, 1993, 1994, 1995, 1996, 1997, 1998, 1999]
        2000er: [2000, 2001, 2002, 2003, 2004, 2005, 2006, 2007, 2008, 2009]
        2010er: [2010, 2011, 2012, 2013, 2014, 2015, 2016, 2017, 2018, 2019]
      herkunft: [deutschsprachig, englischsprachig, aus einem anderen Land]
---
# Musik-Quiz

Du spielst ein Musik-Quiz: Du spielst je Song 30 Sekunden an, die Spieler raten.
Das Quiz ist ein Vorgang mit Plan: Im Plan stehen die Songs mit ihren Antworten
und die Punkte. Verrate nie vorher, welcher Song läuft.

Ruf Werkzeuge, die nicht aufeinander warten, immer **zusammen in einem Schritt**
auf: Jeder Schritt mehr lässt die Spieler warten.

1. **Vorher kurz klären**, mit einer Frage, nur was nicht gesagt ist: Genre oder
   Jahrzehnt (oder „gemischt“), was geraten wird (Interpret, Titel, Jahr,
   Jahrzehnt oder alles) und wer mitspielt (Namen; allein keine). So viele
   Songs, wie gewünscht; ohne Wunsch zehn.
2. **Songs wählen:** Lose sie zuerst aus: Ruf `random_pick` auf, skill
   `musikquiz`, `count` so viele wie Songs; unter `fixed` das Jahrzehnt (`zeit`,
   etwa „80er“), wenn gesagt; bei „gemischt“ lässt du es weg, dann wird es
   ausgelost. Wähle dann je Zeile einen bekannten Song aus genau diesem Jahr und
   mit der Herkunft der Zeile, im Genre, wenn eins gesagt ist; von
   verschiedenen Interpreten; keine Live-, Remix- oder Coverfassungen. Gibt es
   zu einer Zeile keinen passenden Song, bleibt das Jahr, und die Herkunft gibt
   nach. Das Jahr ist das der ersten Veröffentlichung des Songs,
   nicht einer Neuauflage.
3. **Songs suchen und prüfen**, alle auf einmal:
   - In einem Schritt für jeden Song `web_page` auf
     `https://api.deezer.com/search?q=INTERPRET%20TITEL&order=RANKING&limit=2`
     (Leerzeichen als %20). Nimm je den Treffer, bei dem `artist.name` und
     `title` passen und der Titel keinen Zusatz hat („Live“, „Acoustic“,
     „Remix“, eine Jahreszahl). Davon brauchst du `id`, `preview`, `isrc` und
     `album.cover_xl`. Kein passender Treffer oder kein `preview`: nimm
     stattdessen einen anderen Song.
   - Im nächsten Schritt für jeden Song `web_page` auf
     `https://musicbrainz.org/ws/2/isrc/ISRC?fmt=json`: das früheste
     `first-release-date` der `recordings`. Weicht es um mehr als ein Jahr von
     deinem ab, gilt das von MusicBrainz; sonst bleibt deins. Klappt die
     Abfrage nicht, bleibt deins.
4. **Plan schreiben** mit `thread_plan` (action `write`), im selben Schritt wie
   der erste Song (5):
   - die Überschrift „# Musik-Quiz: 80er · Jahr“ (Auswahl · was geraten wird),
     darunter „Spieler: Anna“ (mehrere: „Spieler: Anna, Ben“);
   - jeder Song ein Kästchen mit Antwort, Deezer-id und Cover:
     `- [ ] a-ha – Take on Me (1985) · id 664107 · Cover https://…`.
   Eine Zeile mit Punkten gibt es nicht: Die Punkte stehen bei jedem Song,
   sobald er bewertet ist (6), und werden zusammengezählt. Bei mehreren
   Spielern kommen sie reihum dran: schreib den Namen vor den Song („Anna: …“).
   **Wer antwortet:** Spielt nur einer, ist jede Antwort seine, auch wenn du
   die Stimme nicht erkennst oder einen anderen Namen hörst. Bei mehreren gilt
   die Antwort dem, der laut Plan dran ist.
5. **Einen Song spielen:** Sag nur „Titel 3 von 10:“ (bei mehreren mit dem
   Namen: „Anna, Titel 3 von 10:“) und ruf `clip_play` mit seinem `preview` auf,
   Titel „Musik-Quiz“, Untertitel „Titel 3 von 10“. Der Song spielt nach
   deiner Antwort. Die Adresse des `preview` gilt nur kurz: Für jeden Song
   außer dem ersten holst du sie frisch mit `web_page` auf
   `https://api.deezer.com/track/ID`.
6. **Antwort bewerten:** Welcher Song gerade läuft, steht im Plan unter
   „Jetzt“, nicht in deinem Gesprächsverlauf: Es raten mehrere, und was andere
   geantwortet haben, siehst du im Verlauf nicht. Bewerte immer gegen den Song,
   der im Plan „Jetzt“ ist. Ist die Antwort richtig (auch sinngemäß, ein kleiner Fehler
   im Namen zählt nicht), sag „Richtig!“; sonst die richtige Antwort.
   Punkte: Interpret, Titel, Jahr (genau) oder Jahrzehnt je einen; bei „alles“
   je einen für Interpret, Titel und Jahr (ein Jahr daneben zählt noch).
   Dann in **einem** Schritt zusammen:
   - `display_card` mit dem Song: Titel „Songtitel · Interpret“ (kurz), Zeile
     das Jahr, Bild das Cover aus dem Plan, 20 Sekunden;
   - den Song abhaken mit seinem Ergebnis: `thread_plan` action `check`, step
     die Nummer des Songs, note „1 Punkt“ oder „0 Punkte“ (bei „alles“ bis zu
     3). Schreib den Plan dafür nicht neu;
   - `web_page` auf `https://api.deezer.com/track/ID` für den nächsten Song.
   Danach den nächsten Song spielen (5).
7. **Was sonst kommt:**
   - „weiter“ oder „weiß ich nicht“: die Antwort sagen, keinen Punkt, sonst wie 6.
   - „nochmal“: denselben Song noch einmal, mit frischer Adresse (5), ohne zu
     bewerten.
   - „Wie steht es?“: die Punkte, zusammengezählt aus den bewerteten Songs im
     Plan (bei mehreren je Spieler).
   - „Stopp“ oder „Pause“: nur die Musik anhalten (die Pause von Home
     Assistant); das Spiel bleibt, es geht mit der Antwort oder „weiter“
     weiter. Nur „Abbrechen“ oder „Hör auf zu spielen“ beendet das Spiel mit
     `thread_cancel`.
   - Steht im Vorgang, dass der letzte Clip nicht abgespielt werden konnte,
     sag das und frag, ob du es noch einmal versuchen sollst.
8. **Ende:** Die Antwort auf den letzten Song bewertest du **genau wie 6**:
   Karte und der letzte Song abgehakt mit seinem Ergebnis (`check` mit note),
   nur ohne nächsten Song. Hör dann nicht auf, sondern schließ das Spiel **in
   derselben Antwort** ab:
   - zähl die Punkte aus dem Plan zusammen (bei mehreren je Spieler);
   - ein Klang mit `sound_play`, je nach dem Anteil der möglichen Punkte (bei
     mehreren alle zusammen): alle `fanfare_big`, ab 80 % `fanfare`, unter
     50 % `too_bad`, dazwischen keinen;
   - dann sag „Richtig!“ oder die Auflösung, die Punkte, bei mehreren wer
     gewonnen hat, und frag, ob noch eine Runde gewünscht ist (dann neuer Plan
     mit neuen Songs, neu ausgelost wie in 2). Will niemand mehr spielen, beende den Vorgang mit
     `thread_cancel`.
