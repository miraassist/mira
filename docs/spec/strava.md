# Strava – spec

Status 2026-11-18: steps 1 and 2 built and run (the skill file and its tests; connected on the test HA and run end to
end on the empty account), not released; step 3 (the morning line) later. Scenarios: [strava.feature](strava.feature).

## Goal

[Strava](https://www.strava.com) records rides, runs and hikes. A cyclist asks Mira what Strava knows about their
sport: "Wie war meine letzte Tour?", "Wie viele Kilometer bin ich diese Woche gefahren?", "Wie viel hat mein
Rennrad schon drauf?". Mira reads it from Strava and answers in a few spoken sentences.

The concept it hangs on: a **skill with a connection** ([Connections (OAuth)](oauth.md)) and its **web API tool**
(`<skill>__api_get`, read only). No MCP server, no code of Mira's own for Strava: the skill is a SKILL.md with an
`oauth` and an `api` section and the instructions.

## Decided (2026-11-18, the maintainer)

1. **Strava's normal login and web API, through Mira's connections** (1A). Strava's official MCP server
   (`https://mcp.strava.com/mcp`) was tried first: its client registration
   (`https://www.strava.com/oauth/mcp/register_client`) refuses Mira with
   `{"error":"invalid_client_metadata","error_description":"Unable to register client"}` (HTTP 400, 2026-11-18, for
   every metadata tried, also `{}`); Strava names Claude and Gemini as the assistants it connects. The community MCP
   servers (r-huijts/strava-mcp, ljcl/strava-mcp, tomekkorbak/strava-mcp-server) each keep and renew the Strava
   token themselves and none takes the token of Mira's connection; ljcl has no list of activities and no gear.
2. **This route goes against Strava's API Policy** (effective 2026-06-01,
   [api_policy](https://www.strava.com/legal/api_policy)), knowingly, decided by the maintainer: §5.3 forbids using
   Strava data "in connection with the … operation of any AI Application", which names "ingestion into a context
   window"; the only exception is the Strava MCP (§3.5), which Mira cannot use (decision 1). §5.16 (no own MCP
   server or proxy) is not touched: there is none. The guide and the skill's description do not hide it: the user
   connects their own account with their own Strava API app, for their own use.
3. **No Strava data into memory** (§5.5, no "Persistent Index"): the skill tells Mira never to save activities,
   distances or times with `memory_save`. What Mira reads lives in the turn and the conversation's history only.
4. **The skill is public**, `skills/strava`, in German (2B).
5. **Read only** (`api`: `GET` below `https://www.strava.com/api/v3/`): no `strava__api_write`, no uploads, no
   changes at Strava.
6. **The scopes as Strava's documentation allows them**: `read`, `activity:read_all`, `profile:read_all`. The
   integration joins them with spaces (`custom_components/mira/oauth.py`); Strava's authorize takes "a comma- or
   URL-safe space-delimited string" ([Authentication](https://developers.strava.com/docs/authentication/),
   parameter `scope`, read 2026-11-18). Mira and the integration stay as they are.
7. **The series counts weeks** (1A): the weeks in a row with at least one activity, up to this one. Strava's own
   streaks are not in its web API.
8. **A line in the morning briefing comes later** (2A), as the skill's own schedule (step 3); not built now.
9. **No chain reminder** (3A): Mira names only the bike's kilometres. Strava's web API knows no parts, and
   remembering "Kette gewechselt bei 3.000 km" would put a Strava number into memory (decision 3).
10. **The best efforts from the activities** (1A, after the run of step 2): the longest ride and the biggest climb
    are the maximum of `distance` and of `total_elevation_gain` among the last 100 activities; the totals of the
    year and of all time come from `athletes/{id}/stats` (`ytd_*_totals`, `all_*_totals`). The fields
    `biggest_ride_distance` and `biggest_climb_elevation_gain` of Strava's documentation are never used: Strava did
    not send them (2026-11-18, the empty account and one with a ride).

## The Strava app

Strava gives a web API only to an app of one's own (strava.com → Settings → My API Application). It needs a Strava
subscription; a new app is in "Single Player Mode": it may connect one athlete, its owner
([Rate limits](https://developers.strava.com/docs/rate-limits/)). That is the use here: one connection per skill
for the whole house ([Connections](oauth.md), decision 4), the account of the person who connects it.

- **Authorization Callback Domain:** `my.home-assistant.io` (Home Assistant's redirect,
  `https://my.home-assistant.io/redirect/oauth`).
- **Client ID and Client Secret** go on Mira's page Connections at "Strava".
- **Limits** of a new app: 100 reading requests per 15 minutes and 1,000 a day. Strava answers 429 above them.

## In the skill

```yaml
# skills/strava/SKILL.md
metadata:
  mira:
    oauth:
      name: Strava
      authorize_url: https://www.strava.com/oauth/authorize
      token_url: https://www.strava.com/oauth/token
      scopes: [read, activity:read_all, profile:read_all]
      token_auth: post                 # Strava takes client_id and client_secret in the body
      api:
        - url: https://www.strava.com/api/v3/
          methods: [GET]
```

- **The login** is Mira's ([Connections](oauth.md), The login): no PKCE (Strava's authorize has no such
  parameter), the secret in the body. Strava's token answer has `access_token`, `refresh_token`, `expires_in` (six
  hours) and an `athlete`; Mira keeps the tokens and renews them. Strava may return a new refresh token on every
  renewal and the old one stops working; Mira replaces the stored one at once (as for X).
- **Who is connected:** whoever signs in at Strava. Every voice in the house may ask and hears that person's sport
  (one connection for the house, [Connections](oauth.md), decision 4); whose account it is, Mira learns from
  `athlete` (the first name) when asked.

## What Mira does, per use case

Every use case is a few calls of `strava__api_get`; the turn waits for them (each at most 20 s, the tool's limit).
The model reads the JSON and says it in a few spoken sentences: kilometres rounded, metres of climbing, hours and
minutes, no ids. Distances in Strava's answers are metres, times seconds, speeds metres per second.

| # | Asked | Calls (all `GET https://www.strava.com/api/v3/…`) | Said |
|---|---|---|---|
| 1 | "Wie war meine letzte Tour?" | `athlete/activities?per_page=1` | what (sport), when, how far, how long, metres of climbing, average speed |
| 2 | "Wie viel bin ich diese Woche / diesen Monat gefahren?" | `athlete/activities?per_page=100` (Strava's newest first); the model counts those whose `start_date_local` lies in the week (Monday on) or the month | number of activities, kilometres, metres of climbing, time; per sport if more than one |
| 3 | "Wie lang ist meine Serie?" | `athlete/activities?per_page=100` | the weeks in a row up to this one with at least one activity (this week counts if it has one, else it starts with last week) |
| 4 | "Wie viel hat mein Rad drauf?" (chain, tyres) | `athlete` (its `bikes` and `shoes`, each with `name` and `distance`) | kilometres per bike or shoe; for a question about the chain only the bike's total: Strava knows no parts |
| 5 | "Was sind meine Bestleistungen?" | `athlete/activities?per_page=100`, `athlete`, then `athletes/{id}/stats` | the longest ride and the biggest climb among the last 100 activities (the highest `distance`, the highest `total_elevation_gain`, with their day), the totals of this year and of all time (`ytd_*_totals`, `all_*_totals`) (decision 10) |

- **Costs:** one request for 1 to 4, three for 5. Answers grow with `per_page`: up to 100 activity summaries (each a
  few kilobytes with its map line) for 2, 3 and 5; nothing else grows. No subscription, no background work, no polling.
- **Month and series beyond 100 activities** are counted from the 100 newest; the answer says so when the 100th is
  still inside the month (the model sees it).
- **A morning line in the briefing** comes later (decision 8, step 3).

## Errors and who learns of them

| Case | How it shows | What Mira says, who else learns of it |
|---|---|---|
| Not connected, expired | the tool is not offered; opening the skill says so ([Connections](oauth.md), Problems) | "Strava ist nicht verbunden – das geht in Mira auf der Seite Verbindungen." Expired: also a repair in Home Assistant (Connections) |
| An access token that ran out | renewed before the call ([Connections](oauth.md), Renewing) | nothing; a refused refresh token makes the connection expired (as above) |
| Activities not allowed at the login (the user unticked them at Strava) | `HTTP 401` with `"field":"activity:read_permission"` after Mira's one renewal and repeat | "Strava erlaubt mir deine Aktivitäten nicht. Verbinde Strava neu und lass Aktivitäten angehakt." |
| No activities, no bike | `HTTP 200` with `[]`, `bikes: []` | "Auf Strava ist noch keine Aktivität" / "kein Rad eingetragen"; nothing invented |
| Strava's limit reached | `HTTP 429` | "Strava lässt gerade keine Anfragen mehr zu, frag in einer Viertelstunde noch mal." Not retried: the limit holds for the rest of the 15 minutes, longer than a turn waits ([Retries](retry.md): retryable, but no retry within a turn) |
| Strava fails or is not reached | `HTTP 5xx`, or the tool's error with the address (timeout after 20 s, no connection) | "Ich erreiche Strava gerade nicht." Not retried within the turn (as above); the tool's error names the address, the log of the turn has it |
| Any other answer (`4xx`) | `HTTP <status>` and Strava's body | the model says it could not read it; nothing invented |

Every request and its status is in the turn's tool log (the address without the token,
[Connections](oauth.md), Safety). A failed renewal is logged by the connection.

## Steps

1. **The skill file** `skills/strava/SKILL.md`: `oauth` and `api` as above, the description, the instructions per
   use case and per error (above), the rule against memory (decision 3). Go tests SV-01 to SV-05.
2. **Connected on the test HA and run end to end** through Mira's conversation API with the fake Show's device id:
   the Strava app's client id and secret on the Connections page, the login in the maintainer's browser, then the
   use cases 1 to 5 against the account (empty at first: the "no activities" answers). Scenarios SV-10 to SV-16.
   Run 2026-11-18 on the empty account: SV-11 (all use cases answer "keine Aktivität", "kein Rad"; Strava answered
   `HTTP 200 []`, `bikes: []`, stats with zero totals) and SV-16. With one manual ride (10 km, 1 h, 200 m, no bike):
   SV-10, SV-12, SV-13 and SV-14 answered with the right numbers; SV-14 without a bike only as "kein Rad".
   SV-15 said "längste Fahrt 10 km" from the totals: Strava's stats have no `biggest_ride_distance` and no
   `biggest_climb_elevation_gain`, also with the ride; hence decision 10. Run again with the bike assigned to the
   ride: SV-14 said the bike's 10 km (right) but added its own guess about the chain; SV-15 with decision 10 said
   the longest tour of 18 November, 10 km and 200 m, and 10 km this year (right).
3. **A line in the morning briefing** ("Gestern 42 km geradelt") as a schedule of the skill (decision 8). Later; its
   spec (time, for whom, nothing when there was no activity) before building.

## Open

- **SV-14 guesses about the chain** ("nach zehn Kilometern sollte sie noch halten", 2026-11-18) although the skill
  says to name only the bike's kilometres (decision 9).
