# Retries – Spec

Status 2026-11-11: steps 1 to 6 built, not released; step 7 run on the test Mira except a channel that is down. Scenarios: [retry.feature](retry.feature).

## Goal

A message to a person without a channel was sent again every minute for hours (2026-11-10, test Mira: 315 lines
"anna: no private channel configured"), although only a change of the settings could make it work. A search of the
code (2026-11-11) found ten places that try something again; only three of them ask first whether another attempt
can help at all, each built anew. This spec makes one rule for all of them and one small building block.

## Decided (2026-11-11)

| # | Question | Decision |
|---|---|---|
| 1 | The rule | Every retry classifies the error first: **retryable** (it may heal by itself: a connection lost, a timeout, a 429, a 5xx, a channel that is down) or **fatal** (it heals only by a change: no channel, an unknown person, a refused token, a wrong request). Only a retryable error is tried again; a fatal one ends at once with its trace (a log line, a failed state, the caller's error) |
| 2 | An error nobody classified | Counts as retryable: the bound protects |
| 3 | Bounded | Every retry has a bound: a number of attempts, and for a connection Mira lives on (Home Assistant, an MCP server) a longest wait between attempts instead: it is never given up, but never hammered |
| 4 | The wait fits the error and the other side | Exponential for something that may be down a while (a server, a channel); linear or fixed and short for something local that restarts (the service, a page); none for a conflict in Mira's own database. Neither the client nor the server is overloaded |
| 5 | A wait the server names | A `Retry-After` (seconds or a date) or Google's `RetryInfo.retryDelay` in the body is kept: never earlier. When the named wait is longer than the caller can wait (the rest of its deadline, or the policy's longest wait), the call fails at once with an error that names the wait: a voice turn does not sit out 30 s |
| 6 | Where it lives | `internal/retry`: the classification (an error says whether it is retryable; `retry.Fatal(err)` and `retry.After(err, d)` mark it), the policy (attempts, first wait, growth exponential, linear or fixed, longest wait), `Do` for attempts within one call and `Next` for attempts kept in the database across ticks and restarts. No jitter: Mira is one client per household |
| 7 | The trace | Each further attempt writes a warning where it happens: what failed, attempt n of N, the wait ([Silent fallbacks](fallbacks.md), decision 4). Giving up writes an error with the number of attempts; a fatal error writes one line, not one per tick |
| 8 | The rule for new code | In CLAUDE.md, Specs first: a spec with a retry names which errors are retryable, which fatal, the bound and the wait, and what happens when it gives up |

## The places

| # | Place | Retryable | Fatal | Bound and wait | Today |
|---|---|---|---|---|---|
| 1 | Private delivery of a message (notifier: Tick, a presence, the escalation; `sendPrivate`) | Home Assistant not connected, the call timed out, `home_assistant_error`, `unknown_error`, `timeout`, anything else | no person, the person unknown, no channel; Home Assistant's `not_found`, `invalid_format`, `service_validation_error`, `unauthorized`, `not_allowed` | 10 attempts, exponential from 1 min (1, 2, 4 … 256 min, about 8.5 h together), kept in the message (`attempts`, `retry_at`); the message's expiry still ends it earlier | every minute until it expires, an error line each time, no classification |
| 2 | Reminders (`remindOne`) | as row 1 | as row 1 | 5 attempts, linear from 15 s (15, 30, 45, 60 s): a reminder is for now | 5 attempts on every 15 s tick, no classification |
| 3 | The model client (`internal/llm`) | 429, 5xx, a connection failure | another 4xx; a deadline that fired while the model generated; part of a streamed answer already passed on | 3 attempts; a 429 or 503 with a named wait waits that long (decision 5), at most 30 s and never past the caller's deadline; a 429 without one 1 s, then 2 s (exponential); a 5xx or a failed connection 250 ms, then 500 ms (linear) | the same classification; linear 250 ms also for a 429; a named wait ignored |
| 4 | An agent's model call (`internal/agent`, `Limits.Retries`) | only its own attempt timeout firing (the model hung) | every error of the client (it has classified and retried already); then the model's fallback, if configured, as before | `Retries` of the agent (mail: 1), no wait (the hang already took its time) | every error tried again, also a 4xx, on top of the client's retries |
| 5 | The connection to Home Assistant (`internal/ha/ws`) | a lost or refused connection | the token refused (as today) | never given up; exponential from `ReconnectMin` to `ReconnectMax` | the same; moved onto the policy |
| 6 | MCP servers (`internal/mcp`, Run) | a connection failure, a 429, a 5xx | 401 or 403 (the token refused), a skill not connected (`ErrNoConnection`): not tried again until the server's connection or config changes (`Reconnect`, a config change) | never given up; exponential from 30 s to 30 min; a warning at the first failure and when the error changes, info when connected again | every 30 s forever, also with a refused token; only a debug line |
| 7 | Conflicts of a thread change (`internal/threads`) | `store.ErrConflict` | everything else | 5 attempts, no wait (Mira's own database) | the same: no change |
| 8 | A task's draft refused by Home Assistant (`internal/tasks`) | – | – | a correction with the reason, once; not a retry | no change |
| 9 | The integration's proxy to Mira's page (`panel.py`) | a connection never made | everything else | 4 attempts, fixed 1 s (the service restarts) | the same: no change |
| 10 | Mira's page (`index.html`, `api()`) | the service unreachable | everything else | 10 attempts, fixed 3 s | the same: no change |
| 11 | Setup waits for the new service (`config_flow.py`, `_wait_ready`) | a connection failure | a refused token (`MiraAuthError`), another answer of the service | 60 attempts, fixed 1 s | every `MiraError` tried again, also a refused token |
| 12 | Joining a device's Wi-Fi through the Supervisor (`internal/wifi`) | the Supervisor's answer failed | – | polling within its 20 s, as before | the failed answer is dropped without a word: now a warning at the first one |

Not retries, and left as they are: the model fallback (another model, [Models](models.md)); periodic jobs (the assist
prompt, purging sessions, mail cleanup): their next run is their next turn; a scheduled skill job that failed: its next
schedule.

### Cost

Row 1 makes at most 10 calls of Home Assistant's notify action per message instead of one per minute; a fatal error
makes one. Row 6 makes, for a server that is down for a day, about 50 connection attempts instead of 2880. The others
make at most as many calls as today.

### Fails

| Fails | Who learns of it |
|---|---|
| A private delivery fails with a fatal error | Log error `private delivery failed` with the error and `fatal=true`, once; the message is `failed` ("nicht zustellbar" on the Messages tab) |
| A private delivery fails with a retryable error | Log warning `private delivery failed, trying again` with the attempt, the bound and the time of the next; at the last: error `private delivery given up` with the attempts, the message `failed` |
| A message to the house reaches some persons, not others | Delivered, as today; the failed persons in the log line `private delivery partly failed` |
| An MCP server refuses the token | Log error `mcp server refused the token; not tried again until its connection changes`; the page shows the server's error as today |
| A named wait is longer than the caller can wait | The caller's error: `model busy, asked to wait 30s` (the model, the host, the wait); a voice turn says it cannot answer now, as for every model error |

## Steps

1. **The building block:** `internal/retry` (classification, policy, `Do`, `DoBy`, `Next`, `Join`), the rule in
   CLAUDE.md. RY-01 to RY-06 (built 2026-11-11).
2. **Private delivery** (row 1): the classification of Home Assistant's errors (`ws.Error.Retryable`), `retry_at` on a
   message (migration 0026), give up after 10; the urgent path waits for the next attempt too. RY-10 to RY-14 (built
   2026-11-11).
3. **Reminders** (row 2); a reminder tried again is no longer prefixed "Erinnerung:" a second time. RY-20, RY-21
   (built 2026-11-11).
4. **Model calls** (rows 3 and 4): the named wait, the 429's exponential wait, an agent retrying only a hang. RY-30 to
   RY-34 (built 2026-11-11).
5. **Home Assistant and MCP** (rows 5 and 6); Home Assistant's waits start anew after a connection that worked (they
   never did). A 401 or 403 reaches Mira through the MCP SDK as the transport's own error (`TokenRefused`). RY-40 to
   RY-43 (built 2026-11-11).
6. **Setup and the Wi-Fi wait** (rows 11 and 12; pytest and Go): the integration's `MiraError.retryable`. RY-50 to
   RY-52 (built 2026-11-11).
7. **Run through on the test Mira** (2026-11-11): an urgent confidential message to a person without a channel: one
   line `private delivery failed … fatal=true`, the message `failed` after 1 attempt, nothing after; an MCP server with
   a wrong token (X's): one line `mcp server refused the token`, no further attempt. A channel that is down is tested
   in Go only (RY-11, RY-12): the test HA has no notify action that fails with an error that heals.
