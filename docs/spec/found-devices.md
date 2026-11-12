# Devices Home Assistant found – Spec

Status 2026-11-12: steps 1 to 4 built and released (0.21.0); decision 9 (the iBeacon filter) released in 0.22.1. Built: the fake discovery, the state table checked against it, the noticing, the tools and the skill, run end to end at the fake Show; the real run is open. Scenarios: [found-devices.feature](found-devices.feature).

## Goal

A device that turns up in Home Assistant is noticed by Mira, and she helps to take it in by voice, however it came:
paired in the Hue app, flashed with Tasmota, a Shelly in the Wi-Fi, a Bluetooth sensor nearby, a bridge Home
Assistant discovered. This is the second half of [Adding a device](device-onboarding.md): there Mira brings the
device into its network; here it is in Home Assistant already, or Home Assistant waits for a yes, and what is left is
the yes, the room, the name and exposing it.

1. Mira, when someone is in the kitchen: "Ich habe ein neues Gerät gefunden: eine Hue-Lampe. Soll ich sie
   einbinden?" Stefan: "Ja."
2. Mira: "In welchen Raum kommt sie?" Stefan: "Küche." Mira: "Ich nenne sie ‚Lampe Küche', passt das?" Stefan: "Ja."
3. Mira: "Fertig: Lampe Küche, in der Küche, und ich kann sie jetzt schalten."

Or: "Nein, das ist vom Nachbarn." Mira: "Gut, ich ignoriere es." She never mentions it again, and Home Assistant
shows it as ignored. Or nobody answers: Mira does not ask again; "Gibt's neue Geräte?" lists it.

## Decided (2026-11-10)

| # | Question | Decision |
|---|---|---|
| 1 | Reminding when nobody reacts | Mira says a found device **once**. After that only when asked ("Gibt's neue Geräte?"), with everything still open |
| 2 | Remembering an ignored device that is in Home Assistant already | A label **"Mira: ignoriert"** on the device in Home Assistant: visible there, removed there to undo it. A device Home Assistant discovered and waits for is ignored with **Home Assistant's own ignore** |
| 3 | Who may take a device in or ignore it | Anyone at a satellite, as anyone may switch a light ([Adding a device](device-onboarding.md), decision 3) |
| 4 | Which devices | Both kinds: what Home Assistant **discovered** and waits for a yes for, and a **new device without a room** |
| 5 | Exposing, naming | As for a Zigbee device: every entity except configuration and diagnostic ones; Home Assistant's name of the device (`name_by_user`), and its entity ids follow the new name as with ZHA (`light.tasmota` becomes `light.lampe_wohnzimmer`; decided 2026-11-10 after the Tasmota run); a device of Zigbee2MQTT is renamed there (`homeassistant_rename`), so its entity ids follow |
| 6 | Where it lives | In the skill `geraet-einbinden`: noticing runs only while the skill is on. A household that never turned it on hears nothing new |
| 7 | The room of a Bluetooth device | Asked, as for every device; no proposal from the Bluetooth proxies' signal (decided 2026-11-10) |
| 8 | A step that asks for the Wi-Fi | A step whose only fields are the Wi-Fi's name and password (a Bluetooth set-up: Improv, a Shelly) is filled from the home Wi-Fi Mira keeps (the skill's settings `wlan_ssid` and `wlan_password`; [Wi-Fi onboarding](wifi-onboarding.md), the home Wi-Fi) and submitted after the user's yes; the one exception to "a step with fields stays in Home Assistant" (decided 2026-11-10) |
| 9 | Beacons that pass by | A device of the **iBeacon integration** (`ibeacon`, "iBeacon Tracker") is not counted: it makes a device for every named Bluetooth beacon it sees once, also for the car driving by (a speed camera warner, a driving log plug); seen in the live house 2026-11-12, 33 such devices in three days. Its entities count as diagnostic ones, so the device has nothing that would be exposed. Other Bluetooth integrations are not filtered: Bermuda makes devices only for those its user chose, Private BLE Device only for those added by hand (decided 2026-11-12) |

## The two kinds

| Kind | What it is | How Mira learns of it | Ignoring |
|---|---|---|---|
| **Discovered** | A config flow Home Assistant started by itself (Bluetooth, DHCP, Zeroconf, SSDP, USB, MQTT, HomeKit, the Supervisor, an integration's discovery) and that waits under Settings → Devices & services → "Discovered": a Hue Bridge, a Shelly, a BTHome sensor, an ESPHome device | `config_entries/flow/subscribe`: the open flows at once, then each one added and removed | Home Assistant's ignore (`config_entries/ignore_flow`): the flow goes, Home Assistant never offers it again, "Ignored" in its integrations page undoes it |
| **New without a room** | A device the device registry creates while Mira runs, that has no area: a lamp the Hue app paired, a Tasmota plug, a device a discovered flow just added | the device registry's `create` events (the subscription Mira's structure has) | the label "Mira: ignoriert" on the device |

**Not counted** (no message, not in the list):
- a flow of Mira's own integration, a flow a user started (source `user`, `import`, `reauth`, `reconfigure`);
- a device that is disabled, a service (`entry_type: service`), a device without an entity that would be exposed (only
  configuration and diagnostic ones), a device with the label, a device that got an area by now;
- a device of the iBeacon integration (decision 9): its entities count as diagnostic ones;
- a device a Zigbee pairing of `geraet-einbinden` is waiting for: that dialog has it already;
- what was open already when the noticing ran for the first time: Home Assistant's backlog is not read out. It is
  marked said at once and is in the list on request.

## Building blocks

- **A message to the house** ([Messages](messages.md)): what is found is said as a message, not as an interruption at
  a satellite: the notifier says it where someone is, not in quiet hours, and it shows on the page. Several found in
  one go are one message.
- **The answer to an announced message** ([Threads](threads.md), Assignment 3a; the turn after an announcement sees
  the message and its details): "Ja" after the message is the next turn's affair; the message's details name the
  found devices by their ids, and the skill `geraet-einbinden`.
- **A thread with a plan, bound to the room** ([Adding a device](device-onboarding.md), Building blocks):
  taking one device in is a thread of `geraet-einbinden` at the satellite where the user said yes, with the steps
  (confirm in Home Assistant when discovered,) room, name. The tools `device_assign` and `device_finish` are the
  Zigbee dialog's, for any device.
- **Home Assistant's own APIs**, nothing rebuilt: its config flows for the yes (the frontend's REST API), its ignore,
  its label registry and device registry.
- **Departure:** a thread whose first step is a config flow of Home Assistant. Mira submits only steps that ask for
  nothing (a confirmation, "press the button on the bridge, then submit"); a step with fields (a password, a key, a
  choice) is said and left to Home Assistant's page.

## What it does

### Noticing

1. **Watching.** While the skill is on, Mira subscribes to the open config flows (`config_entries/flow/subscribe`)
   and takes the device registry's `create` events from the subscription her structure has. Handlers only pass the
   change on.
2. **Gathering.** A found item waits 60 s after the last one before it is said, so a bridge that brings fifty lamps
   is one message. A time, because nothing says that a burst is over (a time as the last resort, where no
   signal exists). For a new device Mira checks at the end of the 60 s that it still has no area and is
   counted (the list above): one read of the device registry (`config/device_registry/list`), one of the entities
   (`config/entity_registry/list_for_display`: which device has an entity without a category that is not hidden) and
   one of the labels (`config/label_registry/list`: the id of "Mira: ignoriert").
3. **Naming.** A discovered flow is named by its `title_placeholders.name`, else by the integration's name
   (`manifest/get` of its domain, once per domain); a device by its name, with its manufacturer and model as the
   facts. No model round: the message is a fixed sentence with the names.
4. **The message.** One message to the house: "Ich habe ein neues Gerät gefunden: <name>. Soll ich es einbinden?",
   for several "Ich habe <n> neue Geräte gefunden: <a>, <b> und <c>. Soll ich sie einbinden?" (more than three:
   the first three "und <n> weitere"). Its details name the skill and each found item with its id (`flow:<flow_id>`,
   `device:<device_id>`) and kind. Each item is marked **said** in Mira's database (by the flow's `unique_id`, else
   its handler and `discovery_key`; by the device id), so it is never said again, also after a restart.
5. **The answer.** The next turn sees the message (Assignment 3a). "Ja" opens the skill (`skill_open`), and the model
   calls `device_adopt` with the item; for several, the skill asks which first, or takes them one by one. "Nein",
   "ignorieren", "das ist vom Nachbarn": `device_ignore`. No answer: nothing; the message stays on the page as every
   message does.

### The tools

| Tool | What it does step by step | Cost |
|---|---|---|
| `devices_found` | Lists what is open: discovered flows that are counted, and new devices without a room since the noticing began, said or not | No call to Home Assistant: the open flows are kept from the subscription, the devices from the structure Mira keeps. Grows with what is open |
| `device_adopt` (item) | Starts the thread of `geraet-einbinden` at the turn's place (or takes the open one) with the plan. A **device**: notes its facts, ticks the first step, returns "ask for the room". A **discovered flow**: reads its current step (`GET /api/config/config_entries/flow/<flow_id>`) and the step's text in the household's language (`frontend/get_translations`, category `config`, the integration; once per domain); a step without fields is submitted after the user's yes (below); a step that asks only for the Wi-Fi (decision 8) is filled from the home Wi-Fi after the yes, without the password in the tool's result; any other step with fields is returned as "only in Home Assistant", with where | One or two REST calls, one translation fetch per domain; the turn waits for them (well under a second) |
| `device_confirm` | For a discovered flow: submits its current step without fields (`POST …/flow/<flow_id>` with `{}`). The next step: another without fields is returned with its text (the model says it, "press the button on the bridge", and waits for "erledigt"); a step that asks only for the Wi-Fi is filled (decision 8); another step with fields as "only in Home Assistant"; an error of the step (`errors`, "the button was not pressed") is said, the step stays; `create_entry` ends the flow: Mira waits up to 10 s for the devices the entry brings (registry `create` events; a signal, the time a safety net). One device: it is the thread's device, the room is next. Several: Mira says how many and that they are in the list; the thread is done. None (an integration without devices): done | One POST, then the wait |
| `device_assign` (room) | As in the Zigbee dialog: the area of the thread's device | One device registry update |
| `device_finish` (name) | As in the Zigbee dialog: exposes, then names. A device of Zigbee2MQTT is renamed there; any other gets `name_by_user` (`config/device_registry/update`), and each entity id that begins with the device's name before gets the new name instead (`config/entity_registry/update` with `new_entity_id`, as Home Assistant's own rename of a device offers; the name before unknown: a log line, the ids stay) | Like the Zigbee dialog's; for others one update instead of the publish, plus one read of the device's entities and one update per entity that is renamed |
| `device_ignore` (item) | A **discovered flow**: `config_entries/ignore_flow` with its title. A **device**: the label "Mira: ignoriert" (created in the label registry the first time, `config/label_registry/create`), added to the device's labels (`config/device_registry/update`). Ends the thread if it was the thread's | One or two calls |

### When a step fails

| Fails | Who learns of it |
|---|---|
| The subscription to the flows fails | Log `could not watch discovered devices` with the error; the next reconnect tries again. Discovered flows are not noticed meanwhile; new devices still are |
| The message cannot be delivered | As every message: the notifier logs it and keeps it waiting |
| The said mark cannot be written | Log `could not remember a found device as said`; the item may be said once more after a restart |
| The flow is gone when the user says yes (added or ignored in Home Assistant meanwhile) | `device_adopt` returns that it is no longer open; Mira says so. Log none: not an error |
| Home Assistant refuses a step, or its answer is not understood | The tool returns Home Assistant's error; Mira says it, the flow stays open in Home Assistant. Log `could not confirm the discovered device` |
| A step asks for the Wi-Fi, and no home Wi-Fi is set | Mira says the home Wi-Fi is missing and where it is set; the flow stays open |
| A step needs other fields | Not an error: Mira says it is set up in Home Assistant, Settings → Devices & services → Discovered, and the thread is done |
| No device comes within 10 s after `create_entry` | Mira says the integration is set up; devices that come later are found like any other. Log `discovered device set up, no device yet` |
| Ignoring is refused | The tool returns Home Assistant's error; Mira says so; the item stays open. Log `could not ignore the found device` |
| The area, the expose or the name is refused | As in the Zigbee dialog |

### Cost

While the skill is on: one more subscription (the flows); the registry events Mira has already. Per burst of found
items with a new device: three reads (devices, entities, labels; each grows with the house, read once per burst),
at most one `manifest/get` per new domain, one message. Per device taken in:
the calls of the tools above. Nothing on a schedule; nothing grows with the number of devices in the house, only with
what is found.

## State table

Each row is checked on the test HA before the code waiting for it is built.

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | Open flows at the start | the subscription | one event with every open flow (`type: null`), then the result | `config_entries/flow/subscribe` | 2026-11-10 (a Zeroconf flow of another Mira, `step_id: token`) |
| 2 | A flow discovered | the device turns up in the network | event `type: added` with the flow, its `step_id` and `title_placeholders` | the same subscription | 2026-11-10 (the fake, all four kinds) |
| 3 | A flow ends | added, ignored or dismissed | event `type: removed` | the same subscription | 2026-11-10 |
| 4 | The step of a flow | – | `GET /api/config/config_entries/flow/<id>`: `type: form`, `step_id`, `data_schema` (empty: no fields), `errors`, `description_placeholders` | REST with the owner's token | 2026-11-10 (`data_schema` with one required field `token`) |
| 5 | A step submitted | `POST …/flow/<id>` `{}` | the next step, an error in `errors` (the same form, `errors: {base: …}`), or `type: create_entry` with the entry (`result.entry_id`); the entry's device is created **before** the answer comes | the POST's answer; the entry's devices from the device registry (`config_entries`) | 2026-11-10 (a button step answered `not_pressed` until pressed, then `create_entry`) |
| 6 | Ignored | `config_entries/ignore_flow` | the flow `removed` (row 3); an entry with source `ignore` | the subscription | 2026-11-10 |
| 7 | A device created | an integration adds it | `device_registry_updated` `create` | the subscription of the structure | 2026-11-09 (Zigbee dialog) |
| 8 | Labelled | `config/label_registry/create` (the id is made from the name: `mira_ignoriert`), `config/device_registry/update` with `labels` | the device's `labels` hold the label's id | the update's answer | 2026-11-10 |

Interrupting: "abbrechen" cancels the thread; a flow stays open in Home Assistant, a device as it is. The wake word
during the message is an ordinary interruption of a message.

## Testing

- **A fake discovery** in the test HA (an addition to `testenv/z2m_fake` or a small integration of its own): a test
  action starts a discovered flow of three kinds: a confirmation only; a step "press the button" that fails with an
  error until a second test action presses it; a step with a field. Adding it creates a device without an area. A
  second test action creates a device without a flow, as an app would.
- **A Wi-Fi step**: the fake discovery gets a fourth kind, a step with the fields `ssid` and `password`, as Improv's.
  Which field names count as the Wi-Fi's is checked against Home Assistant's Improv and Shelly flows (assumption until
  then: Improv's step `provision` with `ssid` and `password`).
- **A real device's Zeroconf announcement** (2026-11-10): the test HA runs in a bridge network and hears no
  announcement of the LAN, on purpose (it would find the whole house). `found_fake.zeroconf` hands one announcement
  to a real integration (`domain`, `name`, `host`, `port`, `type`), as Home Assistant's zeroconf would;
  `testenv/zeroconf_relay.py` listens on the machine (`avahi-browse`, pushed, no polling) for exactly the one device a
  test names (a part of its name, its MAC) and calls it once when the device announces itself in the home network:
  only on the interface of the default route (joined to the device's own Wi-Fi, the machine hears it there too), and
  not what avahi had cached before the start (a device reset since stays there until its record expires). For the Wi-Fi hand-over of a Shelly
  ([Wi-Fi onboarding](wifi-onboarding.md)).
- **The real run**: a device of another kind at the live house, with the maintainer (a Bluetooth sensor, a Shelly, a
  lamp from the Hue app).

## Steps

1. **The fake discovery** in the test HA (`testenv/found_fake`, built 2026-11-10).
2. **Check the state table** against it (2026-11-10, every row).
3. **Noticing** in the service: the subscription, the gathering, the count rules, the said marks, the message.
   FD-01 to FD-05.
4. **The tools and the skill**: `devices_found`, `device_adopt`, `device_confirm`, `device_ignore`; `device_assign`
   and `device_finish` for any device; the skill's text. FD-06 to FD-13. End to end at the fake Show.
5. **The real run** and the guide.

## Later

- **Matter**: commissioning with the pairing code said by voice, over Bluetooth of the Home Assistant machine or a
  device shared from another app: a topic of its own (decided 2026-11-10).
