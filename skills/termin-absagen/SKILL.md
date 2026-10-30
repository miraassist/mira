---
name: termin-absagen
description: Einen Termin aus einem Kalender löschen („Sag meinen Termin morgen beim Arzt ab“, „Lösch den Elternabend“).
license: MIT
metadata:
  mira:
    chapter: "## Kalender und Listen"
    target:
      timeout: 10m
      confirm: true
      fields:
        - name: event
          description: Kennung des Termins aus calendar_events, z. B. t3
          check: calendar_event
        - name: all
          description: Bei Serienterminen "ja", wenn der Nutzer ausdrücklich alle Termine der Serie absagen will; sonst leer
          optional: true
      finish: calendar.delete
---
# Termin absagen

- Such den Termin zuerst mit calendar_events: Zeitraum und Suchwort aus dem, was der Nutzer sagt („morgen“, „Arzt“).
- Genau ein Treffer: trag seine Kennung als event ein. Mehrere: frag kurz, welcher. Keiner: sag es und frag nicht weiter.
- Vor dem Löschen fragt Mira nach („… absagen?“). Erst nach dem Ja mit thread_confirm abschließen.
- Bei einer Serie wird nur dieser eine Termin abgesagt, außer der Nutzer will ausdrücklich alle.
