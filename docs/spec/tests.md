# Tests without time – spec

Status 2026-11-23: decided; steps 1 to 3 built. Scenarios:
[tests.feature](tests.feature). The inventory behind it (private): `docs/test-review.local.md`.

## Goal

A test's result does not depend on the speed of the machine it runs on. Seen at the release of 0.30.0: tests that
slept "a moment", polled with a fixed deadline or raced real timers failed on GitHub's runner (RM12, RM13, RM14) or
under the load of `check.sh --release` (FD-03, FD-13); about 300 places in the suite follow these patterns, and some
can pass while the code is wrong (an absence shown by waiting 150 ms, a polling helper that gives up silently).

## Rules (decided 2026-11-22, the maintainer)

| # | Rule |
|---|---|
| 1 | **No time in tests.** No `time.Sleep`, no polling with a deadline, no `time.After`/`context.WithTimeout` as a test's deadline, no measured durations. The only bound is `go test -timeout` (pytest's timeout), against hangs; it names the hanging test. |
| 2 | **What waits in the code waits on a clock.** Every timer, ticker, `After`, `AfterFunc` and wait of the code under test goes through `internal/clock`. In production the clock is the real one; in a test it is the fake clock, which the test moves on (`Advance`). No real wait in a test, however short. |
| 3 | **The test knows when to move the clock.** The fake clock says when the code waits on it (`AwaitWaiters`): the test advances only then, so the timer cannot be armed after the move. |
| 4 | **Fakes never wait.** A fake does not hold an answer by a delay or a sleep. It holds it until the test releases it (a gate) and reports what reached it (a signal the test awaits). |
| 5 | **A test waits for a signal**: one the code gives (a channel, a callback, a hook, a log line through a notifying log) or one of a fake the test controls. A polling helper is allowed only when what it reads is fully in the test's hands and needs no goroutine of the code to change. |
| 6 | **Absence is shown after a positive signal.** "Nothing more happens" is checked without waiting, once the code has said it handled the event (done, skipped, said); where the code says nothing, that is a gap of the code (a missing trace, also in production), not a reason to wait. |

## The clock (`internal/clock`)

```go
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
	AfterFunc(d time.Duration, f func()) Timer
	NewTicker(d time.Duration) Ticker
}

func Real() Clock                 // the time package
func NewFake(now time.Time) *Fake // a test's clock
```

- `Timer` (`C`, `Stop`, `Reset`) and `Ticker` (`C`, `Stop`, `Reset`) behave as the `time` package's: a channel of one,
  `Stop` reports whether it was pending.
- **The fake clock** stands still until the test moves it:
  - `Advance(d)` moves it by `d` and fires every timer and ticker that is due, in the order of their times; a ticker
    fires once for each period it passes. An `AfterFunc` runs inside `Advance`, so when `Advance` returns, it has run.
  - `Set(t)` moves it to `t`; a `t` before now panics (CK-12): the clock never goes back, and a test that moves it
    back is wrong (it hid such tests while `Set` ignored it).
  - `AdvanceToNext()` moves it to the earliest pending timer and fires it: for a test that drives code through its
    waits one by one, without knowing their lengths.
  - `Drive(fn)` runs `fn` and lets each wait of the code run out while `fn` runs (moving to the next timer whenever the
    code waits on the clock): for a test of a wait that ends unanswered. Where an answer is to come in time, the test
    gives it first and moves the clock after.
  - `Waiters()` is the number of pending timers and tickers; `AwaitWaiters(n)` is a channel closed once at least `n`
    are pending (at once when they already are).
- `WithTimeout(ctx, clock, d)` is `context.WithTimeout` on the clock (CK-13): the context ends when the clock passes
  `d`, with `context.DeadlineExceeded` as its error and its children's; on the real clock it is `context.WithTimeout`
  itself. For code whose time limit is a context deadline and whose tests drive that limit (the agents' run time,
  timeout and attempt timeout).
- A package that waits takes the clock as an option (`Clock clock.Clock`, nil for the real one), or for `retry` as a
  field of its `Policy`. Packages that keep a `Clock func() time.Time` today (threads, store, notifier) move to
  `clock.Clock` when their waits are converted.
- **Not used:** a library (`jonboulle/clockwork`, `benbjohnson/clock`): the clock is small, and its waiter signal must
  fit the signals of rule 5.

## Fakes and signals

- A fake that answers gets a gate (`Hold`/`Release` or a channel the test closes) instead of `Delay`.
- A fake that receives (calls, subscriptions, requests) gets `Await(match)`: a channel closed once something matching
  was received, at once when it already was (as `testenv/a2a_fake`'s `Await`).
- A test that needs a log line awaits it through a notifying log handler, not by polling the log's text.

## Steps

1. **The clock** `internal/clock` with its tests (CK-01 to CK-13). Built when the scenarios run.
2. **`retry`** waits on the clock (`Policy.Clock`); RY-05 and the other retry tests without real time.
3. **The packages that wait**, one after the other, each with its fakes made controllable and its tests converted, a
   green `check.sh --release` before each commit. Users of a package outside it that pass no clock still wait in real
   time until their own package is converted. Built:
   - **threads** (2026-11-22): `Options.Clock` is a `clock.Clock` (InlineWait, AgentWait, the expiry ticker); the fake
     finisher holds on a gate; a turn whose wait runs out is driven with `Drive`.
   - **notifier** (2026-11-22): `Options.Clock` (`speakPause`, `speakingStart`, the tickers); the fake Home Assistant's
     `WaitState` runs out on the clock and reports a wait begun, a wait returned and each call; the fake trigger
     subscription reports what it subscribed.
   - **ha/ws** (2026-11-22): `Options.Clock` carries the wait between attempts, `WaitState`'s limit and a template's
     answer; `fakeha.Server.Await(cond)` is closed once a condition on the fake holds, checked after every connection
     made or lost and every command. The deadlines of the socket itself (dialling, authenticating, `CallTimeout`) stay
     context deadlines in real time: they bound a network that hangs, and no test waits for them to run out.
   - **ha/structure** (2026-11-22): `Options.Clock`; the debounce is a timer of the clock armed by the first change of
     a burst; the fake subscriber reports when every registry event is subscribed.
   - **mcp** (2026-11-22): `Options.Clock`; the retry is a timer set anew after each round instead of a ticker, so the
     clock's waiters tell the test when a round is done. A call's `CallTimeout` and the wait for a stdio server's
     process to end stay real time, as bounds of a connection and a process.
   - **devices** (2026-11-23): `FoundOptions.Clock` carries Gather, EntryWait and `Await`'s limit; Run counts what it
     handled (a change taken with its gathering set, a gathering said) and closes its `changed` with each, so a test
     waits until Run handled every change the fake handed over, moves the clock past a gathering and waits until it
     was said; the fake creates an entry's device before it answers the submit, without a goroutine.
   - **agent** (2026-11-23): `Options.Clock` is a `clock.Clock`; the run time, a call's timeout and the attempt
     timeout are `clock.WithTimeout`, the retry waits on the clock; the scripted model holds its calls on a gate
     instead of a delay and reports each call that comes in; two debug lines say why a run waits, for a place at its
     model and for one of the places of the runs, and are the signals the tests of the limits await.
   - **a2a** (2026-11-23): `Options.Clock` is a `clock.Clock`; the reading of the cards again is a timer set anew after
     each round, as for MCP servers, and the attempts of a call wait on the clock; a test drives them with `Drive`
     while it waits for the delivery. The wait for the calls to stop on Close and a cancel's deadline stay real time.
     The fake remote agent's "stored" signal replaces polling for a completed task.
   - **conversation** (2026-11-23): the fake LLM holds an answer on `Hold` and hands a stream's pieces out through
     `PieceGate` instead of `Delay`/`PieceDelay`, and closes a step's `Taken` when a request takes it; a streaming test
     hands the pieces after the first out only once the first was handed on; a turn that
     waits for the turn before it in its session says so in a debug line, the signal of the test that turns of a
     session run one after the other; an agent called as a tool runs on the test's clock in its runtime.
   - **matter** (2026-11-23): `Options.Clock` carries the window, the pause between attempts and the wait for a
     commissioned device's registry event; Scan and Attempt bound calls to Home Assistant and stay real time; the fake
     Home Assistant reports each call; a test lets the window run out with `Drive`.
   - **programs** (2026-11-23): the wait before a failed download is tried again is on the manager's clock; a
     test learns from the clock's waiter that the download failed and moves it on; a download's own time limit
     bounds a connection and stays real time.
   - **setup** (2026-11-23): `Handler.Clock` is a `clock.Clock`; the restart after an answer waits on it, and a
     test moves the clock past it instead of sleeping.
   - **zigbee** (2026-11-23): `Options.Clock` carries the pairing time, Wait and Interview; a test lets the pairing
     time run out with `Drive` and shows that a device joining again is not said once the log line of its second
     join is there.
   - **llm** (2026-11-23): `Options.Clock` carries the waits between attempts; a test reads the wait the server named
     from the clock it moves (RY-30, RY-31) and gives the caller's deadline on the clock (`clock.WithTimeout`, RY-32).
     The test of a timeout in the middle of an answer waits for the HTTP client's real 150 ms: the timeout of a
     connection is its subject.
   - **speakerid** (2026-11-23): Match's Timeout is `clock.WithTimeout` on the client's clock; the fake service holds
     until the request is given up, and the test lets the timeout run out with `Drive`.
   - **hooks** (2026-11-23): Watch's next look is a timer of the engine's clock set anew once a look is done; the test
     moves the clock look by look.
   - **waiting** (2026-11-23): `Options.Clock` carries the wait for the satellite to be idle; the fake Home Assistant
     reports each call; a wait stopped before it showed says so in a debug line, the signal of the test that nothing
     shows after a stop. A replay is decided while the state change is handled, so a test checks it once the fake's
     push returned.
   - **clip** (2026-11-23): the fake satellite reports what it was told; a test awaits it, and the log line of a clip
     that played, instead of polling; a refused clip is checked at once, as nothing is handed on after the answer.
   - **voicemail** (2026-11-23): a playback ended with its run says so in a debug line, the signal of the tests that
     nothing more of it plays or is ticked; the fake box reports what it ticked and stopped.
   - **heard** (2026-11-23): `Options.Clock` is a `clock.Clock` and carries the watch's limit; the tracker closes a
     channel when a watch ends, for the tests; music playing since before the turn is shown by letting the limit run
     out on the clock.
   - **cmd/mira** (2026-11-23): the service logs to a writer a test gives (`prepared.logTo`, `telemetry.Options.LogTo`);
     a test starts it and awaits its line "mira is up", then the lines of what it handled ("presence handled",
     "scheduled task done"); the fakes are closed after the service stopped. The remote agents' tests run the threads
     and the calls on a fake clock and await the fake agent's signals (it also reports "pushed" once Mira answered a
     push), the finished thread and the told messages. A scheduled run is cleared before it is said to be done; the
     speaker test's timeout runs on the client's clock (`speakerid.Client.WithClock`).

   - **the others** (2026-11-23): startools (`Env.Clock` carries a call's Timeout and its looks at the memory; a call
     that takes too much memory now also ends the request it waits in), skills (`Runner.Clock` carries the wait for
     Home Assistant's prompt; a run of Mira's own schedule says it is done in the log, as a skill's does), cmd/testha
     (`waitLog` takes a clock), and fakes that report what reached them in ha/updates, inbox, music, backup, wifi,
     store, watch and channels/haconv. No Go test waits in real time any more.

4. **Absence after a positive signal** (rule 6): the "handled" traces the code lacks, each with its log line.
5. **Python:** `settle()` and `later()` wait on an `asyncio.Event` of the mocked call; `later()` moves the frozen clock.
6. **Against relapse:** `check.sh` refuses `time.Sleep`, `time.After` and polling loops in `_test.go` outside a short
   list of reasons (the clock's own tests).
