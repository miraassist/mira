# Calendars and to-do lists – spec

Status 2026-10-17: built in 0.6.19. Builds on [Skill actions](skill-aktionen.md). How data is presented (levels, timeline): [Context](kontext.md), section 2.

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

## Adding: skill `termin`

This is the main use case: "Trag mir Freitag um 15 Uhr Zahnarzt ein." It is a skill action without code of its own.

- **Fields:**
  - title;
  - start (date with time, or date only for all-day);
  - end or duration, optional (default 1 hour);
  - location, optional;
  - calendar.
- **Finish:** `calendar.create_event`.
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
