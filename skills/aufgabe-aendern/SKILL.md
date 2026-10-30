---
name: aufgabe-aendern
description: Eine bestehende Aufgabe ändern, auch eine, die der Haushalt selbst in Home Assistant angelegt hat („Das Abendlicht soll erst um 19 Uhr angehen“, „Bei der Aufgabe mit dem Wasser nimm den Sensor raus“, „Ändere die Aufgabe …“). Zeiten, Sonnenstand, Bedingungen und was sie schaltet.
license: MIT
metadata:
  mira:
    chapter: "## Mira passt aufs Haus auf"
    target:
      timeout: 15m
      confirm: true
      fields:
        - name: aufgabe
          description: Welche Aufgabe, mit ihrem Namen, ihrem Bereich oder dem, was sie tut
          check: tasks.find
        - name: aenderung
          description: Was sich ändern soll, in den Worten des Nutzers und vollständig („erst 30 Minuten nach Sonnenuntergang“)
      finish: tasks.edit
---
# Aufgabe ändern

Mira ändert eine Aufgabe in Home Assistant: ihre eigenen und die, die der Haushalt im Automationseditor angelegt hat. Du sammelst, welche Aufgabe und was sich ändern soll; den Entwurf macht Mira danach selbst und meldet sich mit einem Vorschlag.

- Trag mit thread_fill ein, was schon klar ist. Die Aufgabe schlägst du über das Feld aufgabe nach, statt zu fragen: passt keine, nennt die Prüfung die Aufgaben im Haus; nimm dann den Namen der passenden.
- Frag nur nach, was unklar ist, kurz und mit einem Vorschlag: welche von zwei Aufgaben gemeint ist, oder wie viel „später“ sein soll.
- Formuliere den Vorschlag nie selbst; er kommt aus dem Vorgang.
