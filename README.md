<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/mira-logo-dark.png">
    <img src="docs/brand/mira-logo.png" alt="Mira" height="96">
  </picture>
</p>

# Mira

A local, always-on household agent for Home Assistant. It runs next to a
llama.cpp server, talks to you through Assist voice satellites and Telegram,
controls the house through Home Assistant's MCP server, and can speak up on
its own when something needs attention.

This repository holds both parts, released together under one version:

| Part | Where | What it does |
| --- | --- | --- |
| Agent (Go) | `cmd/mira`, `internal/` | Prompting, tools, memory, security policy, Telegram |
| Integration (HACS) | `custom_components/mira` | Thin Assist conversation agent that forwards each turn to the agent |

The integration does not use Home Assistant's LLM API and runs no tools. All
logic lives in the agent.

## Agent

```sh
go build -o mira ./cmd/mira
cp mira.example.yaml mira.yaml        # then edit; secrets come from ${ENV}
./mira -config mira.yaml check        # probe the LLM, Home Assistant and MCP servers
./mira -config mira.yaml chat "Wie warm ist es im Wohnzimmer?"
./mira -config mira.yaml profile      # compact the remembered facts into the profile now
./mira -config mira.yaml run
```

Requirements:
- An OpenAI-compatible LLM server: llama.cpp (`llama-server --parallel 3`,
  `llm.slot_pinning: true`) or LM Studio (`slot_pinning: false`; it picks
  slots by common prefix, which works well because all channels share the
  start of the prompt).
- Home Assistant with the "Model Context Protocol Server" integration. Use
  `/api/mcp/assist` as the MCP URL: it works for a non-admin user.
- A dedicated, non-admin HA user with a long-lived token.

Web UI: `http://<agent>:8787/ui` edits the notifier, and the watchers, rooms
and persons unless the HA integration manages them (see below); it lists open notices and the memory. It asks for `MIRA_API_TOKEN`
once. Changes are stored in the database on top of `mira.yaml` (the file is
never rewritten; each section can be reset to it) and apply at once. The page
is in German or English, following the browser, with a switch in its header.

Models: `llm` is the main model; `models` adds more by name, local or in
the cloud (any OpenAI-compatible endpoint, e.g. Gemini), and `routing` says
which serves voice, Telegram and background jobs. With `routing.delegate`
the conversation model gets a `delegate` tool that hands harder tasks
(planning, explanations, research) to a stronger model and reads back its
answer. Only the local main model is warmed up; a cloud model is billed per
token, and each turn logs its model and token counts.

Reminders: "remind me tomorrow at seven" and timers are built in
(`reminder_set`). At the time Mira speaks the reminder in the room it was
asked in, or sends it privately.

Notices can be spoken or shown: per room, `notifier.room_mode` is
`announce` (spoken when someone comes in) or `indicate`, where the room's
indicator light, such as an Echo's LED ring, pulses while notices wait and
"Hast du eine Benachrichtigung?" reads them out (`notices_read`). When you
talk to a satellite anyway, what waits for its room follows the answer
("Licht ist an. Übrigens: ..."), except after a question back and in quiet
hours. Urgent notices and reminders are always spoken.

Skills: folders under `skills/` with a `SKILL.md` in the Agent Skills format
(frontmatter `name`, `description`; Mira's settings, calendars and schedules
under `metadata.mira`). Enable them under `skills.enabled` with their
settings. The prompt lists them in one line each; Mira opens a skill with
`skill_open` when a question needs it. Skills run no code: they use the
core's building blocks, such as `calendar_events`, which reads a skill's
calendar by name so its URL never reaches a model. A skill's schedules run
as background jobs and may queue one notice. `mira skill NAME` runs a
schedule now. The first skill, `abfall`, reads a waste collection calendar
and reminds the evening before.

Languages: `prompt.language` is `de` or `en`. It sets the language of the
prompt, of the notices and of everything Mira writes on her own; she still
answers in the language she is spoken to in. The English prompt is written
but not yet tuned against a model (see `internal/prompt/testdata/prefix_voice_en.txt`).

Security: locks, alarm panels and other configured entities only run after
the user confirms in the next turn. After a turn has read web content,
critical actions are blocked for the rest of that turn. Background jobs
cannot run tools that change anything. Feed text is data and can never raise
an urgent notice. These rules are enforced in code, not only in the prompt.

## Integration

1. HACS → Custom repositories → `https://github.com/sker65/mira`, type Integration.
2. Install **Mira**, restart Home Assistant.
3. Settings → Devices & services: the agent announces itself by mDNS
   (`_mira._tcp`, `server.announce`) and shows up under **Discovered**; it
   only asks for the agent's `server.token`. Otherwise Add integration → Mira
   with URL and token.
4. Settings → Voice assistants: choose **Mira** as the conversation agent.

The integration imports the agent's watchers, persons and rooms as
subentries of its entry (Watcher, Feed, Person, Raum), edited there with Home
Assistant's entity and area pickers. Each change is checked by the agent
before it is saved; what it refuses shows in the form. While the integration
manages them, the agent's web UI shows those sections read-only. Removing the
integration hands them back. The device also has a quiet-mode switch, a
pending-notices sensor and a button to compact the memory profile now.

## Development

```sh
go test -race ./...                                   # agent
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # integration
go run ./cmd/testha up                                # test Home Assistant in Docker
```

`testenv/` holds a throwaway Home Assistant for tests against a real HA; see
[testenv/README.md](testenv/README.md).

Releases: bump `version` in `custom_components/mira/manifest.json`, then tag
`vX.Y.Z`. The release workflow refuses a tag that does not match.
