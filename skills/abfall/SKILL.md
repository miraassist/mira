---
name: abfall
description: Abfuhrtermine aus dem Abfallkalender – wann welcher Müll abgeholt wird; erinnert am Vorabend.
license: MIT
metadata:
  mira:
    settings:
      calendar:
        description: Der Kalender mit den Abfuhrterminen in Home Assistant, Name oder Entität (z. B. calendar.abfuhrtermine). Er muss für Assist freigegeben sein.
        required: true
      types:
        description: Abfallarten, die interessieren, als Wortteile mit Komma, z. B. "Restabfall, Wertstoff". Leer heißt alle.
    schedules:
      - cron: "0 18 * * *"
        task: >-
          Prüfe mit calendar_events (calendar "{{calendar}}", from und to beide der Tag
          von morgen), ob morgen etwas zu den interessanten Abfallarten abgeholt wird.
          Nimm nur, was diese Abfrage liefert. Ist nichts Passendes dabei: importance 0
          und spoken leer. Sonst: importance 1, spoken nennt kurz, was morgen abgeholt
          wird, z. B. "Morgen wird der Restabfall abgeholt. Denk daran, die Tonne
          rauszustellen."
---
# Abfall

Der Kalender "{{calendar}}" enthält die Abfuhrtermine dieses Haushalts. Lies ihn mit
`calendar_events` (calendar: "{{calendar}}"), statt Termine zu schätzen.

Interessante Abfallarten: {{types}}
Termine anderer Arten (etwa die mobile Schadstoffsammlung) erwähnst du nur,
wenn der Nutzer ausdrücklich danach fragt.

- "Wann kommt der Restmüll?": den nächsten passenden Termin mit Wochentag und Datum nennen; frag dafür einen längeren Zeitraum ab (to in vier Wochen).
- Ist der nächste Termin heute, weißt du nicht, ob schon abgeholt wurde. Sag dann: heute, und falls
  noch nicht abgeholt, schnell die Tonne rausstellen. Nenne zusätzlich den Termin danach.
- Nenne nur Termine, die calendar_events geliefert hat. Rechne keine Termine aus Rhythmen hoch.
- "Was wird diese Woche abgeholt?": die passenden Termine der nächsten sieben Tage.
- Die Namen im Kalender sind lang ("Restabfall (privat) 2-Rad 02-woechentl."); sag einfach "Restabfall" oder "Wertstoff".
- Wenn der Nutzer erinnert werden möchte, stell mit reminder_set eine Erinnerung für den Vorabend.
