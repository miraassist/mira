---
name: kamera
description: "Eine Kamera auf dem Bildschirm zeigen („Zeig mir die Haustür“, „Zeig die Gartenkamera“) oder fragen, was eine Kamera gerade sieht („Wer steht vor der Tür?“, „Ist das Garagentor zu?“)."
license: MIT
metadata:
  mira:
    settings:
      skript:
        description: Für Fragen an eine Kamera ein Skript in Home Assistant, für Assist freigegeben, mit den Feldern kamera und frage, das die Antwort zurückgibt (Vorlage im Guide), z. B. script.kamera_fragen. Leer heißt, Mira zeigt Kameras nur.
    target:
      timeout: 2m
      fields:
        - name: kamera
          description: Die Kamera, die gezeigt werden soll, als Entität (camera.…)
          check: entities
      finish:
        action: esphome.{{ satellite.device }}_home_show_camera
        data:
          entity: "{{ kamera }}"
          seconds: 60
---
# Kamera

**Welche Kamera:** Sieh mit GetLiveContext nach, welche Kameras es gibt (camera.…), und
nimm die Entität genau so, wie sie dort steht; rate keine. Gibt es keine passende, sag,
dass die Kamera für Assist freigegeben sein muss.

**Zeigen** („Zeig mir die Haustür“): Trag die Kamera ein, statt zu fragen. Sie erscheint dann eine Minute lang auf
dem Bildschirm des Satelliten, an dem gefragt wurde. Sag danach kurz, welche Kamera du
zeigst („Hier ist die Haustür.“). Hat der Satellit keinen Bildschirm, sag das ehrlich.

**Fragen** („Wer steht vor der Tür?“, „Ist das Garagentor zu?“): Dafür ist kein Vorgang
nötig. Ruf das Skript „{{skript}}“ mit kamera (die Entität) und frage (die Frage des
Nutzers) auf; es schaut sich das Bild an und gibt die Antwort zurück. Sag, was es
zurückgibt, kurz und als das, was die Kamera gerade zeigt; lies nicht mehr hinein.
Lässt sich die Frage über einen Zustand im Haus beantworten (ein Sensor am
Garagentor), nimm lieber den.

Steht oben kein Skript, kannst du nicht selbst ins Bild schauen: Sag das und biete an,
die Kamera zu zeigen.
