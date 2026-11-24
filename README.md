[![Mira](https://raw.githubusercontent.com/miraassist/mira/main/docs/brand/mira-logo-small.png)](https://mira-assist.com)

**Website: [mira-assist.com](https://mira-assist.com)**

**See what Mira can do:** ▶ [the videos on YouTube](https://www.youtube.com/playlist?list=PLEC8MZZ0-sKU)

<table>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=Fpv9fui4S7E&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/Fpv9fui4S7E/mqdefault.jpg" width="200" alt="Making a call"></a><br>Making a call</td><td align="center"><a href="https://www.youtube.com/watch?v=NbZAb3cOgk8&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/NbZAb3cOgk8/mqdefault.jpg" width="200" alt="Walkthrough the admin UI"></a><br>Walkthrough the admin UI</td><td align="center"><a href="https://www.youtube.com/watch?v=qiTTKerOOWs&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/qiTTKerOOWs/mqdefault.jpg" width="200" alt="Learns your voice"></a><br>Learns your voice</td><td align="center"><a href="https://www.youtube.com/watch?v=JRhG-fnupO0&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/JRhG-fnupO0/mqdefault.jpg" width="200" alt="Cooking together"></a><br>Cooking together</td></tr>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=JiR1YV6XYMQ&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/JiR1YV6XYMQ/mqdefault.jpg" width="200" alt="Plays a quiz"></a><br>Plays a quiz</td><td align="center"><a href="https://www.youtube.com/watch?v=rbEEmeoojKM&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/rbEEmeoojKM/mqdefault.jpg" width="200" alt="A vocabulary test"></a><br>A vocabulary test</td><td align="center"><a href="https://www.youtube.com/watch?v=OvzHRbNi6Mg&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/OvzHRbNi6Mg/mqdefault.jpg" width="200" alt="Appointments from a skill file"></a><br>Appointments from a skill file</td><td align="center"><a href="https://www.youtube.com/watch?v=WD0L1nUAwms&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/WD0L1nUAwms/mqdefault.jpg" width="200" alt="Creates an automation"></a><br>Creates an automation</td></tr>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=WpYLW-T7Fns&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/WpYLW-T7Fns/mqdefault.jpg" width="200" alt="Fills the shopping list"></a><br>Fills the shopping list</td><td align="center"><a href="https://www.youtube.com/watch?v=gbQjgAQlm-w&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/gbQjgAQlm-w/mqdefault.jpg" width="200" alt="Searches your documents"></a><br>Searches your documents</td><td align="center"><a href="https://www.youtube.com/watch?v=axJqZueqjf4"><img src="https://img.youtube.com/vi/axJqZueqjf4/mqdefault.jpg" width="200" alt="A music quiz"></a><br>A music quiz</td><td align="center"><a href="https://www.youtube.com/watch?v=K1LEZiy7XTM"><img src="https://img.youtube.com/vi/K1LEZiy7XTM/mqdefault.jpg" width="200" alt="Adds a Zigbee device"></a><br>Adds a Zigbee device</td></tr>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=SyKFBLzfPkY"><img src="https://img.youtube.com/vi/SyKFBLzfPkY/mqdefault.jpg" width="200" alt="Brings a Shelly into your Wi-Fi"></a><br>Brings a Shelly into your Wi-Fi</td><td align="center"><a href="https://www.youtube.com/watch?v=xO_4xwkGXZo"><img src="https://img.youtube.com/vi/xO_4xwkGXZo/mqdefault.jpg" width="200" alt="Adds a Matter device, the QR code scanned"></a><br>Adds a Matter device</td></tr>
</table>

Also: [Mira chatting in German](https://youtu.be/N-RfuKgBsEs).

## Get started

1. **The integration:** [![Open in the Marketplace](https://my.home-assistant.io/badges/marketplace_repository.svg)](https://my.home-assistant.io/redirect/marketplace_repository/?owner=miraassist&repository=mira&category=integration)
   or Settings → Marketplace → **Add from link** → `https://github.com/miraassist/mira`. Before Home Assistant
   2026.11: HACS → Custom repositories, the same address, type Integration. Install **Mira**, restart Home Assistant.
2. **The Mira service:** as an app on Home Assistant OS, or with Docker on a machine in your home network: see
   [Install the Mira service](https://github.com/miraassist/mira-app#install-the-mira-service) in
   [miraassist/mira-app](https://github.com/miraassist/mira-app). Installed the app from this address before? It
   moved: see [Moving the app](https://github.com/miraassist/mira-app#moving-the-app).
3. **Set it up:** Settings → Devices & services → **Mira** under **Discovered** → **Add** (with the app: Add
   integration → Mira, address `http://localhost:8787`). Choose the AI model (OpenAI, Google Gemini, OpenRouter,
   Mistral or Anthropic with your key, or a server of your own) and the assistants Mira answers in, then paste a
   long-lived token of an administrator (your profile → Security → Long-lived access tokens). Done.

More: [Installing Mira in detail](https://github.com/miraassist/mira-app/blob/main/docs/install.md) · the guide's setup: [Deutsch](https://github.com/miraassist/mira-app/blob/main/docs/guide/de.md#einrichtung) ·
[English](https://github.com/miraassist/mira-app/blob/main/docs/guide/en.md#setup).

[![CI](https://github.com/miraassist/mira/actions/workflows/ci.yml/badge.svg)](https://github.com/miraassist/mira/actions/workflows/ci.yml)

# Mira

**User guide:** [Deutsch](https://github.com/miraassist/mira-app/blob/main/docs/guide/de.md) · [English](https://github.com/miraassist/mira-app/blob/main/docs/guide/en.md) – also in the web UI under "Anleitung" / "Guide".
**System documentation:** [docs/system.md](https://github.com/miraassist/mira-app/blob/main/docs/system.md) – components, the main flows as diagrams, data, security.

An always-on voice assistant for Home Assistant. Mira thinks with a language
model of your choice (a cloud provider or a server of your own), talks to you
through Assist voice satellites and, through Home Assistant's Telegram
integration, in Telegram, controls the house through Home Assistant's MCP
server, and speaks up on her own when something needs attention.

Mira has two parts in two repositories, released together under one version:

| Part | Repository | What it does |
| --- | --- | --- |
| Integration (Marketplace) | this one: `custom_components/mira` | Thin Assist conversation agent that forwards each turn to the Mira service |
| Mira service (Go) and HA app | [miraassist/mira-app](https://github.com/miraassist/mira-app) | Prompting, tools, memory, messages, security policy; the app runs the service in Home Assistant OS |

The integration does not use Home Assistant's LLM API and runs no tools. All
logic lives in the Mira service.

## What Mira does

Web UI: `http://<mira>:8787/ui` edits the settings (models, skills, the
notifier, and the satellites and persons unless the HA integration manages
them, see [the integration](https://github.com/miraassist/mira-app/blob/main/docs/install.md#the-integration)); it lists open messages and the memory. It asks for
`MIRA_API_TOKEN` once. The page writes `mira.yaml` itself, the one truth for
the settings: comments and order stay, a token or secret setting goes into
Mira's secrets file in the data folder and the file gets a `${NAME}`
reference (docs/spec/editing.md). Changes apply at once. The page is in German
or English, following the browser, with a switch in its header.

Models: `models` names them, each with an endpoint, a token and whether it
is `local` or `cloud` (any OpenAI-compatible endpoint, e.g. Gemini); `main`
must exist, and every agent takes it unless `agents` names another for it
(`agents: { conversation.telegram: { model: smart } }`). The conversation
model hands harder questions (explanations, comparisons, weighing up)
through the always-on skill `nachdenken` to the model
`agents.delegate.model` names, typically a stronger one, and reads back
its answer. Only local models are
warmed up; a cloud model is billed per token, and each turn logs its model
and token counts.

Reminders: "remind me tomorrow at seven" is built in (`reminder_set`). At the
time Mira speaks the reminder in the room it was asked in, or sends it
privately. Timers are Home Assistant's own: Mira uses the tools Home Assistant
gives its voice assistants, and the timer runs on the satellite.

Messages are shown and spoken: per room, an indicator (such as an
Echo's LED ring) pulses while messages wait, satellites with a screen
(Echo Show with TECHO5) show them, detected by themselves, and "Hast du eine
Benachrichtigung?" reads them out (`messages_read`); with motion sensors and
"announce on entry" (`notifier.morning_trigger.areas`) Mira says them by
herself when someone comes in. Several messages are worded by the model as
one announcement. A message left at a satellite is not announced in that room
until `notifier.own_message_hold` (10 minutes) has passed. When you
talk to a satellite anyway, what waits for its room follows the answer
("Licht ist an. Übrigens: ..."), except after a question back and in quiet
hours. Urgent messages and reminders are always spoken.

Tasks: "Achte auf den Wasserverbrauch" becomes a Home Assistant automation
labelled "Mira" that the model drafts and the user confirms. Home Assistant's
triggers and conditions decide when; the integration's actions `mira.assess`
(judge against the goal with condensed data, maybe notify), `mira.decide`
(yes/no for conditions) and `mira.send_message` decide what is said. See
docs/spec/tasks.md and docs/system.md.

Skills: folders under `skills/` with a `SKILL.md` in the Agent Skills format
(frontmatter `name`, `description`; Mira's settings, calendars and job
templates (`jobs`) under `metadata.mira`). Enable them under `skills` with their
settings (`enabled: false` switches one off). A skill that cannot start (a missing setting, a typo) is left out
and Mira starts anyway: the log says in plain words what is wrong, and
Home Assistant shows it under Repairs. The guide has a section on writing your own. The prompt lists them in one line each; Mira opens a skill with
`skill_open` when a question needs it. Skills use the
core's building blocks, such as `calendar_events`, which reads a skill's
calendar by name so its URL never reaches a model. What a skill does at
fixed times are scheduled jobs: made on the skill from its job templates, each
with its own values and time, kept in `mira.yaml` under `skills.<skill>.jobs`;
they run in the background and may queue one message. `mira skill NAME` runs a
scheduled job now. A skill may bring its own tools in Starlark (`tools.star`)
for a service's web API, with a connection's token. The first skill, `abfall`, reads a waste collection calendar
and reminds the evening before. A skill may also bring Home Assistant
blueprints: Mira makes automations and scripts from them in Home Assistant
(a skill's script is a tool of that skill only, once it is opened). Skills
come from the skill folders or, at start, from a skill source with a
subscription key (docs/spec/skill-files.md, docs/spec/skill-source.md).

Languages (`de`, `en`; docs/spec/language.md): in a conversation Mira takes
the language Home Assistant sends with it, the Assist pipeline's. What she
says on her own takes the language last spoken at that satellite or by that
person, otherwise the last at all, and before anyone spoke, Home Assistant's
server language. There is no setting for it. The English prompt is written
but not yet tuned against a model (see `internal/prompt/testdata/prefix_voice_en.txt`).

More, each explained in the guide ([de](https://github.com/miraassist/mira-app/blob/main/docs/guide/de.md), [en](https://github.com/miraassist/mira-app/blob/main/docs/guide/en.md)):

- **Watches**: "Pass auf die Spülmaschine auf" – Mira learns what is normal for
  a device and says when it departs from it; watches for groups (lights left
  on, windows open while nobody is home, low batteries) by kind, area or floor;
  "Ist alles zu?" names what is still open.
- **Mira's inbox**: documents tagged in Paperless and mails of an IMAP
  mailbox; she finds appointments and to-dos with a deadline in them and, after a
  yes, enters them into the calendar or onto the to-do list.
- **Adding devices by voice**: Zigbee, Matter (the QR code read by an Echo
  Show's camera, or its digits read out), devices Home Assistant found, and
  Wi-Fi devices brought into the Wi-Fi; then room and name.
- **Phone calls and the answering machine** through the integration
  [Mira FRITZ!Box](https://github.com/miraassist/mira-fritz): "Ruf Oma an"
  from the satellite, new messages announced and played.
- **Connections**: a skill signs in at a service over OAuth through Home
  Assistant, such as X for reading posts, Strava for your activities or
  YouTube for new videos on your topics. Such a skill brings its tools as
  small Starlark functions (`tools.star`): fixed tools for the service's web
  API, without a server of its own.
- **Scheduled jobs**: what a skill does at fixed times is a job you make on
  the skill, with its own values and time (a YouTube list in the morning on
  other topics than in the evening).
- **Music with Music Assistant**: music and radio play on Music Assistant's
  players; "Nimm die Musik mit in die Küche" takes it along at the same place
  in the song, "Spiel das auch im Bad" adds a room, and the queue ("Spiel
  danach …"), more of the same ("Spiel mehr davon") and jumping within the
  song ("Spring 30 Sekunden vor") work by voice. While a satellite listens,
  the players playing in its room go down to a third.
- **Alarm at every satellite**: "Weck mich morgen um 6" – Mira's wake sound
  grows louder until you say the wake word, with the speaker at 90 % and back
  afterwards; or music instead (a station, and through Music Assistant a
  playlist, album or artist), and the room's light rising before the alarm.
  "Nein, halb sieben" corrects it.
- **Access tokens**: a partner who looks after Mira for you gets a named
  token of their own, which reads only Mira's state; what was done with it is
  listed on Mira's page. Everything else the partner does in Home Assistant,
  signed in as a user you give rights to.
- **Mira's interface**: described at `/v1/openapi.yaml`; with a token it also
  reads Home Assistant's states and calls its actions (`/v1/ha/…`,
  docs/spec/ha-api.md).
- **Remote agents**: a skill hands work to an agent on another device over
  A2A, such as a long research.
- **Rules of your own**: "Merk dir eine Regel: …" – a helper writes it as a
  hook, on after your yes; or as a Starlark file in `hooks/`.
- **Voice recognition**: who speaks at a satellite, by the voice
  (`speaker_id`, docs/spec/speaker-id.md).
- **Backup**: Mira's data goes into Home Assistant's backups through the
  integration; after a restore a repair offers to restore it.

Security: locks, alarm panels and other configured entities only run after
the user confirms in the next turn. After a turn has read web content,
critical actions are blocked for the rest of that turn. Background jobs
cannot run tools that change anything. Feed text is data and can never raise
an urgent message. These rules are enforced in code, not only in the prompt.

## Development

```sh
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # integration
scripts/check.sh                                      # all CI checks incl. hassfest, before a push
```

The Mira service, the test Home Assistant and the docs are in
[miraassist/mira-app](https://github.com/miraassist/mira-app); with a checkout of it beside this one,
`scripts/check.sh` also checks the automations the service builds.

Releases: the integration and the Mira service share one version. Bump it in `custom_components/mira/const.py` and
`custom_components/mira/manifest.json` here and in the service's `cmd/mira/main.go`; tag `vX.Y.Z` in
miraassist/mira-app first, then here. The release workflow refuses a tag that does not match.

## License

Apache License 2.0, see [LICENSE](LICENSE).
