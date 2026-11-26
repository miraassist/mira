<a href="https://mira-assist.com"><img src="https://raw.githubusercontent.com/miraassist/mira/main/docs/brand/mira-logo-small.png" width="220" alt="Mira"></a>

**A voice assistant for Home Assistant that holds a real conversation:** she asks back, remembers, knows who is
speaking and speaks up on her own when something needs your attention.

🌐 **[mira-assist.com](https://mira-assist.com)** · ▶ **[Videos](https://www.youtube.com/playlist?list=PLEC8MZZ0-sKU)** ·
📖 Guide: [Deutsch](https://github.com/miraassist/mira-app/blob/main/docs/guide/de.md) ·
[English](https://github.com/miraassist/mira-app/blob/main/docs/guide/en.md)

<table>
<tr>
<td align="center" width="33%"><a href="https://www.youtube.com/watch?v=JRhG-fnupO0&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/JRhG-fnupO0/hqdefault.jpg" width="300" alt="Cooking together"></a><br><b>Cooking together</b><br>A recipe step by step, with questions in between</td>
<td align="center" width="33%"><a href="https://www.youtube.com/watch?v=qiTTKerOOWs&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/qiTTKerOOWs/hqdefault.jpg" width="300" alt="Learns your voice"></a><br><b>Learns your voice</b><br>Knows who is speaking and answers for that person</td>
<td align="center" width="33%"><a href="https://www.youtube.com/watch?v=Fpv9fui4S7E&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/Fpv9fui4S7E/hqdefault.jpg" width="300" alt="Making a call"></a><br><b>Making a call</b><br>Calls from the voice satellite</td>
</tr>
</table>

## What you get

- **Conversations, not commands.** Mira keeps the thread over several sentences, asks when something is unclear and
  remembers what matters to your household. ([Cooking together](https://www.youtube.com/watch?v=JRhG-fnupO0&list=PLEC8MZZ0-sKU), [chatting in German](https://youtu.be/N-RfuKgBsEs))
- **She knows who is talking.** With voice recognition (optional) your calendar, your reminders and your messages stay yours.
  ([Learns your voice](https://www.youtube.com/watch?v=qiTTKerOOWs&list=PLEC8MZZ0-sKU))
- **She looks after the house.** "Tell me when the water use is odd": Mira turns it into a Home Assistant automation
  you confirm, and speaks up by herself, in the room you are in. ([Creates an automation](https://www.youtube.com/watch?v=WD0L1nUAwms&list=PLEC8MZZ0-sKU))
- **She gets things done.** Shopping list, appointments from a letter, your documents, calls; and new devices by voice:
  Zigbee, Matter, a Shelly into your Wi-Fi. ([Shopping list](https://www.youtube.com/watch?v=WpYLW-T7Fns&list=PLEC8MZZ0-sKU), [Zigbee](https://www.youtube.com/watch?v=K1LEZiy7XTM), [Matter](https://www.youtube.com/watch?v=xO_4xwkGXZo))
- **Skills for more:** quizzes, a vocabulary test, the waste calendar, news, and your own skills as simple files.
  ([Quiz](https://www.youtube.com/watch?v=JiR1YV6XYMQ&list=PLEC8MZZ0-sKU), [Vocabulary](https://www.youtube.com/watch?v=rbEEmeoojKM&list=PLEC8MZZ0-sKU))
- **Your AI, your choice.** OpenAI, Google Gemini, OpenRouter, Mistral, Anthropic, or a server of your own. Locks and
  alarm panels switch only after your yes, enforced in code.

## What you need

- Home Assistant 2026.10 or newer with an Assist voice satellite (or Telegram).
- **The Mira service:** the Mira app on Home Assistant OS, or a Docker container in your home network.
- A key for an AI provider, or an AI server of your own.

## Get started

1. **The integration:** [![Open in the Marketplace](https://my.home-assistant.io/badges/marketplace_repository.svg)](https://my.home-assistant.io/redirect/marketplace_repository/?owner=miraassist&repository=mira&category=integration)
   or Settings → Marketplace, search for **Mira**. Install it, restart Home Assistant. Before Home Assistant
   2026.11: HACS, search for **Mira**, **Download**, restart Home Assistant.
2. **The Mira service:** as an app on Home Assistant OS ([![Add the app repository to my Home Assistant](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmiraassist%2Fmira-app)),
   or with Docker: see [Install the Mira service](https://github.com/miraassist/mira-app#install-the-mira-service).
   Installed the app from this address before? It moved: see
   [Moving the app](https://github.com/miraassist/mira-app#moving-the-app).
3. **Set it up:** Settings → Devices & services → **Mira** under **Discovered** → **Add** (with the app: Add
   integration → Mira, address `http://localhost:8787`). Choose the AI model and the
   assistants Mira answers in, paste a long-lived token of an administrator. Done.

More: [Installing Mira in detail](https://github.com/miraassist/mira-app/blob/main/docs/install.md) ·
[all videos](https://www.youtube.com/playlist?list=PLEC8MZZ0-sKU)

## All videos

[Making a call](https://www.youtube.com/watch?v=Fpv9fui4S7E&list=PLEC8MZZ0-sKU) ·
[Walkthrough the admin UI](https://www.youtube.com/watch?v=NbZAb3cOgk8&list=PLEC8MZZ0-sKU) ·
[Learns your voice](https://www.youtube.com/watch?v=qiTTKerOOWs&list=PLEC8MZZ0-sKU) ·
[Cooking together](https://www.youtube.com/watch?v=JRhG-fnupO0&list=PLEC8MZZ0-sKU) ·
[Plays a quiz](https://www.youtube.com/watch?v=JiR1YV6XYMQ&list=PLEC8MZZ0-sKU) ·
[A vocabulary test](https://www.youtube.com/watch?v=rbEEmeoojKM&list=PLEC8MZZ0-sKU) ·
[Appointments from a skill file](https://www.youtube.com/watch?v=OvzHRbNi6Mg&list=PLEC8MZZ0-sKU) ·
[Creates an automation](https://www.youtube.com/watch?v=WD0L1nUAwms&list=PLEC8MZZ0-sKU) ·
[Fills the shopping list](https://www.youtube.com/watch?v=WpYLW-T7Fns&list=PLEC8MZZ0-sKU) ·
[Searches your documents](https://www.youtube.com/watch?v=gbQjgAQlm-w&list=PLEC8MZZ0-sKU) ·
[A music quiz](https://www.youtube.com/watch?v=axJqZueqjf4) ·
[Adds a Zigbee device](https://www.youtube.com/watch?v=K1LEZiy7XTM) ·
[Brings a Shelly into your Wi-Fi](https://www.youtube.com/watch?v=SyKFBLzfPkY) ·
[Adds a Matter device](https://www.youtube.com/watch?v=xO_4xwkGXZo) ·
[Mira chatting in German](https://youtu.be/N-RfuKgBsEs)

## How it works

Mira has two parts, each with its own version: this **integration** is a thin Assist conversation agent in Home
Assistant; the **Mira service** ([miraassist/mira-app](https://github.com/miraassist/mira-app)) does the thinking:
the model, tools, memory, messages and the safety rules. It controls the house through Home Assistant's own MCP
server. Details: [system documentation](https://github.com/miraassist/mira-app/blob/main/docs/system.md).

## Development

[![CI](https://github.com/miraassist/mira/actions/workflows/ci.yml/badge.svg)](https://github.com/miraassist/mira/actions/workflows/ci.yml)

```sh
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # integration
scripts/check.sh                                      # all CI checks incl. hassfest, before a push
```

The Mira service, the test Home Assistant and the docs are in
[miraassist/mira-app](https://github.com/miraassist/mira-app); with a checkout of it beside this one,
`scripts/check.sh` also checks the automations the service builds.

Releases: the integration has its own version. Raise it in `custom_components/mira/const.py` and
`custom_components/mira/manifest.json` and tag `vX.Y.Z`; the release workflow refuses a tag that does not match. It
names the oldest Mira service it works with (`MIN_SERVICE` in `const.py`), raised only when an older service would
fail; such a release says so first in its notes.

## License

Apache License 2.0, see [LICENSE](LICENSE).
