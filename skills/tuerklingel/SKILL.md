---
name: tuerklingel
description: "Die Türklingel einrichten: wenn es klingelt, sagt Mira es an und zeigt das Bild der Türkamera auf den Echo Show („Sag Bescheid, wenn es klingelt“, „Zeig mir bei Klingeln die Haustür“). Daraus wird eine Home-Assistant-Automation."
license: MIT
metadata:
  mira:
    settings:
      bildschirme:
        description: Die Echo Show mit TECHO5, auf denen beim Klingeln das Bild der Türkamera erscheint, als ESPHome-Namen mit Komma, z. B. "show, show_bad". Leer heißt, Mira sagt das Klingeln nur an.
    target:
      timeout: 15m
      confirm: true
      fields:
        - name: klingel
          description: Die Türklingel, die Entität, die beim Klingeln anschlägt (binary_sensor, event oder button des Klingeltasters)
          check: entities
        - name: kamera
          description: Die Kamera an der Tür (camera.…), "-" wenn keine
        - name: zeigen
          description: Die Aktionen für das Bild, wörtlich nach der Anleitung, "-" wenn keine
      finish: tasks.automation
---
# Türklingel einrichten

Aus dem Wunsch wird eine Automation in Home Assistant: Schlägt die Klingel an, sagt Mira
sofort an allen Satelliten „Es hat an der Tür geklingelt.“ (mira.send_message,
importance 3), und auf den Echo Show erscheint das Bild der Türkamera.

- Such die Klingel und die Türkamera unter den Entitäten des Hauses, statt zu fragen.
  Gibt es mehrere Klingeln, frag kurz, welche.
- Trag in **zeigen** für jeden dieser Bildschirme eine Aktion ein: {{bildschirme}}.
  Für einen Namen NAME lautet sie wörtlich
  `esphome.NAME_home_show_camera mit entity KAMERA und seconds 60`, KAMERA ist die
  Türkamera. Ohne Kamera oder ohne Bildschirme: "-".
- Nenn im Vorschlag, was passiert: Ansage und auf welchen Bildschirmen das Bild kommt.
