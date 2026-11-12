# Calendars and to-do lists – spec

Status 2026-11-12: built in 0.6.19; steps 1 and 2 (one building block and the tool `calendar_create`) built 2026-11-12, not released. Builds on [Skill actions](skill-actions.md). How data is presented (levels, timeline): [Context](context.md), section 2.

## Principle

Calendars and to-do lists are Home Assistant concepts; Mira uses them instead of keeping her own. Several calendars are the normal case (family, own, Google, holidays, waste collection …).

- **What Mira sees** is governed by HA's exposure to Assist, as for all entities: only exposed calendars and lists.
- A calendar is **writable** if HA says so (`supported_features`: create, delete). Holidays or calendar weeks are read-only; Mira does not offer them for adding events.

## Reading: own tool `calendar_events`

HA's tool `calendar_get_events` returns all events of a period raw, with long descriptions (invitations, links). Mira does not use it. She has her own tool and decides herself how the model sees the calendar.

| Parameter | Meaning |
|---|---|
| `from`, `to` | period; default: today until 7 days from now |
| `calendar` | one calendar; default: all exposed |
| `query` | search text in the title, e.g. "Arzt" |
| `details` | also descriptions; default no |

Presentation, compact:

```
Mo 20.10.
- [t1] 10:00–10:30 Arzt Dr. Berger · Praxis Dr. Berger, Hauptstraße 12, … (Familie)
Fr 24.10.
- [t2] ganztägig Geburtstag Oma (Familie)
```

- Grouped by day, with weekday; all-day events as "ganztägig".
- The location is shortened. The description only comes with `details`, and even then shortened and without links.
- At most 40 events, then "und N weitere".
- Every event has a short ID (`t1`, `t2` …) for cancelling. HA's event ID (`uid`, for series `recurrence_id`) the code keeps to itself; the model does not see it.
- The source is HA's REST API `/api/calendars/<entity>`. It returns the event ID, Mira's user may read it.

**For tasks** the same presentation is added as view `view: upcoming` for calendar entities in `mira.assess` and `mira.decide` (default: the next 24 hours), e.g. for "Sag mir abends, was morgen ansteht".

## Adding: one building block, one tool (decided 2026-11-12)

**Why.** Until now three ways entered an event, each with its own checks: the skill `termin` (the action
`calendar.create_event`), the inbox (`inbox_enter`, through the mail calendar's WebSocket command) and the mail calendar
of parcels (the WebSocket command `calendar/event/create`). They do the same. Now there is one building block,
**`calendar.create`**, and one tool on it, **`calendar_create`**. What differs between them is the thread around it and
the skill's prose, not the entering.

Home Assistant offers no tool for it: its MCP server has `calendar__get_events` and the general
`homeassistant__call_action` (checked on the test HA, 2026-11-12). With `call_action` the model would have to find the
entity, check the calendar and the dates itself, and the mail calendar's code could not use it. The building block
calls Home Assistant's action `calendar.create_event`; it adds only the checks before it.

**The building block `calendar.create`** takes:

| Field | Meaning |
|---|---|
| `title` | required, at most 80 characters |
| `start`, `end` | `YYYY-MM-DD HH:MM`; without `end` one hour after `start` |
| `day`, `day_after` | for a whole day: the day, and the day after the last one (without it the next day) |
| `location` | optional, at most 100 characters |
| `description` | optional, at most 1,000 characters (the mail calendar keeps its details and marks there): where the event comes from ("aus Paperless: Elternbrief der Grundschule (5444)") |
| `calendar` | a calendar's name or entity, `standard`, or empty |
| the person | from the turn or the thread, not a field the model fills; none for Mira's own code (the mail calendar), which may then name any writable calendar |

Step by step:

1. **The dates:** either `start` (and `end`) or `day` (and `day_after`), never both, never neither; valid dates; `end`
   after `start`, `day_after` after `day`. A past date is taken (the caller decides; the tool's answer says it is
   past).
2. **The calendar**, one rule for every way:
   1. a calendar named (`calendar`): it must be writable and not another person's (Personal calendars below); the
      house's calendars and the person's own are allowed;
   2. else the person's own calendar by convention;
   3. else the only writable calendar that is no other person's;
   4. else none: the answer lists the writable calendars the person may use and asks which.

   The default the person's memory names ("Mein Kalender ist Familie") is passed by the model as `calendar`, as the skill
   `termin` does today.
3. **Entering:** `calendar.create_event` with the entity, the title, the dates, the location, the description; 1 call
   to Home Assistant.
4. **The answer:** what was entered, in words (title, day, time, calendar), or why not.

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| Dates or title do not fit | nothing is entered | the answer names the field and its value; a log line with the caller |
| Calendar unknown, not writable, another person's | nothing is entered | the answer names the calendars the person may use; a log line |
| The list of calendars cannot be read | nothing is entered, not retried | the answer says so; a log line with the error |
| Home Assistant refuses or is not reachable | nothing is entered, not retried in the turn | the answer says so; a log line with the calendar, the title and the error |

**Who uses it:**

- **The tool `calendar_create`** (model): the fields above as parameters. It belongs to the skills that name it
  (`postfach` now), not to every turn: without a skill the model enters through the skill `termin`
  ([Threads](threads.md), Tools of a turn in a thread: a tool may belong to several skills). Cost in a turn: 1 read of
  the calendars (as `calendar_writable` today), 1 call to Home Assistant; the turn waits for both.
- **The skill `termin`**: its finish names the building block (`action: calendar.create`) instead of the action
  `calendar.create_event`; its check `calendar_writable` and its `undo` stay.
- **The mail calendar** ([Mail calendar](mail-calendar.md)): its code enters a new parcel through the building block
  instead of its own WebSocket command; updating and deleting by the event's id stay its own. The flow of parcels does not
  change.

## Adding: skill `termin`

This is the main use case: "Trag mir Freitag um 15 Uhr Zahnarzt ein." It is a skill action without code of its own.

- **Fields:**
  - title;
  - start (date with time, or date only for all-day);
  - end or duration, optional (default 1 hour);
  - location, optional;
  - calendar.
- **Finish:** the building block `calendar.create` (above; until 2026-11-12 the action `calendar.create_event`).
- **No question back** whether it fits. Mira says what she added: title, day, time, calendar.
- **Correction** ("Nein, 16 Uhr"): Mira takes the event back with `thread_undo` and adds it again, as the alarm does; no thread `termin-absagen`, no question back (the user just asked for the event). The skill's `undo` is the core building block `calendar.delete_created` (2026-10-23).
  - HA returns no event ID when adding. The building block finds the event again in the same calendar by its title and start (`entity_id`, `summary`, `start_date_time` or `start_date`) and deletes it with `calendar/event/delete`, as `calendar.delete` does; deleting works with calendars that cannot change events (Google), unlike `calendar/event/update`.
  - Exactly one match is deleted. With none or several it deletes nothing and says so; the model then tells the user.
  - As every `undo`, within 15 minutes of adding.

## Personal calendars: convention instead of setting

An exposed calendar that is named like the **login name** of a person's HA user or like the **person** themselves (case and leading/trailing spaces do not matter) is that person's calendar: "Stefan" belongs to the person "Stefan Rinke" with the login `stefan`, "Ben" belongs to Ben. A first name alone does not count (HA has no field for it). Only an admin may read the login name; the integration therefore sends it with the settings (`logins`, person → login), the agent keeps it (`ha_logins`). There is no setting for this. It is used by skill schedules with `per_person: true` (they run per person who receives a personal message, and get that person's calendar, if there is one, and their personal profile; `cmd/mira/calendars.go`, `reachablePeople`) and by the briefing on request.

## Which calendar: ask and remember

There is no setting for this.
- If the person's memory holds a default ("Mein Kalender ist Familie"), Mira takes it.
- If there is only one writable calendar, she takes that one.
- Otherwise she asks: "In welchen Kalender – Familie oder Anna?" After the answer she offers to remember it as the default.
- If the user names a calendar explicitly ("in den Familienkalender"), that one applies.

## Cancelling: skill `termin-absagen`

"Sag meinen Termin morgen beim Arzt ab."

1. Mira searches with `calendar_events` (period and search text from the sentence).
2. If there is exactly one match, it is cancelled. With several, Mira asks which one; with none, she says so.
3. **Question back before deleting** ("Arzt Dr. Berger, Montag 10 Uhr – absagen?"): deleting cannot be undone (`confirm: true`, decided).
4. The finish is a building block in the core, `calendar.delete`. HA has no action for this, only the WebSocket command `calendar/event/delete` with event ID. Mira's user may call it (checked).
5. **For series** only this one event is cancelled, unless the user explicitly says "alle".

## To-do lists

- **Reading, adding, checking off, removing** via HA's Assist tools (`todo__get_items`, `HassListAddItem`, `HassListCompleteItem`, `HassListRemoveItem`). These already exist.
- **New is the skill `todo`** for items with **due date or description** ("Setz Batterien auf die Liste, bis Freitag"). HA's Assist tools cannot do that.
  - Fields: item, list, due (optional), description (optional).
  - Finish: `todo.add_item`.
  - Without due date and description, HA's `HassListAddItem` is enough.
- **Which list:** as with the calendar. If there is only one, Mira takes it; otherwise the default is in memory, or she asks.

## Waste skill

The skill `abfall` will read its dates via `calendar_events` from a calendar in HA (e.g. collection dates) instead of fetching an iCal file itself. Mira's own calendar logic (`internal/ical`) is dropped. Instead of `calendar_url` the skill names the calendar entity.

## Test environment

The test HA has a local calendar "Familie" (`calendar.familie`, writable) and a local list "Einkauf" (`todo.einkauf`, with due date and description). Both are exposed to Assist and have sample entries, among them an event with a long description and a link. When building, this goes into the seed program (`cmd/testha`) so that it can be restored.

## Deliberately not

- **HA's `calendar_get_events`**: too much and too raw.
- **Changing events**: for now cancel and add again.
- **A setting for the default calendar or list**: that is in memory.

## Steps

Before 2026-11-12 this spec had no steps; what it describes above was built in 0.6.19. Scenarios:
[calendar.feature](calendar.feature).

1. **The building block `calendar.create`** and its checks; the skill `termin` finishes with it; the mail calendar
   enters a new parcel with it. CA-01 to CA-07.
2. **The tool `calendar_create`**, and a tool of Mira's belonging to several skills ([Threads](threads.md)). CA-08 to
   CA-09.
