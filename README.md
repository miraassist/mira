# Lares

A local, always-on household agent for Home Assistant. It runs next to a
llama.cpp server, talks to you through Assist voice satellites and Telegram,
controls the house through Home Assistant's MCP server, and can speak up on
its own when something needs attention.

This repository holds both parts, released together under one version:

| Part | Where | What it does |
| --- | --- | --- |
| Agent (Go) | `cmd/lares`, `internal/` | Prompting, tools, memory, security policy, Telegram |
| Integration (HACS) | `custom_components/lares` | Thin Assist conversation agent that forwards each turn to the agent |

The integration does not use Home Assistant's LLM API and runs no tools. All
logic lives in the agent.

## Agent

```sh
go build -o lares ./cmd/lares
cp lares.example.yaml lares.yaml        # then edit; secrets come from ${ENV}
./lares -config lares.yaml check        # probe the LLM, Home Assistant and MCP servers
./lares -config lares.yaml chat "Wie warm ist es im Wohnzimmer?"
./lares -config lares.yaml profile      # compact the remembered facts into the profile now
./lares -config lares.yaml run
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
and persons unless the HA integration manages them (see below); it lists open notices and the memory. It asks for `LARES_API_TOKEN`
once. Changes are stored in the database on top of `lares.yaml` (the file is
never rewritten; each section can be reset to it) and apply at once.

Security: locks, alarm panels and other configured entities only run after
the user confirms in the next turn. After a turn has read web content,
critical actions are blocked for the rest of that turn. Background jobs
cannot run tools that change anything. Feed text is data and can never raise
an urgent notice. These rules are enforced in code, not only in the prompt.

## Integration

1. HACS → Custom repositories → `https://github.com/sker65/lares`, type Integration.
2. Install **Lares**, restart Home Assistant.
3. Settings → Devices & services: the agent announces itself by mDNS
   (`_lares._tcp`, `server.announce`) and shows up under **Discovered**; it
   only asks for the agent's `server.token`. Otherwise Add integration → Lares
   with URL and token.
4. Settings → Voice assistants: choose **Lares** as the conversation agent.

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

Releases: bump `version` in `custom_components/lares/manifest.json`, then tag
`vX.Y.Z`. The release workflow refuses a tag that does not match.
