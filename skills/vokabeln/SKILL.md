---
name: vokabeln
description: "Vokabeln oder das Einmaleins abfragen und üben („Frag mich meine Englisch-Vokabeln ab“, „Übe mit mir das Siebener-Einmaleins“); falsche kommen am Ende noch einmal."
license: MIT
metadata:
  mira:
    target:
      plan: true
      timeout: 1h
---
# Vokabeln abfragen

Du fragst ab, was der Nutzer lernen will: Vokabeln, das Einmaleins oder eine Liste
von Begriffen. Das Abfragen ist ein Vorgang mit Plan: Im Plan steht die Liste, und
im Kontext steht, was gerade dran ist.

1. **Woher die Liste kommt:**
   - Der Nutzer sagt sie an („dog Hund, cat Katze, …“) oder nennt ein Einmaleins.
   - Oder sie steht in dem, was Mira weiß („die Vokabeln der Woche“): such sie mit
     `memory_search`.
   - Sonst frag kurz, mit einer Frage, wie viele Wörter es sein sollen und ob
     leicht oder schwer, und denk dir dann selbst passende aus: leicht sind
     Alltagswörter (Hund, Haus, Wasser), schwer seltenere (Regenschirm,
     Bürgersteig, ungeduldig). Ist die Fremdsprache nicht gesagt, Englisch.
2. **Richtung** bei Vokabeln: ohne Wunsch gemischt, etwa die Hälfte von Deutsch in
   die Fremdsprache, die andere Hälfte andersherum, durcheinander. Nennt der Nutzer
   eine Richtung, nur diese.
3. **Plan schreiben** mit `thread_plan` (action `write`):
   - die Überschrift „# Vokabeln: Englisch“ (oder „# Einmaleins: 7er-Reihe“);
   - eine Zeile „Richtig: 0 von N“;
   - je Vokabel ein Kästchen mit Frage und Lösung, in der Richtung, in der du
     fragst: `- [ ] Hund (Lösung: dog)`, `- [ ] umbrella (Lösung: Regenschirm)`;
     beim Einmaleins gemischt, nicht der Reihe nach: `- [ ] 7 mal 8 (Lösung: 56)`.
4. **Abfragen:** Lies die aktuelle Frage vor, ohne die Lösung.
5. **Antwort bewerten:** Richtig ist auch, was nur die Spracherkennung verdreht
   hat: ein kleiner Erkennungsfehler („cat“ als „cut“), oder ein Wort der anderen
   Sprache, als ähnlich klingendes Wort der Sprache geschrieben, in der ihr
   sprecht („dog“ als „Dock“, „Haus“ als „house“). Klingt die Antwort wie die
   Lösung, ist sie richtig.
   - Richtig: „Richtig!“; hak das Kästchen ab. War es der erste Versuch (die Zeile
     hat noch kein „✗“), zähl „Richtig“ eins hoch.
   - Falsch: sag die Lösung. Das Kästchen bleibt offen: schieb die Zeile ans Ende
     der Liste und häng „✗“ an, wenn sie noch keins hat
     (`- [ ] Hund (Lösung: dog) ✗`). Keine neue Zeile; die Liste bleibt so lang,
     wie sie ist, und das Wort kommt nach den anderen noch einmal.
   Schreib den Plan dann neu (`write`) und lies gleich die nächste offene Frage vor.
6. **Ende,** wenn alle Kästchen abgehakt sind: Spiel zuerst einen Klang mit
   `sound_play`, je nachdem, wie viele beim ersten Mal richtig waren: alle
   `fanfare_big`, ab 80 % `fanfare`, sonst keinen. Sag dann, wie viele beim
   ersten Mal richtig waren, und nenne die, die schwer waren (die mit „✗“).
   Biete an, sie sich zu merken („Merk dir: Ben übt noch dog und horse“); merk
   sie nur nach Ja, mit memory_save. Beende dann den Vorgang mit
   `thread_cancel`, wenn der Nutzer nicht weitermachen will.
