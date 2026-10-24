# MCP servers in skills

Status: built. Scenarios: [skill-mcp.feature](skill-mcp.feature).

## Goal

MCP servers flood the context with tools: every tool of every server in `mcp_servers` is in the prompt of every
conversation, also when nobody needs it (Paperless-ngx's server has 44, most of them writing). Instead, a **skill can
bring its MCP server**: the server's tools are in no conversation until the skill is opened, and then only the tools
the skill names. Everything about it is in one place, the skill; a skill from the store brings its server along, and
the user only enters address and token.

## In the skill

```yaml
# skills/paperless/SKILL.md
metadata:
  mira:
    settings:
      url:   { description: Adresse des Paperless-MCP-Servers, required: true }
      token: { description: Paperless-Token, required: true, secret: true }
    mcp:
      transport: http                  # http (default) or stdio, as under mcp_servers
      url: "{{ url }}"
      token: "{{ token }}"
      web_content: true                # what it returns is third-party text
      tools: [query_documents, get_document_content]
```

```yaml
# mira.yaml: only the skill's settings, as for every skill
skills:
  paperless: { url: http://paperless-mcp:3000/mcp, token: ${PAPERLESS_TOKEN} }
```

- **The fields** are those of an entry under `mcp_servers` (`transport`, `url`, `token`, `command`, `args`, `env`,
  `read_only`, `web_content`), plus `tools`. The server's name is the skill's name; its tools are called
  `<skill>__<tool>` as for every MCP server.
- **Placeholders** from the skill's settings are filled in, secret ones too: they go to the connection, never into a
  prompt.
- **`tools`** names the tools the skill uses; only those are ever offered. Without it, all of the server's tools. The
  list is the control: whoever writes the skill decides what Mira may do with the server (a read-only token is not
  required).
- **`conversation: false`** keeps the tools out of the conversation: only the skill's agents and its schedules get
  them. The default is `true`. With a research agent (below) the documents never enter the conversation's history.

## When the tools are there

- **Not before the skill is opened.** The shared prefix holds no tool of a skill's server.
- **Opening the skill** (`skill_open`) in a conversation makes its tools available **for the rest of that
  conversation**, until the session expires. Another conversation does not get them.
- **The skill's schedules** (`skill.task` running for that skill) get its tools.
- **The skill's agents** (`metadata.mira.agents`) get the tools their `tools` list names, whether the conversation,
  a thread or Home Assistant calls them (agenten.md, Factory). Such an agent with `conversation: true` is a tool of
  the conversation once the skill is opened:

  ```yaml
  # skills/paperless/SKILL.md
  metadata:
    mira:
      mcp: { …, conversation: false, tools: [query_documents, get_document_content] }
      agents:
        paperless.recherche:
          description: Durchsucht die Dokumente mit einem Ziel und antwortet mit dem Ergebnis und den Quellen.
          context: background
          tools: [paperless__query_documents, paperless__get_document_content]
          rights: [read_house, web]    # web: its tools return third-party text (web_content)
          conversation: true
          instructions: …
  ```
- **The price on a local model:** tools stand at the front of the prompt; when they come in mid-conversation, the
  prompt cache no longer fits and the next turn computes the prompt anew (on LM Studio up to about 48 s cold). Only
  conversations that use the skill pay it; on a cloud model it does not matter. Accepted.

## Connection and problems

- Mira connects to a skill's server when it starts, as to the servers under `mcp_servers`, and again when the
  connection drops.
- A server that cannot be reached does not stop Mira, nor the skill: the skill is there, its tools are missing until
  the server answers, and Home Assistant shows it under Repairs.
- A tool the skill names that the server does not have is a problem of the skill; the others are offered.
- A skill's `mcp` section with a mistake (no url, an unknown transport) is a problem of the skill: it does not start.

## Safety

- `web_content: true` makes what the server returns third-party text, as for any MCP server: after it, Mira asks
  before keeping or passing anything on, and critical actions are locked.
- `read_only` decides, as for any MCP server, whether background work may use the tools at all.
- The rights of agents apply as for every tool (agenten.md, Rights).

## Decided

1. A skill brings its MCP server in its `mcp` section; everything is in one place (variant 2).
2. Its tools are there from opening the skill until the conversation's session expires (2A).
3. The prompt cache miss on a local model when the tools come in is accepted (3A).
4. The skill's schedules get its tools.
5. The skill's `tools` list is the control; a read-only token is not required.
6. The skill's agents get its tools (those their `tools` list names); with `conversation: false` the conversation
   does not, only the skill's agents and schedules.
