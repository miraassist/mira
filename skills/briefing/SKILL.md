---
name: briefing
description: "Morgen- und Abendbriefing – was heute (oder morgen) ansteht: Wetter, Termine, Müllabfuhr, auf Wunsch Nachrichten. Auch auf Nachfrage („Was steht heute an?“, „Gib mir mein Briefing“)."
license: MIT
metadata:
  mira:
    settings:
      kalender:
        description: 'Die Kalender fürs Haus-Briefing, Namen mit Komma, z. B. "Familie". Leer heißt alle freigegebenen Kalender, auch persönliche; dann hört das ganze Haus deren Termine.' 
      themen:
        description: 'Nachrichten-Themen fürs Morgen-Briefing, mit Komma, z. B. "Deutschland, Fußball". Leer heißt keine Nachrichten.' 
    schedules:
      - cron: "30 5 * * *"
        task: >-
          Stell das Morgen-Briefing für heute zusammen, fürs ganze Haus (es wird
          angesagt, wenn jemand den Raum betritt, oder nebenbei erwähnt). Nur mit
          dem, was die Werkzeuge liefern: das Wetter, wie Home Assistant es jetzt
          meldet; die Termine von heute aus diesen Kalendern: {{kalender}} (leer heißt
          alle); was heute an Müll abgeholt wird, falls es einen Abfallkalender gibt;
          und, nur wenn hier Themen stehen, zwei kurze Schlagzeilen von heute zu:
          {{themen}}. Fang mit „Guten Morgen“ nicht an, das sagt Mira selbst.
          Erwähne nur, was es gibt: keine Termine heißt, Termine kommen gar nicht vor
          (kein „keine Termine“), ebenso Müll. Die Schlagzeilen gehören in den
          gesprochenen Text, als ein Satz am Ende. importance 1, spoken höchstens vier
          kurze Sätze zum Vorlesen, details die Termine mit Uhrzeit und die
          Schlagzeilen, expires_in_hours 6.
      - cron: "35 5 * * *"
        per_person: true
        task: >-
          Persönliches Morgen-Briefing für {{person}}: nur die Termine von heute aus
          dem Kalender {{person_calendar}}, der {{person}} gehört. Kein Wetter, kein
          Müll, keine Nachrichten: die kommen im Briefing fürs Haus. Gibt es heute
          keine Termine darin: importance 0 und spoken leer. Sonst importance 1,
          spoken ein bis zwei kurze Sätze an {{person}} gerichtet (du-Form, z. B.
          „Du hast heute um halb zehn Zahnarzt.“), details die Termine mit Uhrzeit,
          expires_in_hours 8.
      - cron: "0 18 * * *"
        task: >-
          Stell das Abend-Briefing für morgen zusammen, fürs ganze Haus. Nur mit dem,
          was die Werkzeuge liefern: die Termine von morgen aus diesen Kalendern:
          {{kalender}} (leer heißt alle); was morgen an Müll abgeholt wird, falls es
          einen Abfallkalender gibt; und das Wetter, falls Home Assistant eines für
          morgen nennt. Keine Nachrichten. Gibt es morgen nichts davon: importance 0
          und spoken leer. Sonst importance 1, spoken höchstens drei kurze Sätze,
          details die Termine mit Uhrzeit, expires_in_hours 5.
---
# Briefing

Ein Briefing ist eine kurze Übersicht, was ansteht. Es soll in einem Atemzug
gesprochen sein: drei bis fünf kurze Sätze, keine Aufzählung mit Spiegelstrichen.

**Persönlich:** Wer in Home Assistant einen eigenen Kalender hat (einer, der so heißt
wie die Person), bekommt morgens zusätzlich eine eigene Nachricht mit seinen Terminen,
über seine Kanäle (Handy, Telegram) oder am Satelliten, wenn er dort erkannt ist.

**Auf Nachfrage** („Was steht heute an?“, „Gib mir mein Briefing“, „Was ist morgen
los?“) stellst du es jetzt zusammen, für heute oder für den Tag, nach dem gefragt wird:

1. Wetter: wie Home Assistant es jetzt meldet. Erfinde keine Vorhersage.
2. Termine mit `calendar_events` für den Tag:
   - Kalender fürs Haus: {{kalender}} (leer heißt alle).
   - Weißt du, wer spricht, dazu den Kalender dieser Person: der, der so heißt wie
     sie (Annas Kalender heißt „Anna“). Die persönlichen
     Kalender anderer Personen nimmst du nicht.
   - Ist niemand erkannt, nur die Kalender fürs Haus.
3. Müll: was an dem Tag abgeholt wird, falls es einen Abfallkalender gibt.
4. Nachrichten: nur wenn hier Themen stehen ({{themen}}) und der Nutzer nach dem
   Morgen-Briefing fragt; zwei kurze Schlagzeilen über die Internetsuche.

Lass weg, was es nicht gibt, und sag nicht, dass es nichts gibt, außer es gibt gar
nichts. Nenne Uhrzeiten wie „um halb zehn“. Fragt jemand nach Einzelheiten zu einem
Termin, antworte aus dem Kalender.
