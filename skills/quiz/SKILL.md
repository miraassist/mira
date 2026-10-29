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
---
# Quiz

Du spielst ein Quiz mit dem Nutzer, allein oder mit mehreren. Das Quiz ist ein
Vorgang mit Plan: Im Plan stehen die Fragen und die Punkte, und im Kontext steht,
welche Frage gerade dran ist.

1. **Vorher kurz klären**, mit einer Frage, nur was nicht gesagt ist: Thema (oder
   „gemischt“) und wer mitspielt (Namen). Spielt nur einer, brauchst du keine Namen.
   Steht in dem, was Mira weiß, wie alt die Spieler sind, stell die Fragen passend.
2. **Plan schreiben** mit `thread_plan` (action `write`):
   - die Überschrift „# Quiz: Thema“;
   - eine Zeile „Punkte: Anna 0, Ben 0“ (allein: „Punkte: 0“);
   - zehn Fragen als Kästchen, jede mit ihrer Antwort:
     `- [ ] Wie heißt die Hauptstadt von Frankreich? (Antwort: Paris)`.
   Fragen mit einer kurzen, eindeutigen Antwort; keine Fangfragen. Bei mehreren
   Spielern kommen sie reihum dran: schreib den Namen vor die Frage („Anna: …“).
3. **Fragen:** Lies die aktuelle Frage vor, ohne die Antwort.
4. **Antwort bewerten:** Ist sie richtig (auch sinngemäß oder mit kleinem Fehler),
   sag „Richtig!“; sonst sag die richtige Antwort. Schreib den Plan dann neu
   (`write`): die Punkte angepasst, diese Frage abgehakt, die schon abgehakten
   bleiben abgehakt. Lies danach gleich die nächste Frage vor.
   - „weiter“ oder „weiß ich nicht“: Antwort sagen, keinen Punkt, nächste Frage.
   - „Wie steht es?“: die Punkte aus dem Plan.
5. **Ende:** Nach der letzten Frage spielst du zuerst einen Klang mit
   `sound_play`, je nach dem Anteil richtiger Antworten (bei mehreren alle
   zusammen): alle richtig `fanfare_big`, ab 80 % `fanfare`, unter 50 % `too_bad`,
   dazwischen keinen. Danach die Punkte, bei mehreren wer gewonnen hat, und
   ob noch eine Runde gewünscht ist (dann neuer Plan mit neuen Fragen). Will
   niemand mehr spielen, beende den Vorgang mit `thread_cancel`.
