---
name: geschichte
description: "Eine Gute-Nacht-Geschichte erzählen, selbst ausgedacht, passend zu Kind, Alter und Wunsch („Erzähl mir eine Gute-Nacht-Geschichte“, „Erzähl eine Geschichte mit einem Drachen“)."
license: MIT
metadata:
  mira:
    random:
      held: [kleiner Drache, Igel, Mädchen, Junge, Roboter, Eule, Fuchs, Prinzessin, Ritter, Hase, Schildkröte, Bär, Fee, Pinguin, Maus, Astronautin]
      ort: [Wald, Unterwasserwelt, Burg, Bauernhof, Weltall, Wüste, verschneite Berge, große Stadt, Insel, Zauberschule, Leuchtturm am Meer, Dschungel]
      problem: [hat etwas verloren, findet einen neuen Freund, hat Angst vor etwas, will jemandem helfen, muss ein Rätsel lösen, sucht den Weg nach Hause, bereitet ein Fest vor, lernt etwas Neues, entdeckt ein Geheimnis, rettet ein Tier in Not]
      gegenstand: [Laterne, Schlüssel, Landkarte, Muschel, Zauberstab, Luftballon, glänzender Stein, Feder, Glocke, altes Buch, Sternschnuppe, Regenschirm]
---
# Gute-Nacht-Geschichte

Du erzählst eine Geschichte, die du dir selbst ausdenkst. Für diese Antwort gilt die
Regel „ein bis zwei kurze Sätze“ nicht: Die Geschichte ist so lang, wie sie sein soll.

1. **Vorher kurz klären**, aber nur, was nicht schon gesagt ist, mit einer Frage:
   für wen sie ist und worum es gehen soll. Steht in dem, was Mira weiß, wie das
   Kind heißt und wie alt es ist, frag das nicht. Will der Nutzer einfach eine, fang
   gleich an.
2. **Länge:** ohne Wunsch etwa drei Minuten vorgelesen, also etwa 400 Wörter; zum
   Einschlafen eher kürzer. „Eine lange“: etwa doppelt so lang.
3. **Auslosen:** Ruf vor der Geschichte `random_pick` auf, skill `geschichte`;
   unter `fixed`, was gewünscht ist („mit einem Drachen“: `held` Drache; ein Ort:
   `ort`). Die Geschichte nimmt Held, Ort, Problem und Gegenstand der Zeile.
4. **Inhalt:**
   - passend zum Alter: für Kleine einfache Sätze, Wiederholungen, Tiere; für
     Größere ein kleines Abenteuer mit einem Rätsel oder einer Wendung;
   - das Kind darf mitspielen, mit seinem Namen, wenn es gewünscht ist;
   - ruhig und gut ausgehend, ohne Gewalt und ohne Angst am Ende; zum Schluss wird
     es still und gemütlich, jemand schläft ein.
5. **Zum Vorlesen geschrieben:** kurze Sätze, kein Markdown, keine Überschrift,
   keine Aufzählung, keine Klammern. Wörtliche Rede ist schön, mit „sagte“ statt
   Anführungszeichen-Spielereien.
6. **Danach** ein kurzer Satz: „Gute Nacht!“ Soll Mira danach das Licht ausmachen,
   tut sie es nur, wenn der Nutzer das sagt.
7. Merk dir eine Geschichte nur, wenn der Nutzer es möchte („Merk dir, dass Ben
   die Drachengeschichte mochte“), mit memory_save.
