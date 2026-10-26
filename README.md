[![Mira](https://raw.githubusercontent.com/miraassist/mira/main/docs/brand/mira-logo-small.png)](https://mira-assist.com)

**Website: [mira-assist.com](https://mira-assist.com)**

**See what Mira can do:** ▶ [the videos on YouTube](https://www.youtube.com/playlist?list=PLEC8MZZ0-sKU)

<table>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=Fpv9fui4S7E&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/Fpv9fui4S7E/mqdefault.jpg" width="260" alt="Making a call"></a><br>Making a call</td><td align="center"><a href="https://www.youtube.com/watch?v=NbZAb3cOgk8&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/NbZAb3cOgk8/mqdefault.jpg" width="260" alt="Walkthrough the admin UI"></a><br>Walkthrough the admin UI</td><td align="center"><a href="https://www.youtube.com/watch?v=qiTTKerOOWs&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/qiTTKerOOWs/mqdefault.jpg" width="260" alt="Learns your voice"></a><br>Learns your voice</td></tr>
<tr><td align="center"><a href="https://www.youtube.com/watch?v=JRhG-fnupO0&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/JRhG-fnupO0/mqdefault.jpg" width="260" alt="Cooking together"></a><br>Cooking together</td><td align="center"><a href="https://www.youtube.com/watch?v=JiR1YV6XYMQ&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/JiR1YV6XYMQ/mqdefault.jpg" width="260" alt="Plays a quiz"></a><br>Plays a quiz</td><td align="center"><a href="https://www.youtube.com/watch?v=rbEEmeoojKM&list=PLEC8MZZ0-sKU"><img src="https://img.youtube.com/vi/rbEEmeoojKM/mqdefault.jpg" width="260" alt="A vocabulary test"></a><br>A vocabulary test</td></tr>
</table>

Also: [Mira chatting in German](https://youtu.be/N-RfuKgBsEs).

[![Install with HACS](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=miraassist&repository=mira&category=integration)

[![HACS Custom](https://img.shields.io/badge/HACS-Custom-41BDF5.svg)](https://hacs.xyz) [![CI](https://github.com/miraassist/mira/actions/workflows/ci.yml/badge.svg)](https://github.com/miraassist/mira/actions/workflows/ci.yml)

# Mira

**User guide:** [Deutsch](docs/guide/de.md) · [English](docs/guide/en.md) – also in the web UI under "Anleitung" / "Guide".
**System documentation:** [docs/system.md](docs/system.md) – components, the main flows as diagrams, data, security.

A local, always-on household agent for Home Assistant. It runs next to a
llama.cpp server, talks to you through Assist voice satellites and Telegram,
controls the house through Home Assistant's MCP server, and can speak up on
its own when something needs attention.

This repository holds both parts, released together under one version:

| Part | Where | What it does |
| --- | --- | --- |
| Agent (Go) | `cmd/mira`, `internal/` | Prompting, tools, memory, messages, security policy |
| Integration (HACS) | `custom_components/mira` | Thin Assist conversation agent that forwards each turn to the agent |

The integration does not use Home Assistant's LLM API and runs no tools. All
logic lives in the agent.

## Quick start

No config file, three steps (details and troubleshooting in the guide:
[Deutsch](docs/guide/de.md#einrichtung) · [English](docs/guide/en.md#setup)):

1. **Install the integration** with the HACS button above, restart Home Assistant.
2. **Start the agent** on a machine with Docker in your home network:
   ```sh
   docker run -d --name mira --restart unless-stopped --network host --user nonroot \
     -e TZ=Europe/Berlin -v mira-data:/var/lib/mira ghcr.io/miraassist/mira
   ```
   With Home Assistant OS, install it as an app instead: Settings → Apps → App store → Repositories →
   `https://github.com/miraassist/mira`, then **Mira**. In step 3 add the integration by hand with the address
   `http://localhost:8787`.
3. Settings → Devices & services: **Mira** is under **Discovered** → **Add**. The dialog asks for
   An overview shows what is chosen and what is missing: the AI model (LLM: an AI integration
   Home Assistant already has, OpenAI, Gemini, OpenRouter or Mistral with your key, or a server of
   your own; tried at once) and the assistants Mira answers in. **Start using Mira** does the rest: Mira's own Home Assistant user (no admin rights),
   Home Assistant's MCP server, Mira as the assistants' conversation agent.

## Agent

Running from source, or with your own `mira.yaml` for what goes beyond the
setup form (more models, more MCP servers, skills with settings):

```sh
go build -o mira ./cmd/mira
./mira run                            # no config: waits to be set up by Home Assistant
cp mira.example.yaml mira.yaml        # optional; secrets come from ${ENV}
./mira -config mira.yaml check        # probe the LLM, Home Assistant and MCP servers
./mira -config mira.yaml chat "Wie warm ist es im Wohnzimmer?"
./mira -config mira.yaml profile      # compact the remembered facts into the profile now
./mira -config mira.yaml run
```

Or in Docker (the `Dockerfile` builds a static binary on distroless, about
45 MB):

```sh
docker build -t mira .
docker run -d --name mira --network host --user nonroot -e TZ=Europe/Berlin --env-file .env \
  -v ./mira.yaml:/etc/mira/mira.yaml:ro -v mira-data:/var/lib/mira mira
```

The image runs as root, as a Home Assistant app needs; as a container of your own run it as
`nonroot` (`--user nonroot`, in Compose `user: nonroot`), who owns the data folder. With a folder
of your own instead of a volume, give it the user you run as. The database goes to
`/var/lib/mira/mira.db` in the image (`MIRA_DATA`),
`mira.db` in the working directory otherwise; `store.path` changes it. The
built-in skills are in the image. `--network host` lets the integration find
the agent by mDNS; without it, publish the port (`-p 8787:8787`) and enter
the URL in the integration. MCP servers started over stdio need their own
runtime (e.g. node), which the image does not have; use MCP servers over HTTP.

Requirements:
- An OpenAI-compatible LLM server: llama.cpp (`llama-server --parallel 3`,
  `llm.slot_pinning: true`) or LM Studio (`slot_pinning: false`; it picks
  slots by common prefix, which works well because all channels share the
  start of the prompt).
- Home Assistant. Pairing sets up the rest there: a non-admin user "Mira"
  with a long-lived token, and the "Model Context Protocol Server"
  integration with the Assist API (`/api/mcp/assist`). With `ha.url` and
  `ha.token` in `mira.yaml` instead, set both up by hand.
- Optional: more MCP servers under `mcp_servers`, over HTTP or started by
  Mira (stdio). A web search is built in (DuckDuckGo, no key; switched in the
  integration's options, docs/spec/websuche.md). Radio is built in too: "play SWR3"
  finds the station in Home Assistant's Radio Browser and starts it on the
  room's speaker (`radio_play`, docs/spec/radio.md). See "More tools over MCP" in the guide
  ([de](docs/guide/de.md), [en](docs/guide/en.md)).

Web UI: `http://<agent>:8787/ui` edits the notifier, and the satellites and
persons unless the HA integration manages them (see below); it lists open messages and the memory. It asks for `MIRA_API_TOKEN`
once. Changes are stored in the database on top of `mira.yaml` (the file is
never rewritten; each section can be reset to it) and apply at once. The page
is in German or English, following the browser, with a switch in its header.

Models: `models` names them, each with an endpoint, a token and whether it
is `local` or `cloud` (any OpenAI-compatible endpoint, e.g. Gemini); `main`
must exist, and every agent takes it unless `agents` names another for it
(`agents: { conversation.telegram: { model: smart } }`). The conversation
model has a `delegate` tool that hands harder tasks (planning,
explanations, research) to the model `agents.delegate.model` names,
typically a stronger one, and reads back its answer. Only local models are
warmed up; a cloud model is billed per token, and each turn logs its model
and token counts.

Reminders: "remind me tomorrow at seven" and timers are built in
(`reminder_set`). At the time Mira speaks the reminder in the room it was
asked in, or sends it privately.

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
docs/spec/aufgaben.md and docs/system.md.

Skills: folders under `skills/` with a `SKILL.md` in the Agent Skills format
(frontmatter `name`, `description`; Mira's settings, calendars and schedules
under `metadata.mira`). Enable them under `skills` with their
settings (`enabled: false` switches one off). A skill that cannot start (a missing setting, a typo) is left out
and the agent starts anyway: the log says in plain words what is wrong, and
Home Assistant shows it under Repairs. The guide has a section on writing your own. The prompt lists them in one line each; Mira opens a skill with
`skill_open` when a question needs it. Skills run no code: they use the
core's building blocks, such as `calendar_events`, which reads a skill's
calendar by name so its URL never reaches a model. A skill's schedules run
as background jobs and may queue one message. `mira skill NAME` runs a
schedule now. The first skill, `abfall`, reads a waste collection calendar
and reminds the evening before.

Languages (`de`, `en`; docs/spec/sprache.md): in a conversation Mira takes
the language Home Assistant sends with it, the Assist pipeline's. What she
says on her own takes the language last spoken at that satellite or by that
person, otherwise the last at all, and before anyone spoke, Home Assistant's
server language. There is no setting for it. The English prompt is written
but not yet tuned against a model (see `internal/prompt/testdata/prefix_voice_en.txt`).

Security: locks, alarm panels and other configured entities only run after
the user confirms in the next turn. After a turn has read web content,
critical actions are blocked for the rest of that turn. Background jobs
cannot run tools that change anything. Feed text is data and can never raise
an urgent message. These rules are enforced in code, not only in the prompt.

## Integration

1. Click **Install with HACS** above, or HACS → Custom repositories → `https://github.com/miraassist/mira`, type Integration.
2. Install **Mira**, restart Home Assistant.
3. Settings → Devices & services: the agent announces itself by mDNS
   (`_mira._tcp`, `server.announce`) and shows up under **Discovered**;
   otherwise Add integration → Mira with its URL. A new agent is added
   with one click (an agent with `server.token` in `mira.yaml` asks for
   that token), then the form asks for the AI model and the assistants.

Pairing (docs/spec/einrichtung.md): the form chooses and tries the model
first (the agent answers `/v1/setup/models` and `/v1/setup/probe` without a
token only while it was never paired); nothing is changed before the
summary's "Pair". That creates the HA user and token, switches on HA's MCP
server and pairs and sets up the agent in one call (`POST /v1/pair` with HA's
address, the token and the model), which keeps everything in its database,
hands out its access token and starts anew. An agent that was never paired
needs no code; after that a code from `mira pair` (15 minutes, five tries). It makes Mira the conversation agent of the
assistants ticked in the form. Removing the integration revokes the token,
gives the assistants their previous agent back and lets the agent go
(`POST /v1/unpair`: a new token, paired again without a code); the HA user
stays, Mira's automations refer to it.

Mira's web UI is in Home Assistant's sidebar: the integration passes the
page through Home Assistant (open, it holds no data) and its API calls
through an authenticated proxy that adds the agent's token and the signed-in
HA user. So it works over HA's HTTPS, with HA's login, and takes on HA's
theme. There everyone, admins too, sees the house's memory and their own
(person linked to the HA user); messages, tasks and settings are for admins,
enforced by the proxy and the agent. The integration's options switch it off.

Mira keeps no lists of her own for what Home Assistant has
(docs/spec/ha-anbindung.md): every HA person is a person for Mira, every
Assist satellite with an area speaks for that area. What Mira needs on top
is stored on the person or satellite itself, as entity registry options
under `mira`, and set in the integration's options ("Configure": settings
for a person, for a satellite). Entities labelled "Mira: nachfragen" are
only switched after a yes. Each change is checked by the agent before it is
saved; what it refuses shows in the form. While the integration
manages them, the agent's web UI shows those sections read-only. Removing the
integration hands them back. The device also has a quiet-mode switch, a
pending-messages sensor and a button to compact the memory profile now.

## Development

```sh
go test -race ./...                                   # agent
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # integration
go run ./cmd/testha up                                # test Home Assistant in Docker
scripts/check.sh                                      # all CI checks incl. hassfest, before a push
```

`testenv/` holds a throwaway Home Assistant for tests against a real HA; see
[testenv/README.md](testenv/README.md).

Releases: agent and integration share one version. Bump it in
`cmd/mira/main.go`, `custom_components/mira/const.py` and
`custom_components/mira/manifest.json`, then tag `vX.Y.Z`. The release workflow refuses a tag that does not match.

## License

Apache License 2.0, see [LICENSE](LICENSE).
