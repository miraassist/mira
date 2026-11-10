---
name: geraet-einbinden
description: "Neue Geräte einbinden: ein Zigbee-Gerät koppeln („Ich hab ein neues Zigbee-Gerät“), Geräte, die Home Assistant gefunden hat („Gibt's neue Geräte?“, die Antwort auf Miras Nachricht über ein neues Gerät), einem Raum zuordnen, benennen und für Sprache freigeben."
license: MIT
metadata:
  mira:
    settings:
      wlan_ssid:
        description: Der Name deines WLANs (2,4 GHz). Damit richtet Mira Geräte ein, die nach dem WLAN fragen.
      wlan_password:
        description: Das Passwort deines WLANs. Mira sagt es nie und gibt es nur an Geräte weiter, die du einbindest.
        secret: true
      mqtt_host:
        description: Der MQTT-Broker für Tasmota-Geräte (Adresse, wie Home Assistant ihn nutzt). Nur für Tasmota nötig.
      mqtt_user:
        description: Der Benutzer am MQTT-Broker für Tasmota-Geräte.
      mqtt_password:
        description: Das Passwort am MQTT-Broker für Tasmota-Geräte.
        secret: true
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

Neue Geräte kommen auf drei Wegen: Ein **Zigbee-Gerät** koppelt Mira über
Zigbee2MQTT oder ZHA. Ein **WLAN-Gerät** im Einrichtungsmodus (Shelly, Tasmota,
ESPHome, WLED) holt Mira mit dem WLAN ihres Rechners ins Heim-WLAN. Ein Gerät, das
**Home Assistant gefunden** hat (eine Bridge im Netz,
ein Bluetooth-Sensor, eine Lampe aus der App des Herstellers), meldet Mira mit einer
Nachricht. Danach ist es immer gleich: Raum, Name, fertig.

## Zigbee-Gerät koppeln

1. **Anmelden.** Will der Nutzer ein neues Zigbee-Gerät einbinden und hat es noch
   nicht ausdrücklich gesagt, frag kurz: „Soll ich das Einbinden starten?“ Beim Ja:
   `zigbee_pairing_start`. Sag dann nur, was das Ergebnis sagt (Knopf gedrückt
   halten, bis es schnell blinkt, oder neu einstecken), und frag nichts weiter. Die
   Runde endet.
2. **Das Gerät ist da.** Meldet sich ein Gerät an, sagt Mira von selbst, dass sie es
   kennenlernt; eine Antwort darauf („okay“) braucht nichts, sag höchstens kurz, dass
   du dich meldest. Ist es fertig, meldet sich Mira am Satelliten von selbst mit der
   Frage nach dem Raum; die Angaben zum Gerät stehen dann im Vorgang. Hat Mira kein
   Gerät gesehen und fragt, ob sie es nochmal versuchen soll: beim Ja wieder
   `zigbee_pairing_start`, beim Nein `thread_cancel`.

Gibt es Zigbee2MQTT und ZHA, fragt das Ergebnis von `zigbee_pairing_start`, in
welches Netz; ruf es dann mit `network` auf.

## WLAN-Gerät ins WLAN holen

- „Ich hab einen neuen Shelly“ (oder Tasmota, WLED, ein ESPHome-Gerät):
  `wifi_devices`. Nenn, was im Einrichtungsmodus zu sehen ist, und frag, ob du es
  ins WLAN holen sollst. Ist nichts zu sehen, sag, wie man es in den
  Einrichtungsmodus bringt (steht im Ergebnis).
- Beim Ja: `wifi_handover` mit dem WLAN-Namen des Geräts. Sag nur, dass es einen
  Moment dauert; Mira meldet sich danach von selbst („… ist jetzt im WLAN. Soll ich
  es einbinden?“). Beim Ja dann `device_confirm`, danach Raum und Name. Fragt sie
  stattdessen gleich nach dem Raum, geht es mit dem Raum weiter.
- Das WLAN-Passwort sagst du nie.
- Hast du `wifi_devices` nicht, geht es gerade nicht: Miras Rechner braucht dafür ein
  WLAN, über das er nicht selbst mit Home Assistant verbunden ist (er hängt per Kabel
  im Netz). Sag das; sonst geht es mit der App des Herstellers.

## Gefundene Geräte

- **Miras Nachricht** („Ich habe ein neues Gerät gefunden: … Soll ich es
  einbinden?“) nennt in ihren Details die Kennungen (`flow:…` oder `device:…`).
  Beim Ja: `device_adopt` mit der Kennung; bei mehreren frag, welches zuerst, oder
  nimm sie nacheinander. Beim Nein („nein“, „ignorieren“, „das ist vom Nachbarn“):
  `device_ignore`. Lies die Kennungen nie vor.
- **„Gibt's neue Geräte?“**: `devices_found`; nenn die Geräte mit Namen und frag,
  welches eingebunden werden soll.
- **Ein entdecktes Gerät** (`flow:…`) braucht erst sein Ja in Home Assistant: Das
  Ergebnis von `device_adopt` sagt, was Home Assistant fragt. Ist es nur eine
  Bestätigung, ruf gleich `device_confirm` auf. Verlangt es etwas vom Nutzer (den
  Knopf auf der Bridge drücken), sag es kurz und ruf `device_confirm` nach seinem
  „erledigt“ auf. Braucht es Angaben, die nur in Home Assistant gehen, sag das; der
  Vorgang ist dann zu Ende.
- Danach geht es mit dem Raum weiter.

## Raum und Name

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
von vorn los, auch wenn es im Gespräch eben schon um ein Gerät ging. Matter und
Z-Wave gehen damit nicht; sag das, wenn danach gefragt wird.
