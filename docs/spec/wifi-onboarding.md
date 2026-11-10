# Bringing a Wi-Fi device into the Wi-Fi by voice – Spec

Status 2026-11-10: steps 2 and 3 built against fakes (not released): NetworkManager and the Supervisor, the four families, the tools and the thread handing over to the found devices; NetworkManager's API checked read-only on a desktop. Joining a device's Wi-Fi and the hand-over to a real device are open (step 1, with the maintainer). Step 5 (a connected Wi-Fi Home Assistant does not run over) built 2026-11-10. Scenarios:
[wifi-onboarding.feature](wifi-onboarding.feature).

## Goal

A new Wi-Fi device in its setup mode opens a Wi-Fi of its own (`ShellyPlus1-A8032AB12345`, `tasmota-1A2B3C-4567`).
Today someone joins it with a phone, opens a page or the maker's app and types the home Wi-Fi's password. Mira does
that with the Wi-Fi of the machine she runs on, while that machine is in the network by cable:

1. Stefan: "Ich hab einen neuen Shelly." Mira: "Ich sehe ihn: Shelly Plus 1. Soll ich ihn ins WLAN holen?"
   Stefan: "Ja."
2. Mira joins the Shelly's Wi-Fi, gives it the home Wi-Fi's name and password, leaves it again. A moment later Home
   Assistant discovers the Shelly in the home Wi-Fi, and [Found devices](found-devices.md) takes over: "Er ist im
   WLAN. In welchen Raum kommt er?"

This is the first half (into the network) for Wi-Fi devices, as pairing is for Zigbee
([Adding a Zigbee device](device-onboarding.md)); the second half is the found devices'.

## What is known (2026-11-10)

The four big families that set up over a Wi-Fi of their own (not Bluetooth, not a maker's cloud):

| Family | Its Wi-Fi in setup mode | Address | Handing over the home Wi-Fi | Source |
|---|---|---|---|---|
| **Shelly** (Gen1) | open, `shelly<model>-<id>` (e.g. `shellyplug-s-A1B2C3`) | `192.168.33.1` | `GET /settings/sta?enabled=1&ssid=<ssid>&key=<password>` | Shelly's API docs (Gen1); assumption for the exact path until tried |
| **Shelly** (Gen2, Gen3, Gen4: Plus, Pro, Mini) | open by default (Gen3 and later can have a password), `Shelly<Model>-<id>` | `192.168.33.1` | `POST /rpc` `{"method":"WiFi.SetConfig","params":{"config":{"sta":{"ssid":…,"pass":…,"enable":true}}}}`; the answer says whether a restart is needed (`restart_required`) | [Shelly Gen2 API, WiFi](https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi) |
| **Tasmota** | open, its host name `tasmota-<id>-<nnnn>`, for 3 minutes after the start without a Wi-Fi | `192.168.4.1` | With the broker (decision 3): `GET /cm?cmnd=Backlog SSID1 <ssid>; Password1 <password>; MqttHost <host>; MqttUser <user>; MqttPassword <password>` (assumption: the command page answers while the Wi-Fi manager runs; to check in step 1). Its Wi-Fi manager's form `GET /wi?s1=<ssid>&p1=<password>&save=` takes only the Wi-Fi | Tasmota's source (`xdrv_01_9_webserver.ino`: page `wi`, arguments `s1`, `p1`, `save`; the AP opens with the host name and no passphrase) |
| **ESPHome** (with `captive_portal`, the default of the ready-made projects) | its fallback hotspot, the name its configuration gives (often "<Name> Fallback Hotspot"), after 1 minute without its Wi-Fi; only open ones (decision 4) | `192.168.4.1` | `GET` or `POST /wifisave?ssid=<ssid>&psk=<password>` | ESPHome's source (`captive_portal.cpp`, `handle_wifisave`, arguments `ssid`, `psk`) |
| **WLED** | `WLED-AP`, password `wled1234` | `4.3.2.1` | `POST /json/cfg` `{"nw":{"ins":[{"ssid":…,"psk":…}]}}` | WLED's source (`const.h`: `DEFAULT_AP_SSID`, `DEFAULT_AP_PASS`; `wled.cpp`: `softAPConfig(4.3.2.1)`; `cfg.cpp`: `nw.ins[].ssid`, `psk`) |

Not possible this way: devices that set up through a maker's cloud (Tuya/Smart Life, Meross, Govee, WiZ, eWeLink's
stock firmware), and devices that set up by Bluetooth only (Matter over Wi-Fi, newer Shellys also offer Bluetooth:
that is the later part on Bluetooth).

**After the hand-over** each joins the home Wi-Fi and is found by Home Assistant:
- Shelly, ESPHome, WLED announce themselves (Zeroconf); Home Assistant discovers them, and they are
  [found devices](found-devices.md) that wait for a yes. An ESPHome device with an encryption key asks for it: that
  step has a field and is left to Home Assistant's page (found devices, decision on fields).
- **Tasmota** is found by Home Assistant only through MQTT: Mira gives it the broker with the Wi-Fi (decision 3), and
  Home Assistant's Tasmota integration adds it by itself: a new device without a room.

## How Mira reaches the Wi-Fi

Mira needs to scan for networks, join an open one (or one with a known password) for a minute, and leave it,
without touching the machine's way into the home network. Two ways, chosen by what Mira finds when she starts; no
setting:

| Where Mira runs | How | What it needs |
|---|---|---|
| **The HA app on Home Assistant OS** | (Not tried on a real one: no Home Assistant OS at hand. The Supervisor has no signal for "connected": Mira reads the interface's state once a second, up to 20 s, a poll as nothing reports it; its access points do not say whether a network is open, the families' are taken as known.) The Supervisor's network API: `GET /network/info` (the interfaces, which one is the primary), `GET /network/interface/<wlan>/accesspoints` (the scan), `POST /network/interface/<wlan>/update` with `wifi: {mode: infrastructure, ssid, auth: open or wpa-psk, psk}` and `ipv4: {method: auto}` to join, and `enabled: false` to leave | `hassio_api: true` in the app's config and the role that may use `/network` (the Supervisor's security middleware: `manager` is the smallest; built 2026-11-10 in `app/config.yaml`). The Supervisor token is in the app's environment |
| **A container or the binary on a Linux machine with NetworkManager** (the usual desktop and server setup; the live house runs Mira as a container with the host's network) | NetworkManager over D-Bus: the devices and the primary connection, `RequestScan` and the access points (the property `LastScan` changes when a scan is done: a signal), `AddAndActivateConnection` with a profile that never becomes the default route (`ipv4.never-default`, `ipv6.never-default`, `connection.autoconnect: false`), the active connection's state `activated` as the signal it is up, then `Deactivate` and `Delete` of the profile | The system bus: for a container the socket `/run/dbus/system_bus_socket` mounted in; the right to manage networks: root, or a polkit rule for Mira's user on the actions `org.freedesktop.NetworkManager.*` (checked 2026-11-10 on Ubuntu: the group `netdev` is not enough, its rule covers only changing system connections from a local active session; a scan from a process outside one is refused `not authorized`); a container with the host's network (`network_mode: host`) or a route to the device's address through the host |
| Anything else (no NetworkManager, macOS, Windows, a container without the bus) | – | The function is off; `mira check` and the skill say why |

**A usable Wi-Fi** (decision 6). The function works with a Wi-Fi interface that is present and switched on, and
either not connected, or connected while Mira's way to Home Assistant runs over another interface (the cable): the
interface the machine would use to reach Home Assistant's address is read from the routing table (no packet is
sent). When that way runs over the Wi-Fi, the function is off; leaving that Wi-Fi would cut Home Assistant off.
Borrowing a connected Wi-Fi drops what runs over it for the minute of the hand-over; afterwards Mira only leaves the
device's Wi-Fi, and the machine's own connection comes back through NetworkManager's automatic connecting (Mira
neither restores nor watches it: a connection that does not connect by itself stays off).

**The route.** The device's network is joined so that it never becomes the default route: Home Assistant, the model
and the home network stay on the cable. When the device's network has the same addresses as the home network
(`192.168.4.x` at home and a Tasmota), the hand-over is refused, and Mira says why (decision 5).

## The home Wi-Fi

Mira keeps the home Wi-Fi's name and password: **one setting, used here and later for Bluetooth set-ups.** In
`mira.yaml` they are the skill's settings `wlan_ssid` and `wlan_password` (`geraet-einbinden: {wlan_ssid: …,
wlan_password: ${WLAN_PASSWORD}}`; a skill's settings are flat), the password a secret setting, in the secrets file
like every secret and on Mira's page under the skill with a password field. The broker for Tasmota (decision 3) is
`mqtt_host`, `mqtt_user` and `mqtt_password`, the same way. It is never said, never
put into a model's context, never logged; a tool result says "the home Wi-Fi" by its name only.

Before the hand-over Mira checks the scan for the home Wi-Fi: is it there, and on 2.4 GHz (these devices have no
5 GHz)? When it is not: the hand-over is not tried, Mira says why ("Dein WLAN sehe ich hier nur mit 5 GHz; das
können solche Geräte nicht.").

## What it does

### The tools (skill `geraet-einbinden`)

| Tool | What it does step by step | Cost |
|---|---|---|
| `wifi_devices` | Scans and returns the devices in setup mode that it recognises (the table above, by the name of their Wi-Fi), with family, model when the name says it, and signal; and whether the home Wi-Fi is seen on 2.4 GHz. None: returns how to put the common ones into setup mode (below) | One scan: NetworkManager 2–10 s until `LastScan` changes (a signal; 15 s as the safety net); the Supervisor's access point list. The turn waits for it |
| `wifi_handover` (device) | Starts the thread of `geraet-einbinden` at the turn's place, with the plan (into the Wi-Fi, confirm, room, name). Then, after the turn's answer is said: (1) joins the device's Wi-Fi with a profile that never becomes the default route; waits for it to be up (`activated`, 20 s safety); (2) sends the home Wi-Fi by the family's method (one HTTP call, 10 s); (3) reads the answer: success, or the device's error; (4) leaves the device's Wi-Fi and deletes the profile, always, also after an error; (5) ticks "into the Wi-Fi" and waits for Home Assistant to discover the device in the home Wi-Fi: a found device ([Found devices](found-devices.md)) whose discovery names the same device (Shelly and ESPHome: the MAC in the Wi-Fi's name and in the discovery; WLED: the next discovered WLED), up to 2 minutes (a safety net; the signal is the discovered flow). (6) Then the found device is the thread's, and Mira asks at the satellite: "Er ist im WLAN. Soll ich ihn einbinden?" or straight on to the room when the discovery needs no confirmation | Joining and leaving the Wi-Fi: a few D-Bus or Supervisor calls; one HTTP call to the device; the wait for the discovery. The turn waits for none of it: Mira says "Ich hole ihn ins WLAN, einen Moment" and the rest runs in the thread |

The rest (confirm, room, name, expose, ignore) are the found devices' tools.

**Setup mode**, said when nothing is found:
- Shelly: a new one is in it; else hold its button 10 seconds (a switch: toggle it 5 times).
- Tasmota: a fresh one is in it for 3 minutes after power on; else press its button 6 times.
- ESPHome: switched on without its Wi-Fi, after a minute.
- WLED: a new one, or one that does not find its Wi-Fi.

### When a step fails

| Fails | Who learns of it |
|---|---|
| No usable Wi-Fi interface (none, switched off, or carrying the way to Home Assistant), no NetworkManager, no right to change the network | The skill's Wi-Fi tools are not offered (whether the Wi-Fi is usable is read at most once a minute, as tools are offered at every turn); asked, Mira says the function needs a Wi-Fi on Mira's machine that Home Assistant does not run over (the skill says so). Log `wifi onboarding off` with the reason at the start, `wifi onboarding not offered` with the reason when the Wi-Fi is not usable. Not in `mira check` (built 2026-11-10: the log only) |
| The routing table cannot be read, or Home Assistant's address not resolved | The Wi-Fi counts as not usable when it is connected; the log line `wifi onboarding not offered` names the error |
| No home Wi-Fi set | `wifi_devices` returns that the home Wi-Fi is missing and where it is set (Mira's page, the skill); Mira says so |
| The home Wi-Fi is not seen, or only on 5 GHz | The hand-over is not tried; Mira says why |
| The scan fails or gets no answer in 15 s | The tool returns the error; Mira says so. Log `wifi scan failed` |
| Joining the device's Wi-Fi fails, or is not up within 20 s | The profile is deleted; Mira says she could not reach the device, and to keep it close or try again. Log `could not join the device's wifi` |
| The device refuses or does not answer in 10 s | Mira leaves its Wi-Fi; she says the device did not take the Wi-Fi; the thread waits for "nochmal" or "abbrechen". Log `the device did not take the wifi` with its answer |
| Leaving the device's Wi-Fi or deleting the profile fails | Log `could not leave the device's wifi` with the error, at error level: the machine may stay joined; a repair says so until the next start removes Mira's profiles (they carry Mira's name) |
| The device is not discovered within 2 minutes | Mira says it did not turn up in the home Wi-Fi: most often a wrong password. Log `wifi device not discovered after the handover`. The thread waits for "nochmal" (the device opens its Wi-Fi again by itself after failing, Tasmota after 3 minutes, ESPHome after 1) or "abbrechen" |
| No MQTT broker set when a Tasmota is found | `wifi_devices` returns that the broker is missing and where it is set; Mira says so and does not hand over the Wi-Fi alone |

### Cost

Per device: one scan, joining and leaving one Wi-Fi, one HTTP call to the device, the found devices' calls. Nothing
runs when nobody asks: no scanning on a schedule (a device in setup mode reports nothing; Mira scans when asked).

## Building blocks

- **A thread with a plan, bound to the room** of `geraet-einbinden`, as for Zigbee: the first step waits for a signal
  from outside (the discovery), held by the code.
- **[Found devices](found-devices.md)** for everything after the device is in the home Wi-Fi.
- **Departure: Mira changes the machine's network.** Home Assistant offers no Wi-Fi set-up of devices; its
  Supervisor offers the machine's Wi-Fi on Home Assistant OS, NetworkManager elsewhere. Mira uses them only for the
  minute of the hand-over, never as the default route, and always leaves again.
- **Departure: a client per family.** Four small ones (the table), each one HTTP call with the family's documented or
  source-given request; nothing else of the devices is talked to (their own settings are Home Assistant's
  integrations').

## Testing

- **Go tests**: NetworkManager and the Supervisor faked behind one interface; each family's device as an HTTP test
  server that answers as its source does; every failure row.
- **On this machine**: the test Mira as the binary with NetworkManager over D-Bus, the test HA in its container; a
  real Shelly and a Tasmota or ESPHome device reset to setup mode, with the maintainer.
- **Home Assistant OS**: not at hand; the Supervisor path is tested against the Supervisor's API as documented, and
  is marked so until someone runs it.

## Decided (2026-11-10)

| # | Question | Decision |
|---|---|---|
| 1 | D-Bus in Go | A library for the system bus (`github.com/godbus/dbus`, pure Go); no `nmcli` |
| 2 | The set-up | The guide gives the container recipe (the socket mounted in, the group or a polkit rule) and the HA app gets the Supervisor's role it needs |
| 3 | Tasmota and MQTT | Mira hands over the MQTT broker too: a second setting next to the home Wi-Fi (`mqtt: {host, user, password}`, the password a secret), since Home Assistant does not give its MQTT integration's password out. Tasmota then announces itself to Home Assistant's Tasmota integration, and is a new device without a room ([Found devices](found-devices.md)) |
| 4 | ESPHome devices with a password on their fallback Wi-Fi | Only open fallback Wi-Fis |
| 5 | The same addresses at home and in the device's network | The hand-over is refused, and Mira says why |
| 6 | A connected Wi-Fi (2026-11-10) | Usable when Mira's way to Home Assistant runs over another interface (on Home Assistant OS: when the Wi-Fi is not the primary interface); Mira does not restore the machine's own Wi-Fi connection afterwards, NetworkManager's automatic connecting does |

## Steps

1. **Check on this machine**: scan, join an open Wi-Fi with never-default, leave, over D-Bus; the route stays on the
   cable (2026-11-10: the API read-only on a desktop whose Wi-Fi is in use: the Wi-Fi device type 2, its state 100,
   `LastScan`, the access points' `Ssid`, `Frequency`, `WpaFlags`, `RsnFlags`; joining needs a free Wi-Fi and is
   open). A real Shelly in setup mode: its Wi-Fi, the hand-over by `WiFi.SetConfig`, its discovery in the test HA; a Tasmota in its Wi-Fi manager: does `/cm` with `Backlog` answer (decision 3).
2. **The Wi-Fi part** in the service: NetworkManager, the Supervisor, the free interface, the four families, the
   home Wi-Fi and the MQTT broker settings. WL-01 to WL-07 (built 2026-11-10, `internal/wifi`).
3. **The tools and the thread**, handing over to the found devices (`Await`: the discovered flow of the family's
   integration whose id or name holds the device's MAC from its Wi-Fi's name; for Tasmota a new device of the maker
   "Tasmota"); end to end with a real device (open). WL-08 to WL-10 (built 2026-11-10).
4. **The guide**: the home Wi-Fi, the container recipe, which devices.
5. **A connected Wi-Fi that Home Assistant does not run over** (decision 6): with NetworkManager the route to Home
   Assistant's address decides (the interface whose address the machine would send from). On Home Assistant OS,
   Home Assistant is reached over the Supervisor's internal network, never over the Wi-Fi; there the Supervisor's
   primary interface decides: a connected Wi-Fi that is not the primary interface is usable (decided 2026-11-10).
   WL-11 (built 2026-11-10).
