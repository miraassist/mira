---
name: geraet-einbinden
description: "Ein neues Zigbee-Gerät einbinden („Ich hab ein neues Zigbee-Gerät“, „Bind eine neue Steckdose ein“): über Zigbee2MQTT anmelden, einem Raum zuordnen, benennen und für Sprache freigeben."
license: MIT
metadata:
  mira:
    target:
      plan: true
      bound: room
      timeout: 15m
      done_after: 2m
    agents:
      geraet-einbinden.frage:
        description: Formuliert die Frage nach dem Raum für ein neu gefundenes Zigbee-Gerät.
        context: background
        tools: []
        instructions: |-
          Du bekommst die Angaben zu einem Zigbee-Gerät, das gerade neu eingebunden
          wurde: Hersteller, Modell, die englische Beschreibung von Zigbee2MQTT und
          seine Werte. Schreib genau zwei kurze Sätze auf Deutsch zum Vorlesen, ohne
          Markdown: Im ersten sagst du, was gefunden wurde, so wie man es im Alltag
          nennt („ein Temperatur- und Feuchtesensor von Aqara“, „eine Steckdose mit
          Strommessung von IKEA“), ohne Modellnummer. Im zweiten fragst du, in welchen
          Raum es kommt. Beginne mit „Ich hab's:“. Gib nur diese zwei Sätze zurück.
---
# Gerät einbinden

Ein neues Zigbee-Gerät kommt über Zigbee2MQTT in Home Assistant. Du führst durch
die Schritte; Mira wartet zwischendurch selbst auf das Gerät.

1. **Anmelden.** Will der Nutzer ein neues Gerät einbinden und hat es noch nicht
   ausdrücklich gesagt, frag kurz: „Soll ich das Einbinden starten?“ Beim Ja:
   `zigbee_pairing_start`. Sag dann nur, was das Ergebnis sagt (Knopf gedrückt
   halten, bis es schnell blinkt, oder neu einstecken), und frag nichts weiter. Die Runde endet.
2. **Das Gerät ist da.** Mira meldet sich am Satelliten von selbst mit der Frage
   nach dem Raum; die Angaben zum Gerät stehen dann im Vorgang. Hat Mira kein
   Gerät gesehen und fragt, ob sie es nochmal versuchen soll: beim Ja wieder
   `zigbee_pairing_start`, beim Nein `thread_cancel`.
3. **Raum.** Die Antwort ist der Raum: `device_assign` mit dem Raum, wie der
   Nutzer ihn nennt. Gibt es den Raum nicht, nenn die Räume aus dem Ergebnis und
   frag nach.
4. **Name.** Schlag einen Namen vor: was es ist und der Raum, kurz und auf
   Deutsch („Temperatursensor Wohnzimmer“, „Steckdose Küche“). Frag, ob er
   passt. Beim Ja, oder mit dem Namen des Nutzers: `device_finish` mit dem Namen.
   Sag dann kurz, was das Ergebnis sagt.
5. **Abbrechen** („abbrechen“, „lass mal“): `thread_cancel`; das Einbinden endet.

Ein Gerät pro Vorgang. Für das nächste sagt der Nutzer wieder, dass er ein neues
Gerät hat. Ein Gerät, das schon einen Namen bekommen hat, ist fertig: Sagt der
Nutzer danach wieder, er hat ein neues Gerät, ist es ein weiteres, und es geht
bei Schritt 1 los, auch wenn es im Gespräch eben schon um ein Gerät ging. Andere Funkarten (Matter, Z-Wave, WLAN) gehen damit nicht; sag das,
wenn danach gefragt wird.
