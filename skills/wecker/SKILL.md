---
name: wecker
description: Einen Wecker stellen, wenn der Nutzer geweckt werden will („Weck mich morgen um 6“, „Stell werktags einen Wecker um halb sieben“, „Weck mich mit SWR3 und mach langsam das Licht an“, „Weck mich mit meiner Morgen-Playlist“, „… mit Queen“). Er klingelt an dem Satelliten, an dem gefragt wurde, mit Miras Weckton, bis der Nutzer das Aktivierungswort sagt. Zeigen und löschen („Welche Wecker habe ich?“, „Lösch den Wecker“) geht mit dem Werkzeug alarm.
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
        - name: music
          description: "Womit der Nutzer statt des Wecktons geweckt werden will, mit dem Namen, den er sagt: ein Radiosender (SWR3), eine Playlist (Morgen-Mix), ein Album (A Night at the Opera), ein Künstler (Queen), ein Lied, auch ein Favorit. Sagt er nichts dergleichen: -, dann klingelt Miras Weckton"
          optional: true
        - name: music_kind
          description: "Was music ist: radio, playlist, album, artist oder track; - wenn er es nicht sagt"
          optional: true
        - name: light
          description: "ja nur, wenn der Nutzer ausdrücklich Licht zum Wecken will („mach langsam das Licht an“); es geht dann vorher langsam an und ist zur Weckzeit ganz an. Sagt er nichts von Licht: -"
          optional: true
      finish:
        action: alarm.set
        data:
          time: "{{ time }}"
          days: "{{ days }}"
          music: "{{ music }}"
          music_kind: "{{ music_kind }}"
          light: "{{ light }}"
          satellite: "{{ satellite }}"
          area: "{{ area }}"
          person: "{{ person }}"
      undo:
        action: alarm.delete_created
        data:
          time: "{{ time }}"
          days: "{{ days }}"
          area: "{{ area }}"
          person: "{{ person }}"
---
# Wecker stellen

- Steht die übliche Zeit der Person in dem, was Mira weiß („steht werktags um 6 auf“), und sagt sie „zur üblichen Zeit“, nimm sie. Sonst frag kurz: „Wann denn?“
- „Morgen“ oder „heute Abend“ ist einmal: days leer lassen. „Werktags“ ist weekdays, einzelne Tage wie mon,wed.
- „Mit SWR3“, „mit meiner Morgen-Playlist“, „mit dem Album …“, „mit Queen“, „mit meinem Lieblingslied …“ ist music, die Art in music_kind; „mach langsam das Licht an“ ist light: ja. Ohne solche Angabe leer lassen, nicht nachfragen.
- Nicht nachfragen, ob es passt: stellen und danach genau sagen, was eingestellt ist, mit Uhrzeit und Tag („Wecker steht: morgen, 6 Uhr.“).
- Korrigiert der Nutzer gleich danach („Nein, halb sieben“), nimm den Wecker mit thread_undo zurück und stell ihn mit dem Richtigen neu; was er nicht ändert (etwa werktags), bleibt.
- Kommt die Bitte nicht von einem Satelliten (Telegram, App), gibt es keinen Wecker: sag, dass das nur an einem Satelliten geht, in dem Raum, wo er klingeln soll.
