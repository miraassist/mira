---
name: briefing
description: "Morgen- und Abendbriefing – was heute (oder morgen) im Haus ansteht: Wetter, Termine, Müllabfuhr, Pakete; Nachrichten nur als zwei Schlagzeilen am Ende. Auch auf Nachfrage („Was steht heute an?“, „Gib mir mein Briefing“). Für mehr Nachrichten ist der Skill nachrichten da."
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
          {{themen}}. Schlagzeilen zu allgemeinen Themen liest du mit web_page von
          festen Seiten: Nachrichten allgemein https://www.tagesschau.de/,
          Deutschland https://www.tagesschau.de/inland, Welt
          https://www.tagesschau.de/ausland, Wirtschaft
          https://www.tagesschau.de/wirtschaft, Wissen https://www.tagesschau.de/wissen,
          Sport und Fußball https://www.sportschau.de/; nur ein Thema ohne feste Seite
          (ein Verein, eine Person) suchst du mit web_search. Fang mit „Guten Morgen“
          nicht an, das sagt Mira selbst.
          Erwähne nur, was es gibt: keine Termine heißt, Termine kommen gar nicht vor
          (kein „keine Termine“), ebenso Müll. Die Schlagzeilen gehören in den
          gesprochenen Text, als ein Satz am Ende. importance 1, spoken höchstens vier
          kurze Sätze zum Vorlesen, details die Termine mit Uhrzeit und die
          Schlagzeilen, expires_in_hours 6.
      - cron: "35 5 * * *"
        per_person: true
        task: >-
          Persönliches Morgen-Briefing für {{person}}. Drei mögliche Teile, alle nur,
          wenn es sie gibt: die Termine von heute aus dem eigenen Kalender
          {{person_calendar}} („-“ heißt: keiner, dann keine Termine); die Pakete, die
          heute kommen, aus dem Kalender „Pakete {{person_login}}“ (nur wenn es ihn
          gibt; „Pakete -“ heißt: keiner), mit Zeitfenster und Shop; und zwei kurze
          Schlagzeilen von heute zu Themen, für die sich {{person}} laut dem, was du
          über sie weißt (unten), interessiert: zu allgemeinen Themen mit web_page von
          den festen Seiten (Deutschland https://www.tagesschau.de/inland, Welt
          https://www.tagesschau.de/ausland, Wirtschaft
          https://www.tagesschau.de/wirtschaft, Wissen https://www.tagesschau.de/wissen,
          Sport https://www.sportschau.de/), zu einem Thema ohne feste Seite (ein
          Verein, eine Person) mit web_search. Kein Wetter,
          kein Müll, keine Themen des Hauses: die kommen im Briefing fürs Haus. Gibt
          es weder Termine noch Pakete noch eigene Themen: importance 0 und spoken leer. Sonst
          importance 1, spoken höchstens drei kurze Sätze an {{person}} gerichtet
          (du-Form, z. B. „Du hast heute um halb zehn Zahnarzt.“), details die Termine
          mit Uhrzeit, die Pakete und die Schlagzeilen, expires_in_hours 8.
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

**Persönlich:** Jede Person, die Mira persönlich erreicht (eigener Kanal wie Handy oder
Telegram, oder „allein zu Hause bin ich“), bekommt morgens zusätzlich eine eigene
Nachricht: ihre Termine, wenn sie einen eigenen Kalender hat (einer, der so heißt wie
sie), und Schlagzeilen zu den Themen, die sie interessieren, wenn Mira das weiß
(„Merk dir, mich interessiert der KSC“). Ohne beides kommt nichts.

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
   Morgen-Briefing fragt; zwei kurze Schlagzeilen. Zu allgemeinen Themen liest du
   sie mit `web_page` von festen Seiten:
   - Nachrichten allgemein: `https://www.tagesschau.de/`
   - Deutschland: `https://www.tagesschau.de/inland`
   - Welt: `https://www.tagesschau.de/ausland`
   - Wirtschaft: `https://www.tagesschau.de/wirtschaft`
   - Wissen: `https://www.tagesschau.de/wissen`
   - Sport und Fußball: `https://www.sportschau.de/`

   Nur ein Thema ohne feste Seite (ein Verein, eine Person) suchst du mit
   `web_search`.

Lass weg, was es nicht gibt, und sag nicht, dass es nichts gibt, außer es gibt gar
nichts. Also nicht „Termine stehen heute nicht an“ oder „Die Müllabfuhr kommt heute
nicht“: Gibt es keine Termine und keinen Müll, kommen beide im Briefing gar nicht vor,
und es bleibt beim Wetter und den Nachrichten. Nur wenn es überhaupt nichts gibt,
sag das in einem Satz. Nenne Uhrzeiten wie „um halb zehn“. Fragt jemand nach Einzelheiten zu einem
Termin, antworte aus dem Kalender.
