---
name: quiz
description: "Ein Quiz spielen, allein oder mit mehreren, mit Punkten („Lass uns ein Quiz spielen“, „Stell mir Fragen zu Tieren“); danach geht es mit der Antwort, „weiter“ oder „Wie steht es?“."
license: MIT
metadata:
  mira:
    target:
      plan: true
      bound: room
      timeout: 1h
    random:
      schwierigkeit: [leicht, mittel, schwer]
      thema:
        Tiere: [Säugetiere, Vögel, Meerestiere, Insekten, Reptilien, Dinosaurier, Haustiere, Tierrekorde]
        Geografie: [Hauptstädte, Flüsse und Seen, Gebirge, Länder und Flaggen, Inseln, Deutschland, Wüsten und Klima]
        Natur: [Pflanzen, Bäume, Wetter, Jahreszeiten, Vulkane und Erdbeben, Steine und Kristalle]
        Weltall: [Planeten, Sterne, Mond, Raumfahrt, Sonne]
        Geschichte: [Steinzeit, Antike, Ritter und Burgen, Entdecker, Erfindungen, 20. Jahrhundert]
        Wissenschaft: [menschlicher Körper, Chemie im Alltag, Physik im Alltag, Zahlen und Rechnen]
        Sport: [Fußball, Olympische Spiele, Wintersport, Ballsportarten, Rekorde]
        Musik: [Instrumente, Komponisten, Popmusik, Kinderlieder]
        Kunst und Literatur: [Maler, Märchen, Kinderbücher, Sagen und Mythen]
        Film und Fernsehen: [Zeichentrick, Kinderserien, Filmklassiker, Filmfiguren]
        Essen und Trinken: [Obst und Gemüse, Gerichte aus aller Welt, Backen, woher Lebensmittel kommen]
        Sprache: [Redewendungen, Wortbedeutungen, Fremdwörter, Sprachen der Welt]
        Technik: [Fahrzeuge, Computer, Bauwerke, Geräte im Haushalt]
      frageart: [Wer, Was, Wo, Wann, Wie viele, Was ist größer oder schneller, Richtig oder falsch, Was gehört nicht dazu]
---
# Quiz

Du spielst ein Quiz mit dem Nutzer, allein oder mit mehreren. Das Quiz ist ein
Vorgang mit Plan: Im Plan stehen die Fragen und die Punkte, und im Kontext steht,
welche Frage gerade dran ist.

1. **Vorher kurz klären**, mit einer Frage, nur was nicht gesagt ist: Thema (oder
   „gemischt“) und wer mitspielt (Namen). Spielt nur einer, brauchst du keine Namen.
   Steht in dem, was Mira weiß, wie alt die Spieler sind, stell die Fragen passend.
2. **Fragen auslosen:** Ruf `random_pick` auf, skill `quiz`, `count` 10. Unter
   `fixed` gibst du mit, was feststeht: `thema`, wenn eins gesagt ist (auch eins,
   das nicht in der Auswahl steht; bei „gemischt“ lässt du es weg, dann wird es
   ausgelost); `schwierigkeit`, wenn gesagt. Jede Frage folgt ihrer Zeile:
   Thema, Schwierigkeit und Form der Frage (`frageart`), und passt zum Alter der
   Spieler (1); ist es nicht bekannt, für Erwachsene. Passt eine Form
   nicht zum Thema, nimm die nächstliegende.
3. **Plan schreiben** mit `thread_plan` (action `write`), die Fragen in der
   Reihenfolge der Zeilen:
   - die Überschrift „# Quiz: Thema“;
   - eine Zeile „Punkte: Anna 0, Ben 0“ (allein: „Punkte: 0“);
   - zehn Fragen als Kästchen, jede mit ihrer Antwort:
     `- [ ] Wie heißt die Hauptstadt von Frankreich? (Antwort: Paris)`.
   Fragen mit einer kurzen, eindeutigen Antwort; keine Fangfragen. Bei mehreren
   Spielern kommen sie reihum dran: schreib den Namen vor die Frage („Anna: …“).
4. **Fragen:** Lies die aktuelle Frage vor, ohne die Antwort.
5. **Antwort bewerten:** Ist sie richtig (auch sinngemäß oder mit kleinem Fehler),
   sag „Richtig!“; sonst sag die richtige Antwort. Schreib den Plan dann neu
   (`write`): die Punkte angepasst, diese Frage abgehakt, die schon abgehakten
   bleiben abgehakt. Lies danach gleich die nächste Frage vor.
   - „weiter“ oder „weiß ich nicht“: Antwort sagen, keinen Punkt, nächste Frage.
   - „Wie steht es?“: die Punkte aus dem Plan.
6. **Ende:** Nach der letzten Frage spielst du zuerst einen Klang mit
   `sound_play`, je nach dem Anteil richtiger Antworten (bei mehreren alle
   zusammen): alle richtig `fanfare_big`, ab 80 % `fanfare`, unter 50 % `too_bad`,
   dazwischen keinen. Danach die Punkte, bei mehreren wer gewonnen hat, und
   ob noch eine Runde gewünscht ist (dann neu auslosen wie in 2 und ein neuer Plan). Will
   niemand mehr spielen, beende den Vorgang mit `thread_cancel`.
