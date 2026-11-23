# When something ends while others still use it – Spec

Status 2026-11-23: decided (the rule by the maintainer, the places following from it); built. Scenarios:
[endings.feature](endings.feature) (EN-01 to EN-08).

## Goal

The rule (2026-11-23, by the maintainer): **what ends is cleaned up only when everyone using it is done.** One party
being finished (a turn, a run, a wait, a subscription) does not cut, delete or answer for another that still uses the
same thing. Found by a search for the pattern after the remote agents' calls ([Remote agents: transport and call
loop](remote-agent-calls.md)), a thread's history ([Threads](thread-history.md), decisions 9 and 10), the MCP
connections ([MCP servers: transport and session](mcp-connections.md)) and the notifier's restart
([Messages](messages.md), A change of the configuration while a delivery runs), which have their own specs. These are
the remaining places.

## The places

| # | Place | What happened | Now |
|---|---|---|---|
| EN-01 | A turn waits for its thread's agents (`threads.waitFor`, `handOver`; [Agents](agents.md), Continuation; [Remote agents](remote-agents.md), decision 52) | A turn that ended (barge-in, its caller gone, its timeout) still took a result that came just then: it was neither said nor sent as a message. A second turn waiting on the same thread could have its wait removed by the first | A handshake: the result is offered, and the turn takes it only while it still answers; else, or once it stopped waiting, the agent's side sends it as a message. Each turn removes only its own wait |
| EN-02 | The wait video at a satellite (`waiting`, [Waiting indicator](waiting-indicator.md)) | A wait stopped while its video was being started was not stopped: Stop knew of no video yet, and the video played on after the answer | The video's start notes that it was sent; whoever comes last, Stop or the start, stops it (as the wait's timer already did) |
| EN-03 | The answering machine's and a clip's playback ([Answering machine](voicemail.md); a clip, `internal/clip`, [Music quiz](music-quiz.md)) | A run ended on purpose (a newer run, its thread, "Abbrechen") while its play call was on its way logged an error; a clip also told the next turn it had not played | A play call that fails because its run ended is no error: a debug line, no note to the next turn |
| EN-04 | A thread's work when the thread is cancelled ([Threads](threads.md)) | The cancelled work was logged as "thread work failed"; a turn still waiting for it (`InlineWait`) said the work failed and ended the thread a second time | A work cancelled with its thread is no failure: an info line; a turn still waiting says "Vorgang abgebrochen." The service translates the runtime's cancel into `threads.ErrWorkCancelled` |
| EN-05 | A subscription at Home Assistant (`ha/ws`, `issueSubscribe`) | A subscribe whose wait ran out gave up, though Home Assistant could still subscribe: the subscription stayed there and its events went nowhere. A handler could be called once more after its caller unsubscribed | The answer stays awaited; a late yes is unsubscribed at once (an info line); a lost connection ends the wait. A cancelled subscription's handler is not called again |
| EN-06 | The stop of the Mira service (`app.close`) | The store closed while turns still ran: their answers were not stored ("database is closed") | The stop waits for the running turns, then closes the store. Its limit (10 s) is the stop's safety net only; past it a warning names it |
| EN-07 | A Starlark tool's run (`internal/startools`, [API tools](api-tools.md)) | A tool whose caller ended was cancelled with "the tool took longer than 30s" | Its own limit and its caller's end say each its own reason |
| EN-08 | The agents' cancels (`agent.Runtime`) | Two runs under one id: the first to end removed the second's cancel, so the second could not be cancelled any more | A run removes only its own cancel |

**Fails:** nothing new fails; each place logs as before, now only for its own reasons. **Cost:** nothing new: no call,
no wait in a turn; the stop of the service waits for its turns at most 10 s.
