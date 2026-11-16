# Home Assistant through Mira's interface – Spec

Status 2026-11-16: steps 1 and 2 built (not released). Decisions 1 to 3 and 10 by the maintainer, the others taken
while writing, to be confirmed. Scenarios: [ha-api.feature](ha-api.feature) (`HE-…`).

## Goal

A service outside the house that holds a token of Mira's ([Access tokens](access-tokens.md)), such as the Partner
service, can call everything Mira's HTTP interface offers, but cannot reach Home Assistant itself. A skill whose state
lives in Home Assistant's helpers (a select, a date) needs that service to read and set them: "is the alarm on?",
"pause until Friday".

Mira passes such calls on to Home Assistant, with her own connection, and changes nothing about them.

## Decisions

By the maintainer (2026-11-16):

| # | Topic | Decision |
|---|---|---|
| 1 | What it reaches | Every entity of Home Assistant, not only a skill's helpers: a general route |
| 2 | Who may | Whoever holds a token of Mira's interface (the integration's or an access token); a partner's calls show on Mira's page as every partner call does ([Access tokens](access-tokens.md), decision 7) |
| 3 | Described | In Mira's OpenAPI file ([OpenAPI](openapi.md)) |
| 10 | A refused token (the open question of step 1: decided B) | Home Assistant refusing Mira's token on these routes raises a repair, not only a log line: the existing one, "Mira has no access any more" / „Mira hat keinen Zugang mehr“ ([HACS default](hacs-default.md), 4). It goes again with the next call Home Assistant accepts |

Taken while writing (2026-11-16), to be confirmed:

| # | Topic | Decision |
|---|---|---|
| 4 | Reading | One entity's state as Home Assistant gives it (`GET /api/states/<entity_id>` of Home Assistant's REST interface): `entity_id`, `state`, `attributes`, `last_changed`, `last_updated`. No list of all states: a caller asks for the entities it knows (push, not polling, holds for Mira; the caller's polling is its own matter) |
| 5 | Writing | By calling an action (a service), as Home Assistant itself does: `input_select.select_option`, `input_datetime.set_datetime`, `light.turn_on`. Not by writing a state into Home Assistant's state machine (`POST /api/states`), which changes no device and does not last for a helper |
| 6 | Nothing rebuilt | The request and the answer are Home Assistant's own, passed through: the action's data as Home Assistant takes it, its response (`return_response`) as it comes |
| 7 | No retries | Mira tries once; a write must not run twice behind the caller's back. The caller sees the error and decides |
| 8 | No hooks | Mira's hooks check the model's tool calls; these calls come from a token holder, not from the model. The token is the permission (decision 2) |
| 9 | Mira's own connection | Mira calls Home Assistant with her token (`ha.token`); what that token may not do, the route may not either |

## The routes

```
GET  /v1/ha/states/{entity_id}
POST /v1/ha/services/{domain}/{service}[?return_response]
```

`GET` answers `200` with Home Assistant's state object:

```json
{ "entity_id": "input_select.angehoerige_zustand", "state": "ok",
  "attributes": { "options": ["ok", "nachfrage", "hilfe", "alarm", "pause"], "friendly_name": "Zustand" },
  "last_changed": "2026-11-16T20:31:50+00:00", "last_updated": "2026-11-16T20:31:50+00:00" }
```

`POST` takes the action's data as Home Assistant does (the target inside it, `entity_id`, `area_id`, …):

```json
{ "entity_id": "input_datetime.angehoerige_pause_bis", "datetime": "2026-11-20 18:00:00" }
```

and answers `200` with `{}`, or with `{"response": …}` when `return_response` is given and the action returns one.

## What it does and costs

- `GET`: one request to Home Assistant's REST interface (`GET /api/states/<entity_id>`), with Mira's token. Nothing is
  kept.
- `POST`: one action call over Mira's WebSocket connection (`call_service`, with `return_response` when asked).
- No subscription, no wait beyond Home Assistant's answer; Home Assistant's own timeout of Mira's client holds.

## Errors

| Fails | Answer | Who learns of it |
|---|---|---|
| No or a wrong token | `401` | the caller |
| `entity_id` malformed (not `domain.object_id`, lower case letters, digits and `_`), `domain` or `service` empty, the data not a JSON object | `400` naming the value | the caller |
| Home Assistant has no such entity | `404` naming the entity | the caller |
| Home Assistant has no such action, or refuses the data (a refusal another try cannot change: `not_found`, `invalid_format`, `service_validation_error`, …, as [Retries](retry.md) classifies them) | `400` with Home Assistant's message, its code and the action's name | the caller; a log line with the action, the code and the message |
| Mira's token is refused by Home Assistant | `502` "Home Assistant refused Mira's token" | the caller; a log line; the repair "Mira has no access any more" (decision 10, below) |
| Home Assistant cannot be reached, a timeout, an error inside Home Assistant (`home_assistant_error`, `unknown_error`, `timeout`) | `502` naming what failed | the caller; a log line with the route, the entity or action, and the error |
| Mira's WebSocket connection is down at the moment of an action call | `502` "Home Assistant cannot be reached" at once, without waiting for the reconnection | the caller; a log line |
| Mira runs without Home Assistant | `503` "Home Assistant is not connected" | the caller |

Notes from building (2026-11-16):

- An error inside Home Assistant is a `502`, not a `400`: it is no fault of the request, and the caller may try again
  (decision 7 leaves that to it). The first text put every refusal of an action under `400`.
- An action call does not wait for Mira's connection to come back (her client would wait without a bound): the caller
  gets `502` at once and decides.
- A refused token: the first text named "the existing repair for the token". Step 1 raised none (only the log line);
  decision 10 adds it in step 2.
- `GET` reads Home Assistant's REST interface at the address and with the token of Mira's WebSocket connection
  (`ha.url`, `ha.token`); nothing new is configured.

Every `4xx`/`5xx` uses Mira's error body (`{"error": "…"}`) as every route does.

## The repair for a refused token (decision 10)

- **The service** keeps, in memory, whether Home Assistant refused Mira's token on the last call of these routes that
  Home Assistant answered (a `401`/`403` of its REST interface, `unauthorized` of an action call). `GET /v1/status`
  says so in a new field, `"ha_token_refused": true`; while the token is not refused the key is absent, so an older
  integration sees nothing new. The first refusal writes a log line (warning) saying the repair comes; each refused
  call keeps its own error line (above).
- **It goes** with the next call of these routes that Home Assistant answers without refusing the token: the state,
  no such entity, an action done, or an action refused for its data. A log line (info) says the repair goes. A call
  that does not reach Home Assistant (a malformed entity id, Home Assistant not reachable, Mira's connection down)
  says nothing about the token and changes nothing. A restart of the service (as after a new token is given through
  the repair) starts without it.
- **The integration** raises the existing repair "Mira has no access any more" / „Mira hat keinen Zugang mehr“
  (`new_access`, fixable, error) at its status poll (every minute) while the field is `true`, as for a token whose
  check fails ([HACS default](hacs-default.md), 4); it removes it when neither the field nor its own check of the
  token says so. Its log line names which of the two. An older service sends no field: nothing changes. No new text:
  the repair's text ("the token was revoked, or its user is no longer an administrator") fits.
- **Costs**: nothing beyond the flag; no call of its own, the integration reads the field from the poll it makes
  anyway.

| Fails | What happens | Who learns of it |
|---|---|---|
| The integration's poll fails | the repair stays as it was until the next poll | the integration's log, as for every poll |
| The token is refused again after the repair went | it comes again at the next poll | the user, by the repair; the log |

## Steps

1. **The two routes** with their errors and tests (HE-01 to HE-08), in the OpenAPI file (built 2026-11-16:
   `internal/channels/haconv/ha.go`, `ws.Client.State`).
2. **The repair for a refused token** (decision 10): `ha_token_refused` in `/v1/status`, the integration's existing
   repair (HE-09; built 2026-11-16, Go and pytest).
