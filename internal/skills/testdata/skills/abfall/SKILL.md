---
name: abfall
description: Abfuhrtermine aus dem Abfallkalender – wann welcher Müll abgeholt wird; erinnert am Vorabend.
license: MIT
metadata:
  mira:
    settings:
      calendar:
        description: Der Kalender mit den Abfuhrterminen in Home Assistant (Name oder Entität)
        required: true
      types:
        description: Abfallarten, die interessieren, als Wortteile mit Komma, z. B. "Restabfall, Wertstoff". Leer heißt alle.
    schedules:
      - cron: "0 18 * * *"
        task: >-
          Prüfe mit calendar_events (calendar "{{calendar}}", nur morgen), ob morgen etwas
          zu den interessanten Abfallarten abgeholt wird.
---
# Abfall

Der Kalender "{{calendar}}" enthält die Abfuhrtermine. Interessante Abfallarten: {{types}}
