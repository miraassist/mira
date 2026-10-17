# Skills mit HA-Aktionen – Spezifikation

Stand 2026-10-17, gebaut in 0.6.18. Baut auf [Vorgänge](vorgaenge.md) und [Aufgaben](aufgaben.md) auf. Beispiel: der Wecker.

## Grundsatz

Ein Auftrag wird nicht zwingend zur Automation. Ein **Skill** beschreibt das Anliegen:
- woran Mira es erkennt;
- welche Angaben sie braucht, als Felder des Vorgangs;
- wie es am Ende umgesetzt wird.

Eine Automation (`tasks.automation`) ist dabei nur eine Möglichkeit. Eine andere ist, **sofort eine HA-Aktion aufzurufen**, die der Skill selbst festlegt. So wird eine Gerätefunktion zu einem Skill, ohne Code: der Wecker der Show, ein Timer, „zeig die Kamera“.

Der Skill kommt vom Betreiber, der weiß, was er tut. Die Aktion legt der Skill fest, nicht das Modell; das Modell füllt nur die Felder.

## Der Abschluss `action`

Neben den eingebauten Abschlüssen (`memory.save`, `tasks.automation`) kann ein Skill eine oder mehrere HA-Aktionen als Abschluss angeben:

```yaml
metadata:
  mira:
    target:
      timeout: 5m
      fields:
        - name: time
          description: Uhrzeit, HH:MM
        - name: days
          description: Wochentage, leer für einmalig
          optional: true
        - name: label
          description: Bezeichnung, z. B. „Arbeit“
          optional: true
      finish:
        action: esphome.{{ satellite.device }}_alarm_set
        data:
          time: "{{ time }}"
          days: "{{ days }}"
          label: "{{ label }}"
---
# Wecker
Erkennen: „Weck mich …“, „Stell einen Wecker …“. Steht die übliche Zeit im Gedächtnis, nimm sie; sonst frag kurz: „Wann denn?“
Sag danach, was du gestellt hast: Uhrzeit, Tag, Raum.
```

- **Leere Werte bleiben stehen**: ESPHome-Aktionen wollen jedes Feld, und leer bedeutet etwas (bei der Show heißt `days` leer: einmal, zum nächsten Mal dieser Uhrzeit; sonst `weekdays` oder `mon,wed`).
- **Platzhalter** in `action` und `data`: die Felder des Vorgangs sowie der **Ort**, also `area` (ID und Name), `satellite` (Entität), `satellite.device` (ESPHome-Gerätename), sowie `person`, falls bekannt.
- **Gibt es die Aktion nicht** (z. B. ein Echo mit echolocal hat keinen Wecker), sagt Mira ehrlich, dass es an diesem Satelliten nicht geht. Einen festgelegten Ersatzweg gibt es nicht. Findet das Modell mit seinen Werkzeugen selbst einen passenden Weg, darf es ihn vorschlagen.
- **Der Abschluss läuft sofort im Gespräch**, wie `memory.save`, nicht im Hintergrund.
- Die Sperrliste (`security.confirm_domains`, `confirm_entities`, Label „Mira: nachfragen“) gilt auch hier.

## Bestätigen: optional, wie bisher

`confirm` ist schon heute pro Skill einstellbar.
- **`confirm: true`**: Mira fragt vor dem Aufruf („Ich wecke dich morgen um 6, passt das?“).
- **Ohne `confirm`**: Mira führt aus und sagt, was sie getan hat („Wecker steht: morgen 6 Uhr, Schlafzimmer.“). So macht es Alexa beim Wecker, und das passt fast immer.

Für den Wecker gilt: ohne `confirm`.

## Korrigieren statt neu anfangen

Hat Mira falsch verstanden, korrigiert man im Gespräch: „Nein, halb sieben.“ Mira kennt aus dem Verlauf, was sie eben gestellt hat. Der Skill kann dafür eine zweite Aktion nennen, die das Gestellte zurücknimmt, etwa `alarm_delete` mit Zeit und Bezeichnung. Mira nimmt dann zurück und stellt neu, ohne dass man alles wiederholen muss.

```yaml
      undo:
        action: esphome.{{ satellite.device }}_alarm_delete
        data: { time: "{{ time }}", label: "" }
```

Das Werkzeug dafür heißt `thread_undo`: Es nimmt die neueste ausgeführte Aktion eines Vorgangs am selben Ort aus den letzten 15 Minuten zurück; danach startet Mira den Vorgang mit dem Richtigen neu. Ausprobiert an der Show: `alarm_delete` löscht nur mit leerer Bezeichnung, mit Bezeichnung nichts.

## Szenarien

- „Weck mich morgen zur üblichen Zeit“ am Satelliten im Schlafzimmer, Person bekannt, im Gedächtnis „steht werktags um 6 auf“: Mira stellt 6:00 an der Show im Schlafzimmer und sagt es.
- „Bitte weck mich morgen“: „Wann denn?“ – „Um 6“: Mira stellt 6:00 und sagt es.
- „Nein, halb sieben“: Mira nimmt 6:00 zurück und stellt 6:30.
- Am Echo mit echolocal: „Hier kann ich keinen Wecker stellen.“

## Dafür nötig (einmal, allgemein)

1. Der Abschluss `action` (und `undo`) mit Platzhaltern.
2. Der Ort als Platzhalter: Bereich, Satellit, ESPHome-Gerät. Dieselbe Angabe soll auch in den Entwurf von Aufgaben (siehe Liste: Ort und Person in den Entwurfs-Kontext).

## Bewusst nicht

- **Eine Weckmelodie über den Media-Player** als Ersatz: vorerst ausgeklammert.
- **Prüfen, was ein Skill-Autor aufruft**: Er ist der Betreiber.
