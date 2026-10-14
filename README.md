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
./lares -config lares.yaml run
```

Requirements: a llama.cpp server (`llama-server --parallel 3`, one slot per
channel keeps each prompt cache warm), Home Assistant with the MCP server
integration, and a dedicated HA user with a long-lived token.

Security: locks, alarm panels and other configured entities only run after
the user confirms in the next turn. After a turn has read web content,
critical actions are blocked for the rest of that turn. Background jobs only
get read-only tools. These rules are enforced in code, not only in the prompt.

## Integration

1. HACS → Custom repositories → `https://github.com/sker65/lares`, type Integration.
2. Install **Lares**, restart Home Assistant.
3. Settings → Devices & services → Add integration → Lares: agent URL and the
   agent's `server.token`.
4. Settings → Voice assistants: choose **Lares** as the conversation agent.

## Development

```sh
go test -race ./...                                   # agent
uv venv --python 3.14 && uv pip install -r requirements_test.txt
.venv/bin/python -m pytest                            # integration
```

Releases: bump `version` in `custom_components/lares/manifest.json`, then tag
`vX.Y.Z`. The release workflow refuses a tag that does not match.
