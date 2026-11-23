# Remote agents: transport and call loop – Spec

Status 2026-11-23: specified (decisions 1 to 5 by the maintainer, 6 to 14 taken while writing, to be confirmed);
steps 1 to 3 built. Part of [Remote agents](remote-agents.md), step 11; its scenarios are in
[remote-agents.feature](remote-agents.feature) (RM-27 to RM-31).

## Goal

A call of a remote agent is a conversation over the network that runs asynchronously in both directions: Mira sends
messages (the work, an answer to an agent request, a cancel), the agent reports its task's states (the answer to a
send, a stream, pushes to Mira's address). Every message carries its ids (task, context, message, the call's id in
the push address), so what belongs to what is never in doubt; only the **order** in which things arrive over two or
three HTTP connections is not fixed by anyone.

Until now that order was assumed in places, and the network and the call's logic were mixed in one type: ending a
call cut the HTTP request still on its way, "the answer reached the agent" was read from that request alone, and
state was written from several goroutines at once. Seen in the release of 0.30.1 (decision 64 of
[Remote agents](remote-agents.md)): Mira said she could not pass an answer on that the agent had, and a thread was
never finished.

So the call is split by concern:

1. **The transport** sends and receives. It is opened for a call, may consist of several HTTP connections (a stream
   with SSE, a send and pushes to Mira's push route), and has only its own state: open or closed. It knows nothing of
   tasks, questions, threads or the store.
2. **The call loop** handles what the transport received and what Mira asks, one after the other, in one place. Only
   it decides what a message means, when the call ends, when the transport is closed, and only it writes what the
   call knows.
3. **The thread** gets the call's question and result from the loop, as today; the loop tells it in its order.

## Building blocks

All decisions of [Remote agents](remote-agents.md) stay: push or stream (4, 12, 13), the push token (14), a push
twice (15), the cancel (19), the restart (20, 44 to 46), the answer to an agent request (21, 43), what counts as
reaching the agent and when a send is tried again (26, 38, Retries), a stream that ends early (35), the order of
pushes and a stale state (37, 37a), and the result (11, 36). They move to the layer they belong to; the behaviour a
user or a remote agent sees does not change, except the two errors above. The official Go SDK's client
(`a2aclient`) stays the protocol: the transport uses it, it is not replaced. `internal/retry` stays the building
block for retries, now only in the transport.

## Decisions

### By the maintainer (2026-11-23)

| # | Question | Decision |
|---|---|---|
| 1 | The structure | Transport, call loop, thread logic, each with its own concern ("separation of concerns") |
| 2 | What the transport is | Opened for a call so that Mira can send and receive; it may be two or three HTTP connections (a stream with SSE, a send, Mira's push route). Its state is its own (open, closed), never a task's or a thread's |
| 3 | Where messages are handled | In one loop per call, by handlers, one after the other |
| 4 | Who closes the transport | The loop alone, when the call ends |
| 5 | Who stores | Only the loop stores what has to do with the call's messages (its row in `agent_runs`, its remote state, its end) |

### Taken while writing (2026-11-23, to be confirmed by the maintainer)

| # | Question | Decision |
|---|---|---|
| 6 | One transport per call or per agent | Per call: a stream belongs to one call, and the push address carries the call's id and token. The agent (its card, its SDK client with the login) stays shared, as today |
| 7 | Where the transport lives | Its own package, `internal/a2a/transport`. It imports neither the store, the threads nor the call logic; a test checks the imports (RM-27), so the separation does not wear off |
| 8 | What the transport reports | One ordered channel per call to its loop: the answer to a send (with the message's id), a send that failed (with whether it reached the agent), an event (from the stream or a push), the end of a stream (with why), the answer of `GetTask`, the outcome of `CancelTask`. Nothing else |
| 9 | Retries | In the transport, as [Retries](retry.md) and decision 26 say: a send only when it did not reach the agent, reads (`GetTask`, `SubscribeToTask`) as reads; giving up is reported to the loop, which fails the call |
| 10 | Closing does not cut a send | Closing stops receiving: the stream ends, the push route no longer hands this call's pushes on (a late push is answered as for an ended call). A send with push on its way is finished and its outcome reported; only the stop of the Mira service cuts it, and then the call stays running in its row (G). With a stream the send and its receiving are one connection: the stream is the send, and closing ends it (the call has ended, so nothing more is wanted from it) |
| 11 | When an answer counts as passed on | When the send's answer comes, **or** when the agent reports anything for the call after the answer was sent (a state of the task, a new task in its context, a message), whichever comes first. A send that fails after that changes nothing but a log line. "Not passed on" only when the send failed before any such report |
| 12 | The order towards the thread | The loop answers the caller of the answer before it hands on the result that came after it; question and result are handed on by the loop alone, never from the transport's goroutines |
| 13 | When a loop ends | When its call ended and no send of it is on its way, so a cancel during the first send still learns the task and cancels it (decision 19); at the stop of the service, without ending the call |
| 14 | What others read | `Asking` (the open question for the thread's fields) reads a copy of the call's state the loop publishes after each step; nobody else reads or writes the call's state |

## What it does, step by step

### The transport

| State | Means | Goes to |
|---|---|---|
| open | sends and receives | closed: the loop closes it, or the service stops |
| closed | receives nothing more; a send with push on its way is still finished (decision 10) | — |

A **stream transport** (the card offers streaming): a send is `SendStreamingMessage`; its events are the send's
answer and the call's events; the end of the stream is reported with why (closed, broken off). `Follow` subscribes
to the task again (`SubscribeToTask`, tried as a read). A **push transport** (no stream, Mira's address set): a send
is `SendMessage` with `returnImmediately` and the push configuration (the call's address and token); its answer is
reported; pushes come from Mira's push route.

**Mira's push route** (`POST /v1/a2a/push/<call id>`, E of [Remote agents](remote-agents.md)) checks the call and
its token against the open push transports and hands the parsed event to that transport; it answers at once and
never blocks. A call no longer open: the route answers as today (200 for an ended call with its own token, read from
its row; else 401).

Each operation (send, follow, get, cancel) runs in its own goroutine of the transport and reports its outcome on the
call's channel. **Fails:** as [Remote agents](remote-agents.md), Retries; every retry and every giving up has its log
line in the transport, with the agent, the call, the address and the error; the loop gets the error.

### The call loop

One goroutine per open call, started when the call starts (`Send`) or is taken up after a restart (`Resume`). Its
inputs, in one queue: what its transport reports, and from Mira `answer`, `cancel` and the stop of the service.

| Input | What the loop does |
|---|---|
| answer to the first send | takes the task (C), stores the task id |
| an event (stream, push, the answer of a send) | takes it once (decisions 15, 37a): a new state is stored; `input-required` hands the question on (`Ask`); `completed` ends the call done and hands the result on (`Deliver`); a failed state ends it failed |
| a send failed | the first send: the call fails (C.5). An answer: the caller hears "not passed on" (D.4), unless decision 11 already counted it passed |
| a stream ended | when the task is open and asks nothing: `Follow` (decision 35); giving up fails the call |
| `answer` from the thread | an open question: a new message (id `<call id>-<n>`), stored, sent; the caller waits for decision 11 |
| `cancel` from the thread | ends the call cancelled; with a task: `CancelTask`; without one yet: when the send's answer brings it (decision 19) |
| the answer of `GetTask` (restart) | taken as an event; a task still working: `Follow` where Mira streams, else wait for pushes |
| the stop of the service | ends the loop without ending the call |

The call **ends** once (done, failed, cancelled): the loop writes the end to the row, closes the transport, hands
the result or failure on (not for a cancel), and runs on only while a send of the call is on its way (decision 13).

**Fails:** the row cannot be written → a log line with the call and the error, as today; the call goes on (its
state in memory). `Ask` fails → the call fails ("its question could not be passed on"). `Deliver` is the thread's;
it logs its own errors.

### The thread

Unchanged interfaces: `Deliver`, `Ask`, `Answer`, `Cancel`, `Asking`, `Failure`, `Names`. `Answer` returns when
decision 11 decides, or with the error of D.4. A thread change that meets another one is read again (`change`, as
decision 64 of [Remote agents](remote-agents.md)).

## What it costs

Per open call one goroutine for the loop, and while one runs, one goroutine per operation of the transport (a send,
a stream, a follow, a get, a cancel); no polling, no new calls to the agent, no new writes: the same writes as today,
from one place. Nothing grows with anything but the number of open calls.

## Errors

| Where | What happens | Who learns of it |
|---|---|---|
| A send, follow or get gives up | the loop fails the call (or the answer is not passed on) | log in the transport (agent, call, address, error); the user via C.5 or D.4 |
| A send fails after the agent already reported on the answer | nothing (decision 11) | log line naming the call and the error |
| A push for a closed transport | 200 with the call's own token (ended call), else 401 | log, as E |
| The row cannot be written | the call goes on in memory | log (call, error) |
| A loop's queue after the call ended | inputs other than a send's outcome are left | debug line |

## Steps

1. **The transport** (`internal/a2a/transport`): stream and push, send, follow, get, cancel, close, the push route's
   hand-over; retries and the classification of errors moved here. Built 2026-11-23. RM-27, RM-30 in Go
   (`internal/a2a/transport`).
2. **The call loop** in `internal/a2a`: one goroutine per call, the table above; `Host` keeps its methods and runs
   on the loops. Built 2026-11-23. RM-28, RM-29, RM-31 in Go (`internal/a2a`); every scenario RM-01 to RM-26 stays
   green.
3. **On the test Mira** with the fake A2A agent's container (push and stream): a field filled, an agent request
   answered, a cancel, a restart during the work. Run 2026-11-23 by voice at the fake Show, with three test skills
   in the scratchpad (push and stream asking back, push working slowly): the question said in the starting turn,
   the answer passed on and the result in the thread in that order (push and stream); a cancel sent `CancelTask`, and
   the agent's later push was answered as for an ended call; a restart while the agent worked took the call up
   (`GetTask`, working), its push completed it, and the thread's message went out. Found on the way, also in 0.30.1
   and not from this step: when the agent's result ends the thread within the turn that answered it, that turn fails
   storing its history (`FOREIGN KEY constraint failed`) and the satellite says "Something went wrong".
