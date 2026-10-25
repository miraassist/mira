---
name: urlaub
description: "Anwesenheit vortäuschen, solange niemand zu Hause ist: abends gehen Lichter wechselnd an und aus („Tu so, als wäre jemand da, wenn wir weg sind“, „Richte den Urlaubsmodus ein“). Daraus wird eine Home-Assistant-Automation."
license: MIT
metadata:
  mira:
    target:
      timeout: 15m
      confirm: true
      fields:
        - name: lichter
          description: Die Lichter, die wechselnd an- und ausgehen, mit den Entitäten des Hauses (id oder Name), zwei bis fünf in Räumen, die man von außen sieht
          check: entities
        - name: zeitfenster
          description: Wann es abends läuft, z. B. 18:00 bis 23:00
        - name: abwesend
          description: Woran die Automation erkennt, dass niemand zu Hause ist, z. B. alle Personen nicht zu Hause oder ein Schalter für den Urlaub (Entität)
      finish: tasks.automation
---
# Urlaubsmodus einrichten

Aus dem Wunsch wird eine Automation in Home Assistant: Im Zeitfenster am Abend, solange
niemand zu Hause ist, geht etwa alle 30 Minuten zufällig eines der Lichter an oder aus,
mit einer zufälligen Pause von ein paar Minuten, und am Ende des Zeitfensters gehen alle
aus. Nachrichten schickt sie nicht.

- Schlag zwei bis drei Lichter vor, die man von außen sieht (Wohnzimmer, Küche, Flur),
  aus den Entitäten des Hauses, statt zu fragen. Der Nutzer kann sie ändern.
- Ohne Zeitfenster nimm 18:00 bis 23:00 und sag es im Vorschlag.
- „Niemand zu Hause“ prüft die Automation selbst: alle Personen nicht zu Hause, oder
  ein Schalter für den Urlaub, wenn es einen gibt und der Nutzer ihn nennt.
- Sag im Vorschlag, welche Lichter wann geschaltet werden.
