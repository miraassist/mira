# Changelog

The app has the version of Mira's release; what changed is in the release notes:
<https://github.com/miraassist/mira/releases>

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
