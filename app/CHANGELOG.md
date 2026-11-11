# Changelog

The app has the version of Mira's release; what changed is in the release notes:
<https://github.com/miraassist/mira/releases>

## 0.22.0

- **Matter devices by voice.** "Ich hab ein neues Matter-Gerät": at an Echo Show Mira reads the QR code with its
  camera, at every other satellite you read the eleven digits beside it out (she checks them before she uses them).
  Then she looks for the device over Bluetooth, gives it your Wi-Fi and asks for its room and name as for every
  device. Home Assistant needs its Matter integration; Matter over Wi-Fi, Thread comes later.
- **Mira keeps listening when she asked you for something** — for the setup code, the room, which Zigbee network —
  however she words it. Until now the satellite listened on only when her sentence ended with a question mark.
- **A message that cannot be delivered no longer tries for ever.** Mira tells apart what may heal (Home Assistant not
  reachable, a channel that is down) from what will not (no channel, an unknown person): the first is tried again
  with growing waits and given up after ten attempts, the second ends at once and the message is "nicht zustellbar"
  on the Messages tab. The same rule holds for reminders, for model calls (a model that asks to wait is waited for)
  and for the connections to Home Assistant and to a skill's MCP server, whose token, when refused, is not tried
  again until something changes.
- **"Welche Nachrichten sind offen?" now shows what you may hear** — what would be read out where you are, your own
  in your chat, never another person's private ones.
- **After a restart Mira knows her satellites and persons at once** instead of up to a minute later: she tells Home
  Assistant that she started, and the integration sends its settings right away.
- **A scheduled job that falls into a restart waits for Home Assistant** (up to five minutes) instead of failing;
  when Home Assistant comes back, Mira asks it for the house description at once.
- **A helper that answers nothing says why** (what it asked for instead, whether it only thought), and its last
  round is told that it is the last, so a long research ends with an answer instead of silence.

## 0.21.0

- Devices Home Assistant found (a bridge, a Shelly in the Wi-Fi, a Bluetooth sensor, a lamp from a maker's app) are
  said once as a message; on your yes Mira takes them in: room, name, values for voice. "No, that's the neighbour's"
  ignores one for good. "Any new devices?" lists what still waits.
- A new Wi-Fi device in its setup mode (Shelly, Tasmota, ESPHome, WLED) is brought into your Wi-Fi by voice, with the
  Wi-Fi of the machine Mira runs on while it stays on its cable. Enter your Wi-Fi (and for Tasmota the MQTT broker)
  at the skill `geraet-einbinden`. The app gets the Supervisor's role "manager" for the machine's Wi-Fi.
- Zigbee devices on ZHA, too; with Zigbee2MQTT and ZHA both, Mira asks which network.
- While Mira waits for a device, the satellite shows it: a turquoise ring on an Echo Show, the light ring on an Echo.
- A device's entity ids follow the name Mira gives it.
- The integration's update and the app's are offered together; Mira asks Home Assistant to look for the other one at
  once.
- Errors in the log name what they failed on (the file, the address, the start of an answer that could not be read).

## 0.20.0

- New skill `geraet-einbinden`: add a Zigbee device by voice. "Ich hab ein neues Zigbee-Gerät" opens pairing in
  Zigbee2MQTT; once the device is there Mira asks for its room, proposes a name and exposes its values to voice.
  Needs Zigbee2MQTT connected to Home Assistant; turn the skill on under Capabilities → Skills.
- A skill's schedule can send its result to one person: `to` names the person (usually from a setting of the skill).
  The message comes at once and only over that person's channels, such as Telegram, and is never read aloud. Good
  for a list with links.
- The Messages tab marks such a message "private".
- A list or device just added in Home Assistant is known to Mira at once, not only up to a minute later.
- Mira no longer confirms an item on a list that does not exist; she says the list is missing.

## 0.19.0

- A running plan (a recipe, a quiz) can be ended on the page: tab Plans, "End". Everyone who sees the plans may end
  one.
- A model that fails (a key the server refuses, a server that does not answer) is said in the answer and shown as a
  repair until it answers again.
- Quizzes, vocabulary and stories vary more: Mira picks at random instead of always the same.
- Local models can be warmed up: the switch "Warm up" on the page Models, so the first answer does not take long. A
  model with warm slots now warms them at the start too; the log says when a warm-up begins.
- An answer ended by you or by Home Assistant is one line in the log with what was said; no longer an error.
- The evening briefing no longer names the waste collection; the skill Waste reminds you of it.
- HACS installs the integration from a zip of the release.

## 0.18.0

- New skill `musikquiz`: Mira plays 30 seconds of a song, you guess the artist, title, year or decade. The Echo Show
  shows only "Musik-Quiz" while it plays, and a card with the song and its cover after your answer.
- Mira now works with an access in the name of whoever set Home Assistant up, with admin rights: she saves your tasks
  and timers through Home Assistant's own automation editor interface. Existing installs: confirm "Mira needs a new
  access" under Settings → Repairs; until then new tasks and timers wait. The old user "Mira" may be deleted.
- The AI model can no longer be taken over from another integration; enter its key for Mira.
- A missing calendar for mails ("Pakete") is a repair you confirm; Mira no longer creates it by herself.

## 0.17.0

- Answers spoken while they are written no longer repeat themselves: a quiz asked some questions twice with
  models that write a sentence next to their tool calls (DeepSeek, for example).
- A sound (the quiz's fanfare) no longer cuts Mira's answer off: it plays under her voice, and as an announcement
  only when music or the radio plays, so the radio goes on after it.
- After a question the satellite listens again even when the question was not the last sentence.
- "Spiel SWR 3" with a space finds the station at once.
- Version 0.16.0 told the integration it was 0.15.2 and showed a wrong "versions do not match" repair; fixed.
- More is in Mira's log: retried model calls, and errors that were handled without a trace before.

## 0.16.0

- Connections: a skill can sign in at a service (OAuth). On the new page Setup → Connections you enter the client ID
  and secret of your own app at the service and click "Connect"; Home Assistant does the sign-in, Mira keeps the
  sign-in fresh. Needs Home Assistant 2026.11.
- New skill `x`: reads posts on X (search, what an account posted, your timeline, mentions). Every post read costs a
  little at X, so Mira fetches only a few.
- Home Assistant 2026.11 renamed the prompt that describes the house; Mira found it no longer and worked with less.
  She now takes the prompt Home Assistant offers, under any name.
- Without that description of the house Mira no longer answers with less: she says so in one sentence, and Home
  Assistant shows a repair until it is back.
- Installing and updating now point to the Marketplace (HACS before Home Assistant 2026.11).
- Questions before deleting or disconnecting are the page's own dialog, no longer a browser alert.

## 0.15.2

- When Home Assistant has started, the integration tells Mira at once what Assist sees, instead of only at the next
  check a minute later.
- Radio on the speaker you are talking to is confirmed briefly ("SWR3 läuft."); elsewhere Mira says only the room
  ("SWR1 läuft im Bad"), no longer the speaker's name.
- Mira answers questions about the past: "When was the bathroom window open today?", "How long did the washing machine
  run yesterday?", up to 31 days, from Home Assistant's history.
- Mira speaks while she writes the answer: a long answer (a story, the briefing) starts seconds earlier. Before a
  longer task she may first say what she is doing. The speech engine and the satellite must stream too (Phonon does; TECHO5: reply delivery "Early").
- For a local model on llama.cpp: the new setting "Always keep a warm slot" (`warm_slots`) keeps Mira's prompt warm in
  one of two slots while a changed one is read into the other, so a turn no longer waits half a minute after Home
  Assistant changed something. The server needs two slots of its own (see the guide).
- A story or an answer from general knowledge is no longer cut off as "Das habe ich nicht ausgeführt.": Mira no
  longer checks the model's answer for words like "eingeschaltet" or a number with a unit.

## 0.15.1

- Entities you expose to Assist all reach Mira, also those Home Assistant marks as diagnostic (the GitHub
  integration's stars, for example) or hides; before, Mira left them out.
- A large tool result (a whole house's sensor list) is no longer cut at a fixed size: the limit follows the model's
  context, and when something is cut, Mira is told how much is missing.
- Mira answers a fragment that makes no sense in her own words instead of a fixed sentence.

## 0.15.0

- New page **Capabilities → Schedules**: every schedule of the skills and Mira's own, with the last run and what came
  of it, the next run, the time with weekdays, a switch, and "Run now". A change holds at once and goes into
  `mira.yaml`.
- Mira's data goes into Home Assistant's backups through the integration; after a restore of Home Assistant a repair
  offers to restore Mira's data. The app's own backup leaves the database out.
- A satellite without an area is reported as a repair; a room may have several satellites, with a main satellite.
- "Spiel SWR3" shows the station's name and logo on a TECHO5 satellite.
- People are shown with their picture from Home Assistant.

## 0.14.1

- The persona set in the integration's options is saved again: it goes into `mira.yaml` under `prompt: persona:`.
  Saving it failed in 0.14.0 with an error about `mira.yaml`. A persona set before 0.14.0 was kept in Mira's
  database, which 0.14.0 no longer reads: enter it again in the options (Mira's integration, Configure, General).

## 0.14.0

- Mira's pages show their entries as cards: opened, a card offers what can be changed right there, skills and tasks
  have a switch, each fact in the memory is a text field, satellites and people save one entry at a time. Every page
  links to its chapter in the guide.
- Settings saved on the pages (notifications; satellites and people without the integration) go into `mira.yaml`,
  only what differs from the defaults, and hold at once. "Back to mira.yaml" is gone; changes Mira kept in her
  database before are no longer read.
- New skill `aufgabe-aendern`: change a task in the conversation, also one made in Home Assistant ("Das
  Abendlicht soll erst um 19 Uhr angehen").

## 0.13.1

- Fixes from a review: the reply to an announcement no longer waits, satellites' speakers and actions are looked
  up without reading Home Assistant's registries each time, reminders no longer hold up other announcements, and
  old finished messages, announcements and threads are cleaned up.

## 0.13.0

- New skill `anrufbeantworter`: the FRITZ!Box's answering machine at the satellites (needs the integration
  Mira FRITZ!Box from HACS). The skill `telefon` reads its phone book there too; its own FRITZ!Box settings in
  `mira.yaml` are no longer read.
- A satellite's LED ring is found by itself; the setting is only needed for another light.

## 0.12.0

- Hooks: rules of your own as `.star` files in the folder `hooks` next to `mira.yaml` (this app's config folder).
- `security.confirm_domains`, `confirm_entities` and `confirm_tools` in `mira.yaml` are no longer read; use the label
  "Mira: nachfragen" in Home Assistant or a hook.

## 0.11.0

- New option **Reset data**: deletes Mira's data at the next start and keeps `mira.yaml`.
- Pairing with another Home Assistant takes the code from this app's log.

## 0.10.0

- First version as a Home Assistant app.
