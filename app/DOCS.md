# Mira

Mira is a household voice agent for Home Assistant: conversations, tasks she keeps an eye on, memory and skills.
This app runs Mira's service; the **Mira integration** (from HACS) connects Home Assistant to it. Both share one
version: update them together.

## Setup

1. Install and start this app.
2. Install the **Mira** integration through HACS and restart Home Assistant.
3. Settings → Devices & services → **Add integration** → **Mira**. As the address enter
   `http://localhost:8787`. The setup then asks for the AI model and the assistants Mira answers in.

No config file is needed. For what goes beyond the setup (more models, skills with settings), Mira's `mira.yaml`
is in this app's config folder (`/addon_configs/…_mira/mira.yaml`). The guide explains everything else:
<https://github.com/miraassist/mira/blob/main/docs/guide/en.md>.

## Data

Mira's data (memory, messages, settings, keys, your own skills) goes into Home Assistant's backups through the Mira
integration; after a restore of Home Assistant, a repair offers to restore Mira's data. The guide's "Backup and
restore" says more. The app's own backup leaves the database out: it comes through the integration, as one
consistent copy.

## Pairing again and resetting

- Removing the Mira integration and adding it again in the same Home Assistant needs no code.
- Another Home Assistant needs the **pairing code**: it is in this app's **Log**, written when the integration was
  removed.
- **Reset data** (Configuration tab): turn it on and restart the app. Mira deletes its data (memory, messages,
  settings from Home Assistant, the AI model) and keeps `mira.yaml`; then it pairs again without a code, like a
  new install. It resets once; turn the option off again afterwards.

## Access to the Supervisor

The app may use the Supervisor's network API (the role "manager"): with the skill `geraet-einbinden` Mira brings a
new Wi-Fi device into your Wi-Fi with the Wi-Fi of the machine Home Assistant runs on, when the machine is in the network
by cable. She borrows the Wi-Fi only for the minute of the hand-over.
