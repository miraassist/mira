# Changelog

The app has the version of Mira's release; what changed is in the release notes:
<https://github.com/miraassist/mira/releases>

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
