---
name: wecker
description: Einen Wecker stellen, wenn der Nutzer geweckt werden will („Weck mich morgen um 6“, „Stell einen Wecker“). Er klingelt an dem Satelliten, an dem gefragt wurde, wenn das Gerät Wecker kann (z. B. Echo Show mit TECHO5).
license: MIT
metadata:
  mira:
    chapter: "## Erinnerungen, Timer und Wecker"
    target:
      timeout: 5m
      fields:
        - name: time
          description: Uhrzeit im Format HH:MM, z. B. 06:00 oder 06:30
        - name: days
          description: Wiederholung, z. B. weekdays oder mon,wed; leer für einmal (zum nächsten Mal dieser Uhrzeit)
          optional: true
      finish:
        action: esphome.{{ satellite.device }}_alarm_set
        data:
          time: "{{ time }}"
          days: "{{ days }}"
          label: Mira
      undo:
        action: esphome.{{ satellite.device }}_alarm_delete
        data:
          time: "{{ time }}"
          label: ""
---
# Wecker stellen

- Steht die übliche Zeit der Person in dem, was Mira weiß („steht werktags um 6 auf“), und sagt sie „zur üblichen Zeit“, nimm sie. Sonst frag kurz: „Wann denn?“
- „Morgen“ oder „heute Abend“ ist einmal: days leer lassen. „Werktags“ ist weekdays, einzelne Tage wie mon,wed.
- Nicht nachfragen, ob es passt: stellen und danach genau sagen, was eingestellt ist, mit Uhrzeit und Tag („Wecker steht: morgen, 6 Uhr.“).
- Korrigiert der Nutzer gleich danach („Nein, halb sieben“), nimm den Wecker mit thread_undo zurück und stell ihn mit dem Richtigen neu.
