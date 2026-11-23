# MCP servers: transport and session – Spec

Status 2026-11-23: specified (decisions 1 and 2 by the maintainer, 3 to 9 taken while writing, as they follow from
them); steps 1 and 2 built. Scenarios: [mcp-connections.feature](mcp-connections.feature) (MX-01 to MX-05).

## Goal

Mira's MCP host (`internal/mcp`) talks to every configured MCP server: Home Assistant, a search, the skills' servers.
Until now one object was both the connection and the logical session of a server: the SDK's client session. A call
that failed on it closed it and connected anew, also while other calls of other turns still ran on it. Those calls
then failed on the closed connection, took that for a lost session, connected once more and **ran their tool a second
time**, although the server may have done it already ("Milch auf die Einkaufsliste" twice). A second connect at the
same time overwrote the first without closing it; for a server started over stdio a whole program kept running.
Found 2026-11-23 while looking for the pattern of [Remote agents: transport and call loop](remote-agent-calls.md).

## Building blocks

The pattern of the remote agents' calls ([Remote agents: transport and call loop](remote-agent-calls.md)): a
transport that only sends and receives and has its own state, and one place per server that alone decides. The rule
of [Threads](thread-history.md), decision 9: what is ended is dropped only when everyone using it is done. The
retries of [Retries](retry.md), row 6, stay as they are; the official Go SDK stays the protocol.

## Decisions

### By the maintainer (2026-11-23)

| # | Question | Decision |
|---|---|---|
| 1 | The structure | As for the remote agents: a proper transport, separate from the logical session over it |
| 2 | Closing | A connection is closed only when nobody uses it any more ("dropped only when all are done") |

### Taken while writing (2026-11-23, following from 1 and 2)

| # | Question | Decision |
|---|---|---|
| 3 | The transport | `internal/mcp/transport`: one connection (the SDK's client session over HTTP or stdio) with its own state: open, retired, closed. Every call uses it for its duration; it imports nothing of the host's logic (tools, servers' config, prompts), a test checks it (MX-01) |
| 4 | Retiring | A connection that is replaced is retired: no new call gets it, and it is closed when its last call ended (MX-02) |
| 5 | The logical session | Each server has one loop that alone changes its state: which connection is current, its tools, its errors and waits. Connects, reconnects, refreshes of the tool list and a dropped connection go through it, one after the other; calls do not wait for it, they take the current connection |
| 6 | A call that failed on a lost session | Asks the loop for a connection: when another call has already replaced the failed one, it gets that one; else the loop retires the failed one and connects once. Two calls failing at once connect once (MX-03); nothing is overwritten without being closed |
| 7 | A connection Mira closed herself | Only at the stop of the service. A call on it is not repeated, whatever it answers, so no tool runs twice (MX-04); the SDK's close waits for a call already sent (at most `CallTimeout`), so the stop cuts no call either |
| 8 | The server's connection changes (`Reconnect`, a skill's login) | The current connection is retired, not closed: calls on it finish (MX-05); the loop connects anew |
| 9 | Run's rounds | Run asks the loops to connect a server that is due and waits for each, so a round is over when its connects are; the waits of [Retries](retry.md), row 6, are unchanged |

## What it does, step by step

1. **A call** (`Call`, `CallServer`, `CallSetup`, `Prompt`): it takes the server's current connection and uses it
   (one more use). None: `ErrServerDown`. The connection was retired meanwhile: it takes the current one again.
2. It calls the tool (or gets the prompt) within `CallTimeout` and ends its use. A retired connection whose last use
   ended is closed then.
3. **Failed on a lost session** (not an answer of the server, not a timeout, not a connection Mira closed): it asks
   the loop (decision 6) and repeats the call once on the connection it gets; none → the error, as before.
4. **The loop** handles, in order: a connect (Start, Run, `Wake`), a reconnect after a change of the connection
   (`Reconnect`), a replacement after a lost session, a refresh of the tool list (the server's announcement, a turn's
   `DeviceTools`, the refresh interval), and a connection the server dropped. Only it writes the server's state;
   readers (`Status`, the tool list) read it under its lock.
5. **The stop of the service** (`Close`): each loop closes its connection and ends; the SDK's close waits for the
   calls already sent, at most `CallTimeout`.

**Fails:** a connect that fails is kept and logged as before ([Retries](retry.md), row 6). A call that fails is the
caller's error, with the server and the tool (as before). A replacement that cannot connect: "mcp server
unreachable" and the call's error. A call on a connection Mira closed: its error, not repeated, no log line of its
own (the caller logs it).

## What it costs

Per server one goroutine for its loop and one that notices a dropped connection; no new calls to the servers. A
retired connection lives until its last call ended, at most `CallTimeout`.

## Steps

1. **The transport** (`internal/mcp/transport`): open, use, retire, close; who closed it. MX-01 in Go.
2. **The loop per server** in `internal/mcp`, the host on it; calls take the current connection. MX-02 to MX-05 in Go;
   the tests of the host and RY-40 to RY-42 stay green.
