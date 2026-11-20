# Home Assistant's intent answers where nothing succeeded – spec

Status 2026-11-20: decided (the maintainer), step 1 built (not released). Scenarios:
[intent-results.feature](intent-results.feature).

## Goal

A tool of Home Assistant's Assist API (the MCP server `ha`) that acted on nothing must reach the model as a failure,
so that Mira does not say it worked.

Seen on live (2026-11-20 19:13): "Lass die Musik hier weiter spielen" in the area Bad unten. The model called
`ha__media_player__HassMediaUnpause` with `name: "Dot 2"` and `area: "Bad unten"`; Home Assistant answered

```json
{"speech": {}, "response_type": "action_done",
 "data": {"success": [{"name": "Bad unten", "type": "area", "id": "bad_unten"}],
          "failed":  [{"name": "Dot 2", "type": "entity", "id": "media_player.dot_2"}]}}
```

Home Assistant's log had `music_assistant_models.errors.QueueEmpty: The queue is empty.` for the Dot 2 (its queue had
moved to the kitchen). The answer was no error, and Mira said "Musik läuft weiter".

## How Home Assistant comes to this answer

`ServiceIntentHandler.async_handle_states` in Home Assistant's `homeassistant/helpers/intent.py` (2026.11):

1. Before any call it puts the matched floors, else the matched areas, into `success_results`, as the place it acted
   in.
2. It calls the action for each matched entity; an entity whose call raised goes to `failed_results`, the others to
   `success_results` (type `entity`).
3. Only when `success_results` is empty does it raise an error ("Failed to call … for …"). With an area or a floor
   named, step 1 has filled it already, so a call where every entity failed still answers `action_done`.

Without an area or a floor, the same call raises, and the MCP server answers it as an error.

## Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | What Mira does | Marks such an answer as an error herself, as Home Assistant does without an area (B). Not chosen: a sentence for the model on how to read the answer (A); only reporting it to Home Assistant (C) |
| 2 | A partial success (some entities succeeded, some failed) | Stays a success; Home Assistant's answer goes through unchanged and the model sees `failed` itself (1A) |
| 3 | A spec first | Yes, this one (2A) |

## The rule

An answer of a tool of the MCP server `ha` counts as an error when all of these hold:

- it is no error already;
- its text is a JSON object with `"response_type": "action_done"`;
- `data.failed` has at least one entry;
- `data.success` has no entry of `"type": "entity"`.

Then Mira sets the result's error flag; the text stays Home Assistant's JSON, so the model reads which entities
failed. The types come from Home Assistant's fixed list (`IntentResponseTargetType`): only `floor` or `area` are put
into `success` before a call, everything acted on is an `entity`.

## What is not touched

- An answer with any entity in `success` (a partial success), or without `failed`.
- An answer that is no JSON object, or has another `response_type` (`query_answer`, `error`).
- The tools of every other MCP server, and Mira's own tools.

## Errors and traces

The rule itself cannot fail: what does not parse is left as it is. When it marks an answer, the log has one line:
`home assistant acted on nothing` with the tool and the failed entities.

## Costs

None: one parse of a text the call returned anyway; no call to Home Assistant.

## Steps

1. **The rule in the MCP host** (`internal/mcp`, for the server the app names as Home Assistant's). Scenarios IR-01
   to IR-05. Built.
