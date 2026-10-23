---
name: termin
description: Einen Termin in einen Kalender eintragen („Trag mir Freitag um 15 Uhr Zahnarzt ein“, „Am 3. November ist Elternabend“).
license: MIT
metadata:
  mira:
    target:
      timeout: 10m
      fields:
        - name: title
          description: Titel des Termins, kurz, z. B. Zahnarzt
        - name: start
          description: Beginn als YYYY-MM-DD HH:MM; bei einem ganztägigen Termin leer lassen und day ausfüllen
          optional: true
        - name: end
          description: Ende als YYYY-MM-DD HH:MM; ohne Angabe eine Stunde nach Beginn
          optional: true
        - name: day
          description: Nur bei ganztägigen Terminen der Tag als YYYY-MM-DD, sonst leer
          optional: true
        - name: day_after
          description: Nur bei ganztägigen Terminen der Tag nach dem letzten Tag als YYYY-MM-DD (bei einem Tag also der nächste), sonst leer
          optional: true
        - name: location
          description: Ort, falls genannt
          optional: true
        - name: calendar
          description: Kalender (Name). Steht im Gedächtnis ein Standard-Kalender der Person, nimm ihn; sonst "standard"
          check: calendar_writable
      finish:
        action: calendar.create_event
        omit_empty: true
        data:
          entity_id: "{{ calendar }}"
          summary: "{{ title }}"
          start_date_time: "{{ start }}"
          end_date_time: "{{ end }}"
          start_date: "{{ day }}"
          end_date: "{{ day_after }}"
          location: "{{ location }}"
      undo:
        action: calendar.delete_created
        omit_empty: true
        data:
          entity_id: "{{ calendar }}"
          summary: "{{ title }}"
          start_date_time: "{{ start }}"
          start_date: "{{ day }}"
---
# Termin eintragen

- Rechne relative Angaben („Freitag“, „übermorgen“, „nächste Woche Dienstag“) mit dem heutigen Datum aus.
- Entweder start und end (mit Uhrzeit) oder day und day_after (ganztägig), nie beides.
- Fehlt die Uhrzeit und ist es kein ganztägiger Termin (Geburtstag, Urlaub), frag kurz: „Um wie viel Uhr?“
- Kalender: Hat der Nutzer einen genannt, nimm ihn. Sonst den Standard aus dem Gedächtnis, sonst "standard". Fragt die Prüfung zurück, welcher Kalender, frag den Nutzer; nach seiner Antwort biete an, dir das als Standard zu merken (memory_save, z. B. „Annas Standard-Kalender ist Familie.“).
- Nicht nachfragen, ob es passt: eintragen und danach genau sagen, was eingetragen ist: Titel, Tag, Uhrzeit, Kalender.
- Korrigiert der Nutzer gleich danach („Nein, um 16 Uhr“), nimm den Termin mit thread_undo zurück und trag ihn mit dem Richtigen neu ein, ohne Rückfrage. Klappt das Zurücknehmen nicht, sag das, statt einen zweiten Termin einzutragen.
