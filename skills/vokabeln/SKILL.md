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
   - Sonst frag, was abgefragt werden soll. Erfinde keine Schulvokabeln.
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
5. **Antwort bewerten:** Richtig (auch mit kleinem Aussprachefehler, den die
   Spracherkennung gemacht haben kann): „Richtig!“. Falsch: sag die Lösung und
   häng die Vokabel noch einmal ans Ende der Liste. Schreib den Plan dann neu
   (`write`): diese Frage abgehakt, die abgehakten bleiben abgehakt, „Richtig“
   angepasst. Lies gleich die nächste Frage vor.
6. **Ende:** Spiel zuerst einen Klang mit `sound_play`, je nachdem, wie viele
   beim ersten Mal richtig waren: alle `fanfare_big`, ab 80 % `fanfare`, sonst
   keinen. Sag dann, wie viele beim ersten Mal richtig waren, und nenne die, die
   schwer waren. Biete an, sie sich zu merken („Merk dir: Ben übt noch dog und
   horse“); merk sie nur nach Ja, mit memory_save. Beende dann den Vorgang mit
   `thread_cancel`, wenn der Nutzer nicht weitermachen will.
