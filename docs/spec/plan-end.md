# Ending a plan on the page – Spec

Status 2026-11-08: steps 1 and 2 built (not released). Scenarios: [plan-end.feature](plan-end.feature).

## Goal

A plan that nobody finishes stays open until its deadline: an hour without activity for a quiz, a vocabulary test, a
music quiz or a recipe ([Threads](threads.md), Plans). Seen in the games: a quiz broken off half-way stayed open, and
the next "lass uns ein Quiz spielen" asked whether to go on with it; the thread had to be deleted by hand in the
database. Saying "brich das Quiz ab" ends it too, but whoever has the page open sees the plan there and should be
able to end it with one click.

## Decided (2026-11-08)

| # | Question | Decision |
|---|---|---|
| 1 | Who may end a plan | Everyone who sees it: the Plans tab is for everyone, and the worst case is a lost quiz |
| 2 | Which threads | Only plans. The other threads (an appointment, a note, an alarm) are not shown on the page and end by themselves after 2 to 15 minutes |
| 3 | The button | "Beenden" / "End", with a question in the page's own dialog |

## Building blocks

- **The concept: ending a thread**, as `thread_cancel` in a conversation and the "Cancel" button of a task still being
  planned ([Tasks](tasks.md)): the same `CancelThread` of the threads' manager. It stops the thread's background work
  and its agents, withdraws its messages, stops the clips it played (the music quiz's song) and marks the thread
  cancelled. Nothing of ending is new; only the way to it is.
- **The Plans tab** ([Threads](threads.md), Plans, Display): it was read only; now each plan has the one button.

## What it does

- Each plan card on the Plans tab has, when opened, a button **"Beenden"**. It asks in the page's dialog (never a
  browser alert): "„<title>“ beenden? Mira vergisst den Stand." On yes the page calls
  `DELETE /v1/ui/plans/{id}`, then reloads the list.
- The service takes only an **open plan**: the id must be among the running plans (the same list the tab shows). Any
  other thread, open or not, is "no such plan" (404): the page offers no way to end threads it does not show.
- It then ends the thread with `CancelThread` and answers `{"ended": true}`, and logs `plan ended on the page` with
  the thread's id and target.
- **Cost:** one read of the open threads (the list the tab reads every 2 seconds anyway) and the writes of ending a
  thread; nothing that grows beyond the number of open threads.

### When a step fails

| Fails | Who learns of it |
|---|---|
| The plan is not running (ended meanwhile by voice, its deadline, or another page) | 404 "no such plan"; the page says "Dieser Plan ist schon beendet." and reloads the list |
| Reading the running plans fails | 500 with the error; the page shows it; the service logs `could not end a plan on the page` with the error |
| Reading the thread in `CancelThread` fails | as above (500, the page shows it, a log line) |
| Writing the thread's new state fails | `CancelThread` logs `could not end a thread` and does not pass it on (as for `thread_cancel` by voice); the page sees the plan again at its next refresh, 2 seconds later |
| No token or a wrong one | 401, as every call of the page |

## Between the integration and the service

In Home Assistant the page calls through the integration's proxy, which lets a user without admin rights make only
the calls named there. It now also lets them make `DELETE v1/ui/plans/<id>`; the service decides whether the id is a
running plan.

- A **newer service with an older integration**: admins end plans; for anyone else the old proxy refuses the call
  (401), and the page shows that error. They end the plan by voice as before.
- A **newer integration with an older service**: the service does not know the call (404 or 405), and the page shows
  the error. Nothing is ended.

## Steps

1. **The button and the call** (built 2026-11-08): `DELETE /v1/ui/plans/{id}`, the button with its question, the
   Plans tab's text in [Threads](threads.md) no longer "read only". PE-01 to PE-05.
2. **Through Home Assistant for everyone** (built 2026-11-08): the integration's proxy lets a user without admin
   rights end a plan. PE-06, tested in pytest.
