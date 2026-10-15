---
name: abfall
description: Abfuhrtermine aus dem Abfallkalender – wann welcher Müll abgeholt wird; erinnert am Vorabend.
license: MIT
metadata:
  mira:
    settings:
      calendar_url:
        description: iCal-Adresse des Abfallkalenders (beim Entsorger als Abo/Sync-Link erhältlich)
        required: true
        secret: true
      types:
        description: Abfallarten, die interessieren, als Wortteile mit Komma, z. B. "Restabfall, Wertstoff". Leer heißt alle.
    calendars:
      abfall: calendar_url
    schedules:
      - cron: "0 18 * * *"
        task: >-
          Prüfe mit calendar_events (Kalender "abfall", days 3), ob ein Termin mit "morgen"
          davor zu den interessanten Abfallarten passt. Termine von heute zählen nicht, die
          Tonne steht schon draußen. Wenn ja: importance 1, spoken nennt kurz,
          was morgen abgeholt wird, z. B. "Morgen wird der Restabfall abgeholt. Denk daran, die
          Tonne rauszustellen." Wenn nicht: importance 0 und spoken leer.
---
# Abfall

Der Kalender "abfall" enthält die Abfuhrtermine dieses Haushalts. Lies ihn mit
`calendar_events` (calendar: "abfall"), statt Termine zu schätzen.

Interessante Abfallarten: {{types}}
Termine anderer Arten (etwa die mobile Schadstoffsammlung) erwähnst du nur,
wenn der Nutzer ausdrücklich danach fragt.

- "Wann kommt der Restmüll?": den nächsten passenden Termin mit Wochentag und Datum nennen.
- Ist der nächste Termin heute, weißt du nicht, ob schon abgeholt wurde. Sag dann: heute, und falls
  noch nicht abgeholt, schnell die Tonne rausstellen. Nenne zusätzlich den Termin danach.
- Nenne nur Termine, die calendar_events geliefert hat. Rechne keine Termine aus Rhythmen hoch.
- "Was wird diese Woche abgeholt?": die passenden Termine der nächsten sieben Tage.
- Die Namen im Kalender sind lang ("Restabfall (privat) 2-Rad 02-woechentl."); sag einfach "Restabfall" oder "Wertstoff".
- Wenn der Nutzer erinnert werden möchte, stell mit reminder_set eine Erinnerung für den Vorabend.
