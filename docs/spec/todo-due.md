# To-dos with a due date – spec

Status 2026-11-15: decided (decisions 1 to 10); steps 2 to 5 built (not released), step 1 dropped; step 3 with the inbox's step 10. Builds on [Calendars and to-do lists](calendar.md) (the to-do lists, the
skill `todo`, the calendar's one building block as the model), [Mira's inbox](inbox.md) (section 8, kinds by meaning)
and the skill `briefing`. Terms: [Glossary](../glossary.md), Calendars and lists. Scenarios: [todo-due.feature](todo-due.feature).

## Goal

Mails and documents name as many things to *do* by a day as things that *take place*: pay the invoice by the 30th,
hand in the form by Friday, call Frau X back. Today the inbox enters only appointments; a deadline becomes a whole-day
appointment when the person asks for it, a payment or a callback Mira only names. A whole-day appointment slips into
the past quietly, paid or not, and a calendar knows no "done".

So the line is drawn by what the thing is:

- an **appointment** takes place at a time, often at a place, and is over when the time has passed: it goes into a
  calendar, as today;
- a **to-do** is something to do and is gone only when ticked: a deadline, a payment, a callback go onto the person's
  own **to-do list**, with the day as the **due date**.

Home Assistant's to-do lists hold a due date and a description and show a to-do as overdue. What they do not do is
remind: so the personal briefing names the to-dos that are due today or overdue.

## Concepts it hangs on

- **The inbox's kinds by meaning** ([Mira's inbox](inbox.md), section 8; [Messages](messages.md), decision 33): a
  thing in a mail is an appointment or a to-do; a to-do becomes an entry of kind `todo` with a thread of the skill
  `todo`, its fields filled from the reading.
- **The calendar's one building block** ([Calendar](calendar.md), Adding): `calendar.create` checks and enters. The
  to-do gets the same, `todo.create`, and the skill `todo` finishes through it, as the skill `termin` does through
  `calendar.create`. Departure: no tool beside it (the calendar has `calendar_create`): the inbox's to-dos go through
  the skill's own thread. The list's rule follows the calendar's.
- **Personal calendars by convention** ([Calendar](calendar.md), Personal calendars): the same convention names a
  person's own to-do list.
- **Reading the calendar with an own tool** ([Calendar](calendar.md), Reading): `calendar_events` instead of HA's raw
  tool. Reading the due to-dos gets an own tool, `todo_due`, for the reason below.
- **Skill schedules with `per_person`** ([Schedules](schedules.md)) and their placeholders (`{{person_calendar}}`):
  the personal briefing gets `{{person_list}}` beside it.

## Decided (2026-11-15)

| # | Question | Decision |
|---|---|---|
| 1 | Which list | The person's own list: the exposed list named like the person or their login, as personal calendars; else the default in the person's memory; else Mira asks and offers to remember |
| 2 | What becomes a to-do | A deadline, a payment, a callback: everything the person must do themselves, with its day as the due date when there is one. An appointment stays an appointment |
| 3 | Offered by itself | Yes, as appointments: each to-do the inbox finds is offered with "Soll ich das auf deine Liste setzen?" (inbox, section 8). Replaces the inbox's decision of 2026-11-12 that a deadline is entered only on request, as a whole-day appointment |
| 4 | Which briefing names due to-dos | The personal morning briefing, from the person's own list, and the briefing on request for whoever speaks. Not the house's briefing, not the evening one |
| 5 | A reminder beside the briefing | None; the briefing is the reminder |
| 6 | Reading | An own tool `todo_due` that filters by due date itself: HA's `todo__get_items` filters only by status (`needs_action`, `completed`, all) and returns every item raw, so the model would sort dates and the result would grow with the list (checked in HA's source, `components/todo/llm.py`, 2026-11-15) |
| 7 | Background runs and HA's to-do tool | Nothing to do (corrected 2026-11-15 before building): HA serves its tool as `todo__get_items` since 2026.10, and the default `security.read_only_tools` names only the old `ha__todo_get_items`, but HA's MCP server marks the tool `readOnlyHint: true` (its `tools/list` on the test HA, 2026-11-15), and Mira takes an annotated tool as read-only (`internal/mcp/host.go`). The name list is only for servers that do not annotate |
| 8 | A list that cannot hold a due date (HA's shopping list: `supported_features` without `SET_DUE_DATE_ON_ITEM`, 16; on the test HA `todo.einkaufsliste`, 15) | When there is a due date, the rule takes only lists that can hold it; a named list that cannot is refused with the lists that can. The same for the description (`SET_DESCRIPTION_ON_ITEM`, 64) |
| 9 | Overdue: how far back | The last 30 days: an open to-do due more than 30 days ago is not named; within them the tool says "fällig seit" and cuts at 20 with "und N weitere" |
| 10 | A message of its own for to-dos | Changed the same day: a to-do *to be entered* is an entry of the new kind `todo`, with a thread of the skill `todo` ([Messages](messages.md), decision 33; inbox, decision 29). A to-do *on the list* is no message: due ones are named by the briefing, whose result is a message as today (its kind by importance and confidentiality) |

## What it does

### The building block `todo.create`

**The building block** takes:

| Field | Meaning |
|---|---|
| `item` | required, at most 80 characters: "Rechnung Stadtwerke bezahlen" |
| `due` | optional, `YYYY-MM-DD`; a day, not a time (a deadline is a day) |
| `description` | optional, at most 1,000 characters: the amount, the number to call, and where it comes from ("aus Paperless 5444: Rechnung der Stadtwerke") |
| `list` | a list's name or entity, `standard`, or empty |
| the person | from the turn or the thread, not a field the model fills |

Step by step:

1. **The fields:** `item` not empty and short enough; `due` a valid date (a past one is taken; the answer says it is
   past); `description` short enough.
2. **The list**, one rule:
   1. a list named (`list`): it must be exposed and not another person's own list;
   2. else the person's own list by convention (below);
   3. else the default from the person's memory, which the model passes as `list`, as for the calendar;
   4. else the only exposed list that is no other person's;
   5. else none: the answer names the lists the person may use and asks which; after the answer the skill's prose
      offers to remember it as the default.
   When `due` (or `description`) is set, only lists that can hold it count (decision 8).
3. **Entering:** Home Assistant's action `todo.add_item` with the entity, `item`, `due_date`, `description` (empty
   fields left out); 1 call.
4. **The answer:** what is on which list, with the due date in words, or why not.

**A person's own list:** an exposed to-do list named like the person or like the login name of their HA user (case
and leading/trailing spaces do not matter), as personal calendars. No setting.

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| A field does not fit | nothing is entered | the answer names the field and its value; a log line with the caller |
| List unknown, not exposed, another person's, or unable to hold the due date or the description (decision 8) | nothing is entered | the answer names the lists the person may use; a log line |
| The lists cannot be read (HA's states) | nothing is entered, not retried | the answer says so; a log line with the error |
| Home Assistant refuses `todo.add_item` or is not reachable | nothing is entered, not retried in the turn | the answer says so; a log line with the list, the item and the error |

**Who uses it, and what it costs:**

- **The skill `todo`**: its finish names the building block (`action: todo.create`) instead of the action
  `todo.add_item`; its check `todo_list` gives way to the building block's rule. Its prose about the default list
  stays. Cost at the finish: 1 read of the to-do lists' states (filtered to the domain `todo`), 1 call to Home
  Assistant; the turn waits for both. This holds for a thread the user starts ("Setz Batterien auf die Liste, bis
  Freitag") and for one the inbox started.

### The inbox: deadlines, payments and callbacks become to-dos

How a to-do comes from a mail or document is in [Mira's inbox](inbox.md), section 8 (kinds by meaning): the reading
writes a `todo` item, the code starts a thread of the skill `todo` with `item`, `due` and `description` filled, its
entry goes into the person's message of kind `todo`, and the yes runs the skill's finish, the building block above.
For it, the skill `todo` gets:

- a section `## Ansage` for the kind `todo` ([Messages](messages.md), decision 30): say what is to be done by when and
  from what source, and offer "Soll ich das auf deine Liste setzen?"; several: name them and offer to put them all on
  the list;
- in its prose, for a thread started from a message: the fields are there; on the yes fill the list (named, the
  memory's default, else "standard") and confirm; a change goes through `thread_fill` first.

### Reading: the tool `todo_due`

| Parameter | Meaning |
|---|---|
| `list` | a list's name or entity; required |
| `until` | the last due date to include, `YYYY-MM-DD`; default today |

Step by step:

1. Read the to-do lists' states (domain `todo`) and keep the exposed ones; find `list` among them. 1 call.
2. Home Assistant's action `todo.get_items` with the entity and `status: needs_action`, with the response. 1 call.
3. Keep the to-dos with a due date from 30 days before today up to `until` (decision 9). A due date with a time
   (`due` as date and time) counts by its day.
4. Present, compact, sorted by due date:

```
Überfällig
- Rechnung Stadtwerke bezahlen · fällig seit Donnerstag 12.11. (Anna)
Heute
- Anmeldung Schwimmkurs abgeben (Anna)
```

- Grouped into "Überfällig" and the days up to `until`; the description is not shown (it may be long; asked for, the
  model reads the list with HA's `todo__get_items`).
- At most 20, then "und N weitere".
- Nothing due: one line saying so, for the model; the briefing's prose leaves the part out.

**What it costs:** 2 calls to Home Assistant per call of the tool; the answer grows with the due to-dos, at most 20
lines. Read-only: offered to background runs (`ReadOnly: true`). It belongs to the skill `briefing`.

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| The list is unknown or not exposed | no to-dos | the answer names the exposed lists; a log line |
| HA's states or `todo.get_items` fail | no to-dos, not retried | the answer says the list could not be read; a log line with the list and the error |
| An item's `due` does not parse | the item is left out | a log line with the list, the item's summary and the value |

### The briefing

- **Personal morning briefing** (`persoenlich`): a fourth part, "die To-dos, die heute fällig oder überfällig sind,
  aus der Liste {{person_list}} („-“ heißt: keine, dann keine To-dos), mit `todo_due`". In `spoken` one short sentence
  ("Die Rechnung der Stadtwerke ist seit Mittwoch überfällig."), in `details` the list. Without due to-dos the part does
  not come up; the rule "weder Termine noch Pakete noch eigene Themen → importance 0" gets "noch fällige To-dos".
- **`{{person_list}}`**: the person's own list by the convention above, "-" without one; replaced as
  `{{person_calendar}}` (`internal/skills/runner.go`).
- **On request** ("Was steht heute an?"): a fifth point in the prose: knowing who speaks, `todo_due` on their own list;
  not for others' lists, not without a known speaker.
- **Errors:** `todo_due` fails → the briefing leaves the part out (its prose: what the tools do not give is not
  mentioned); the tool's log line is the trace.

## Deliberately not

- **A reminder on the due day** (decision 5).
- **Changing or ticking to-dos with own tools**: HA's `HassListCompleteItem` and `HassListRemoveItem` do it.
- **A time of day on the due date**: a deadline is a day.
- **The house's lists in the briefing** (decision 4).
- **A setting for the person's list**: the convention and the memory.

## Steps

1. Dropped (decision 7): background runs get HA's to-do tool by its annotation already. TD-01 dropped.
2. **The building block `todo.create`**, the list's rule, the person's own list; the skill `todo` finishes through
   it. TD-02 to TD-08, TD-18.
3. **The inbox's to-dos:** built with [Mira's inbox](inbox.md), step 10 (the skill `todo`'s `## Ansage` and its prose
   for a thread started from a message); its scenarios are there. TD-09 and TD-10 (a tool `todo_create` for the skill
   `postfach`) dropped with decision 10's change.
4. **The tool `todo_due` and the briefing:** the tool, `{{person_list}}`, the briefing's prose (scheduled and on
   request); an eval of the personal briefing with an overdue to-do. TD-11 to TD-17.
5. **The guide** (de/en): Calendars and lists, the inbox, the briefing; the test HA's seed gets a list "Anna" with due
   to-dos (`cmd/testha`).
