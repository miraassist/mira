# Remote agents (A2A) – Spec

Status 2026-11-14: specified (decisions 1 to 7, 13 and 16 by the maintainer, the others taken while writing, to be
confirmed), not built. Mira is an A2A **client** only; Mira as an A2A server is not part of this spec.

The testable rules are numbered scenarios in [remote-agents.feature](remote-agents.feature); each Go test for one
carries the number in its name (`TestRM07_…`).

## Goal

Other agents can do work Mira cannot: a research agent with its own tools, a coding agent, an agent that runs in the
household's private cloud or on the same machine as Mira. A2A (Agent2Agent, version 1.0) is the open protocol for
handing them that work. Mira hands work to them the way she hands it to her own helpers today: a **skill** describes
the agent, and the agent fills a **thread** ("Vorgang").

1. A skill `recherche` describes a remote agent: its address and a token, nothing else.
2. Anna: "Finde heraus, welche Wärmepumpe für unser Haus passt, und schick mir eine Übersicht." Mira starts the
   skill's thread and sends the request to the remote agent. "Ich habe das weitergegeben und melde mich."
3. The remote agent, Hermes, asks back: "Hermes fragt: Wie groß ist die Wohnfläche?" Anna gets it as a message (an **agent request**), on
   her phone or at the satellite where she is recognised. "140 Quadratmeter." Mira passes the answer on.
4. An hour later the remote agent is done and reports it to Mira. The thread's finish runs, and Anna gets the
   overview as a message of the thread.

## Building blocks

The concept it hangs on: **agents fill a thread** ([Agents](agents.md), Continuation). A skill's target names the
agent that fills a field or the plan; Mira calls it asynchronously with the route "thread"; the result is filled in
as if the user had said it; a failure makes the thread ask the user; the thread's end cancels the agent.

A remote agent is such an agent of a skill (`metadata.mira.agents`, [Agents](agents.md), Factory). What departs
from a model agent, and why:

| Part | Model agent | Remote agent | Why |
|---|---|---|---|
| Description, input, output | in the skill | from the agent card (decision 9) | the remote agent knows best what it does; the skill would only copy it |
| Model, context, tools, rights, max_rounds | in the skill | none | the remote agent runs its own model and tools; Mira sees none of it |
| Called by | another agent, Home Assistant, the code, the conversation | only a thread (decision 3) | A2A work is slow and has a life of its own; a thread holds it |
| Asking back | not possible | an **agent request** (decision 5) | A2A has a state for missing input (`input-required`) |
| The result arrives | the run ends | a push of the remote agent, else a stream (decision 4) | the work runs elsewhere |

Further building blocks: the [message kinds](messages.md) (a new kind `agent_request`), the skill's settings with
secrets in `mira.yaml` ([Skill MCP](skill-mcp.md), placeholders), [Retries](retry.md) (`internal/retry`), the
action `mira.agent` ([Agents](agents.md), Calling; a new optional field `skill`), and the official Go SDK
`github.com/a2aproject/a2a-go/v2` (client), not an own implementation of the protocol.

## Decisions

### By the maintainer (2026-11-14)

| # | Question | Decision |
|---|---|---|
| 1 | Where a remote agent is described | As an agent of a skill, `metadata.mira.agents`, only that it is remote. Its address and token are settings of the skill: in `mira.yaml`, written by Mira's page; no second place |
| 2 | Who starts it | A thread only: the skill's target; Home Assistant through `mira.agent` with the new field `skill`, which starts that skill's thread (decision 7). Neither `delegate` nor the conversation call it directly |
| 3 | In the conversation | A2A is always a thread, never directly part of a turn |
| 4 | How the result arrives | By push (A2A push notification to the Mira service) when the agent card offers it and Mira has her address for remote agents (decision 13), else by a stream (SSE). Push, not polling |
| 13 | Push address | The push endpoint is the Mira service's. Mira does not know how she is reached from outside (in a container and in the HA app she only listens on all interfaces), so it is a setting: `server.url`, Mira's address for remote agents, in `mira.yaml`, written by Mira's page. Without it Mira uses the stream (2026-11-14, 13A) |
| 16 | A login | Mira logs in at the remote agent through the skill's **connection** ([Connections](oauth.md)) when the agent card asks for OAuth2: the skill has an `oauth` section, Home Assistant does the login, Mira sends the token. A login the remote agent needs during its work (`auth-required`) becomes later a special agent request ("Hermes braucht eine Verbindung … Geh dafür auf Miras Seite …"); deferred (step 7), until then it ends the call as failed (2026-11-14, 16A) |
| 5 | When the remote agent asks back (`input-required`) | A new kind of message, `agent_request`, with its thread; the owner's answer goes back to the remote agent |
| 6 | Who may use it, what it gets | Whoever may use the skill may use its remote agent. It gets only what the skill's target puts into its input: the thread's fields and the request. No setting of its own |
| 7 | What thread `mira.agent` starts | The skill's own thread, as by voice: `mira.agent` names the skill, not the agent; the call's input is the request, its receiver the owner. No thread type of its own |

### Taken while writing (2026-11-14, to be confirmed by the maintainer)

| # | Question | Decision |
|---|---|---|
| 8 | Name of the kind | `agent_request` ("Agentenanfrage"), as the maintainer proposed; in the glossary |
| 9 | Description, input, output | From the agent card. The skill may name one of the card's skills (`a2a_skill`); then its description and input/output modes are used, else the card's own. The skill's `description` overrides the card's when given |
| 10 | Input to the remote agent | One A2A message: a text part with the request and the fields the target's `input` names, worded as the target's `input` says; an input object becomes a data part beside it |
| 11 | The result | The task's artifacts: all text parts joined, in order; a data part as JSON. That fills the field (or is the plan) as an agent's answer does today. Files (`raw`, `url`) are named in the text by their file name and not downloaded in this spec |
| 12 | No way for the result | A card without streaming, when Mira cannot use push (the card offers none or `server.url` is not set): the remote agent is refused when the skill is loaded, since Mira would have to poll `GetTask`. The skill's problem says so and names what is missing; the skill stays, without that agent |
| 14 | Push token | Per call a random token Mira makes; the remote agent sends it back on every push (`Authorization: Bearer`). A push with an unknown or wrong token is refused (401) and logged |
| 15 | A push comes twice | A2A delivers at least once. Mira keeps the state per task; a push that repeats a state or an artifact already taken changes nothing |
| 17 | The agent card | Read when the skill is loaded (Mira's start, a skill reload), kept in memory; an unreachable card is tried again like an MCP server that is down (from the retry interval up to 30 minutes). Signed cards are not checked in this spec |
| 18 | http or https | Both: the address is the user's choice. A2A requires TLS in production; a remote agent in the home network often has none. The guide says that a token over http is readable in the network |
| 19 | When the thread ends | Cancelled or expired: Mira sends `CancelTask`. A failing cancel is logged, nothing more |
| 20 | A restart of the Mira service | The open calls are in `agent_runs` with the task id; at the start Mira asks each once with `GetTask` and subscribes again (`SubscribeToTask`) where it streams. A state missed meanwhile is taken from the answer. No polling afterwards |
| 22 | Who asks | A remote agent is named in everything Mira says and shows by its **name**: the agent card's `name`, or the skill's `name` for the agent when it gives one ("Hermes fragt: …", "Hermes konnte das nicht: …"). "Der entfernte Agent" is never said to the user (2026-11-14, by the maintainer; the source of the name taken while writing) |
| 21 | The answer to an agent request | Goes to the remote agent as a new message with the same task id; the call waits on. A second agent request of the same task replaces the first, if that is still unheard |

## What a skill writes

```yaml
metadata:
  mira:
    settings:
      url:   { description: Adresse des Agenten, required: true }
      token: { description: Token des Agenten, secret: true }
    agents:
      recherche:
        remote:
          url: "{{ url }}"          # the agent's base URL; the card is at /.well-known/agent-card.json
          token: "{{ token }}"      # sent as the card's security scheme asks (bearer or API key)
          a2a_skill: research       # optional: one skill of the card
        name: Hermes                # optional: how Mira names it to the user; else the card's name
        description: Recherchiert ausführlich im Netz.   # optional: overrides the card's
    target:
      plan: false
      fields:
        - name: frage
          description: Was herausgefunden werden soll.
        - name: ergebnis
          description: Die Übersicht.
          agent: recherche
          input: { input: "{{ frage }}" }
      finish: …
```

The skill's other fields (`model`, `context`, `tools`, `rights`, `max_rounds`, `instructions`, `conversation`) are
not allowed with `remote`; a skill that sets one has a problem when it is loaded and is left out (as an unknown
agent today).

## What it does, step by step

### A. Loading the skill (Mira's start, a skill reload)

1. Mira fills the placeholders from the skill's settings and reads the agent card (one GET).
2. She takes from the card: the transports (the first one the SDK supports), the security scheme, push and
   streaming, the skill (decision 9). The agent is ready.
3. **The login:** a bearer or API key scheme takes the skill's `token`; an OAuth2 scheme takes the token of the
   skill's connection (decision 16). The skill's connection not yet made → the agent waits for it, and the skill's
   page says "Verbinden", as for every connection.
4. **Fails:** card unreachable or not JSON → log line with the URL and the error (the body's start), the agent is
   unavailable and tried again (decision 17); a thread that needs it meanwhile fails that field (C.5). No way for
   the result → the skill's problem (decision 12). A security scheme Mira does not support (OpenID Connect, mTLS),
   or OAuth2 without an `oauth` section in the skill → the skill's problem, naming the scheme.

### B. Sending the work (the thread's field or plan is due)

1. As for every agent of a target: once every field its input names is there, Mira starts the call with the route
   "thread". The call is a row in `agent_runs` (as today), with the remote task id once known.
2. Mira sends one message (`SendMessage`, not blocking: `returnImmediately`), with the push configuration when she
   uses push (decisions 4, 13, 14), else `SendStreamingMessage`.
3. The answer is a task (or, rarely, a direct message: then that is the result at once, C.1).
4. **At a satellite the turn waits** as for every agent of a thread: up to 10 seconds ([Agents](agents.md));
   what comes within them is in the same answer, else Mira says she will come back.
5. **Fails:** see Errors and Retries. A refused or failed send is a failed agent (C.5).

### C. What comes back

1. **Completed:** the artifacts become the answer (decision 11), the field is filled, the log says "agent recherche:
   …", and the thread goes on as always.
2. **Working, submitted:** nothing to do; the thread shows the field as being filled.
3. **Input required:** an **agent request** (D).
4. **Cancelled** (not by Mira), **rejected**, **failed**, **auth required** (until step 7, decision 16): a failed
   agent (5).
5. **A failed agent:** as today: a log line with the agent, the task id and the remote agent's status text; the
   thread asks the user for that field, as a message of the thread.

### D. An agent request

1. The remote agent's status message (its text parts) becomes an entry of a message of the kind `agent_request`
   to the thread's owner, with the thread; the entry names the agent (decision 22): "Hermes fragt: …".
2. The kind: one message per question, it waits and is gathered like the other kinds that wait, it is announced as
   they are, sent privately to a person who cannot hear it; it ends with its thread.
3. The owner answers (by voice, Telegram, the companion app): the answer is a turn of the thread; the model passes
   it on with the tool `agent_answer` (thread, text). Mira sends it to the remote agent with the same task id.
4. **Fails:** the answer cannot be sent (retries exhausted, a refusal) → the message stays, a log line, and Mira
   tells the owner ("Ich konnte die Antwort nicht weitergeben: …"); the thread waits on. The owner does not answer
   → the thread's expiry ends it (decision 19).

### E. A push arrives

1. `POST /v1/a2a/push/<call id>` on the Mira service, token in the header (decision 14).
2. Mira answers 200 at once and hands the event on; the handler never blocks.
3. The event is taken as in C, once (decision 15).
4. **Fails:** unknown call id or wrong token → 401, log line with the call id and the sender's address; a body that
   does not parse → 400, log line with its start; a push for a call that has ended → 200, nothing done, log line.

### F. From Home Assistant: `mira.agent` with `skill`

1. Fields: `skill` (new, optional), `input` (the request, a text), `receiver` (the owner: a person or a room, as the
   skill's target is bound). `agent`, `mode`, `timeout`, `on_timeout`, `delivery` are not allowed with `skill`.
2. Mira starts the skill's thread with the request, as if the owner had asked by voice; the response is
   `{thread_id}`.
3. **Fails:** unknown skill, a skill without a target, a receiver that does not fit the target's owner → 400 with
   the reason; the action fails with it. An older service (no `skill`) → the integration's error "Mira kann das erst
   nach einem Update" (as `mira.check`).

### G. A restart

As decision 20. **Fails:** `GetTask` unreachable → retried (Retries); after the bound the call is a failed agent
(C.5).

## What it costs

| When | What | Grows with |
|---|---|---|
| Loading the skills | one GET of the agent card per remote agent | the number of remote agents |
| A field or plan due | one `SendMessage` (with the push configuration) | — |
| While it works | nothing with push; one open stream with SSE | the number of open calls (streams) |
| Per state the remote agent reports | one push to Mira (or an event in the stream); one database write | the remote agent's updates |
| An agent request | one message; the answer one `SendMessage` | the questions |
| The thread ends early | one `CancelTask` | — |
| Mira's start | one `GetTask` per open call, one `SubscribeToTask` per open stream | open calls |

What the remote agent spends (its model, its tools) is its own and not Mira's.

## Retries

[Retries](retry.md), `internal/retry`. Retryable: a lost connection, a timeout, 429 (its `Retry-After` is kept),
5xx. Fatal: 400, 401, 403, 404, an A2A error in the answer (e.g. `TaskNotFound`, `UnsupportedOperation`,
`VersionNotSupported`). Bound: 3 tries, exponential from 1 s. Giving up: the agent card → decision 17; a send, an
answer, a `GetTask` → a failed agent (C.5) or the message of D.4. A retried `SendMessage` keeps its `messageId`.
`CancelTask` is tried once.

## Errors

| Where | What happens | Who learns of it |
|---|---|---|
| Card unreachable, not parseable | agent unavailable, tried again | log (URL, error, body's start); a thread needing it: the user via C.5 |
| No way for the result; unknown security scheme; OAuth2 without `oauth`; a model field with `remote` | skill problem | the skill's problem on Mira's page, log |
| Send refused (401/403) | failed agent; with a connection, Mira renews its token once first (as for every connection) | log (agent, URL, status), the user via C.5 |
| Task failed, rejected, cancelled by the remote agent, auth required | failed agent | log with the status text, the user via C.5 |
| Push with wrong token, unknown call | 401 | log (call id, sender's address) |
| Push body not parseable | 400 | log (body's start) |
| Answer to an agent request not sent | message stays | log, the owner: D.4 |
| Cancel fails | nothing more | log |
| `mira.agent` with `skill` refused | 400 | the action's error |

## Checks on the test HA

A state table: each row is checked against a remote agent before step 2 is built.

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| 1 | card read | the card's JSON at `/.well-known/agent-card.json` | GET | — |
| 2 | sent, push configured in the same send | task `submitted`/`working` | the answer of `SendMessage` | — |
| 3 | working → completed | push with the task and its artifacts | push route | — |
| 4 | working → input required → (answer) → working | push with status message; the answer with the same task id is accepted | push route; the answer's reply | — |
| 5 | working → failed / rejected | push | push route | — |
| 6 | Mira cancels | task `canceled` | the answer of `CancelTask` | — |
| 7 | without push: the stream | SSE events until a final state | the stream | — |
| 8 | Mira restarted during work | `GetTask` gives the state reached meanwhile | `GetTask` at the start | — |
| 9 | a push repeated | the same state twice | push route | — |
| 10 | a retried `SendMessage` with the same `messageId` | one task or two? | the answers | — |

## Steps

1. **The remote agent in a skill:** `remote` in `metadata.mira.agents`, the settings' placeholders, reading the agent
   card, the login with a token or the skill's connection, the skill's problems (decisions 9, 12, 16, 17, 18; A).
   Glossary done.
2. **Sending and the result:** a target's field or plan filled by a remote agent; push route and stream; failures;
   cancelling at the thread's end (B, C, E; decisions 10, 11, 13–15, 19). The setting `server.url` on Mira's page.
3. **The agent request:** the kind `agent_request`, the tool `agent_answer` (D; decisions 5, 8, 21);
   [Messages](messages.md) gets the kind.
4. **A restart** (G; decision 20).
5. **From Home Assistant:** `mira.agent` with `skill` in the service and the integration (F; decision 7).
6. **The guide** (de and en): a skill with a remote agent, the address and the token or the connection, Mira's
   address for remote agents, http and https.
7. **Deferred: a login during the work** (`auth-required` as a special agent request, decision 16).
