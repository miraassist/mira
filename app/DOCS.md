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

Mira's database is in the app's data folder and part of Home Assistant's backups.

## Pairing again and resetting

- Removing the Mira integration and adding it again in the same Home Assistant needs no code.
- Another Home Assistant needs the **pairing code**: it is in this app's **Log**, written when the integration was
  removed.
- **Reset data** (Configuration tab): turn it on and restart the app. Mira deletes its data (memory, messages,
  settings from Home Assistant, the AI model) and keeps `mira.yaml`; then it pairs again without a code, like a
  new install. It resets once; turn the option off again afterwards.
