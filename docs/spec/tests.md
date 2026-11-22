# Tests without time – spec

Status 2026-11-22: decided; step 1 (the clock), step 2 (`retry`) and of step 3 threads, the notifier, ha/ws, ha/structure and mcp built. Scenarios:
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

1. **The clock** `internal/clock` with its tests (CK-01 to CK-08). Built when the scenarios run.
2. **`retry`** waits on the clock (`Policy.Clock`); RY-05 and the other retry tests without real time.
3. **The packages that wait**, one after the other, each with its fakes made controllable and its tests converted, a
   green `check.sh --release` before each commit: threads (InlineWait, AgentWait, the expiry ticker; built 2026-11-22:
   `Options.Clock` is a `clock.Clock`, the fake finisher holds on a gate, a turn whose wait runs out is driven with
   `Drive`; users of threads outside the package that pass no clock still wait in real time until their package is
   converted), notifier (`speakPause`, `speakingStart`, waits for states; built 2026-11-22: `Options.Clock` is a
   `clock.Clock`; the fake Home Assistant's `WaitState` runs out on the clock and reports a wait begun, a wait returned
   and each call; the fake trigger subscription reports what it subscribed), ha/ws (backoff, `WaitState`; built
   2026-11-22: `Options.Clock` carries the wait between attempts, `WaitState`'s limit and a template's answer;
   `fakeha.Server.Await(cond)` is closed once a condition on the fake holds, checked after every connection made or
   lost and every command; the deadlines of the socket itself, dialling, authenticating and `CallTimeout`, stay
   context deadlines in real time: they bound a network that hangs, and no test waits for them to run out),
   ha/structure (debounce; built 2026-11-22: `Options.Clock`; the debounce is a timer of the clock armed by the first
   change of a burst; the fake subscriber reports when every registry event is subscribed), mcp (retry ticker;
   built 2026-11-22: `Options.Clock`; the retry is a timer set anew after each round instead of a ticker, so the
   clock's waiters tell the test when a round is done; a call's `CallTimeout` and the wait for a stdio server's
   process to end stay real time, as bounds of a connection and a process),
   devices (Gather, EntryWait), agent (attempt timeout, run time), a2a (card retry), conversation (fakellm: gate instead
   of `Delay`/`PieceDelay`), matter, programs, setup, zigbee, llm (Retry-After), speakerid (timeout), hooks (file watch),
   waiting, clip, voicemail, heard, cmd/mira (a ready signal of `runAgent`, `fakeha.Await`).
4. **Absence after a positive signal** (rule 6): the "handled" traces the code lacks, each with its log line.
5. **Python:** `settle()` and `later()` wait on an `asyncio.Event` of the mocked call; `later()` moves the frozen clock.
6. **Against relapse:** `check.sh` refuses `time.Sleep`, `time.After` and polling loops in `_test.go` outside a short
   list of reasons (the clock's own tests).
