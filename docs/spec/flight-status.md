# Flight status – spec

Status 2026-11-18: step 1 built (the skill `flug`, its landing automation, the script in the guide), step 2 run on
the test HA with real flights; step 3 built (the skill brings its script; the delay named); released. Scenarios: [flight-status.feature](flight-status.feature).

## Goal

"Wann landet LH 123?", "Behalt Omas Flug im Auge": Mira tells the state of one flight and reports by herself when it
has landed. Browsing timetables is not a goal: a list of flights is read on a screen, not heard.

## The concept it hangs on

Nothing of Mira's own: Home Assistant's integration **Flightradar24** (HACS,
[AlexandrErohin/home-assistant-flightradar24](https://github.com/AlexandrErohin/home-assistant-flightradar24), checked
with v2.5.0) tracks a flight; a **Home Assistant script** exposed to Assist starts and ends the tracking and gives the
state back (as the skill `kamera` asks a camera through a script); a **skill automation** from a blueprint
([Skill files](skill-files.md)) reports the landing with `mira.send_message`. No Mira code.

Why a script and not a tool of Mira's: Home Assistant's Assist MCP server offers no generic action and no template
(checked on the test HA 2026-11-18: `homeassistant__call_action` and `render_template` are only on `/api/mcp`, not on
`/api/mcp/assist`, which Mira uses), and `GetLiveContext` shows the tracked flights only as a count, without the
attribute `flights`. A script exposed to Assist is a tool of Home Assistant's MCP server and returns its response
(checked). Since step 3 the skill brings the script as a script blueprint, and Mira makes and exposes it herself
([Skill files](skill-files.md), step 5); until then the user copied it from the guide.

## Decided (2026-11-18)

| # | Question | Decision |
|---|---|---|
| 1 | The source | The HACS integration Flightradar24: no account, no key; flights tracked by number; events on take-off, landing and the gate (decided by the maintainer: check what Home Assistant offers first) |
| 2 | How the model reads and tracks | One script "Mira Flug" with `aktion` (verfolgen, stand, vergessen) and `flugnummer`; it calls the integration's text entities and returns the tracked flights in a compact form with local times. Since step 3 (2026-11-18, by the maintainer): the skill's script blueprint `flug.yaml` and the declared script `abfrage`, so `script.mira_skill_flug_abfrage`; Mira makes it, does not expose it, and it reaches the model as the skill's tool `flug__abfrage` once the skill is opened ([Skill files](skill-files.md), decision 27; before: `script.mira_flug` from the guide, exposed) |
| 3 | The notice | A skill automation on the event `flightradar24_tracked_landed`: `mira.send_message`, importance 2, dedupe per flight |
| 4 | After the landing | The automation ends the tracking (`text.flightradar24_remove_from_track`): tracked by its number, the flight would be tracked again on its next day |
| 5 | Delays | No notice of its own (no event exists; the estimate changes every few minutes): Mira names a delay when asked |
| 6 | Naming the delay (by the maintainer, 2026-11-18, 3A) | The script's answer has `verspaetung_minuten` (the real arrival once landed, else the expected one, minus the scheduled, rounded; checked: after the landing the integration has no expected arrival any more); the skill tells the model to say a delay of more than 5 minutes, or an arrival more than 5 minutes early, always, also unasked |
| 7 | A flight that has landed already (by the maintainer, 2026-11-18, 2B) | When tracking finds the flight landed, the script takes it out of tracking at once and answers `schon_gelandet: true`, `verfolgt: false`; the model says it landed (arrival, terminal, delay) and that no message comes. Reason: the integration fires no landing event for a flight added after its landing, so its tracking would never end (seen with A3822) |
| 8 | Forgetting (by the maintainer, 2026-11-18, 3A) | After `vergessen` the model says clearly that it no longer keeps an eye on the flight, also after a short follow-up ("Und LH 456 auch"); seen before: "Behalte ich im Auge." while the flight was forgotten |
| 9 | Times in the answer (by the maintainer, 2026-11-18, 4A) | A time of today is "heute 20:15", any other with weekday and date, "Do 19.11. 06:40" (blueprint `mira-version: 4`). Reason: with "18.11. 20:15" the model said "gestern" for a landing that evening (FS-05) |

## What the integration gives (checked on the test HA, 2026-11-18)

| What | Evidence |
|---|---|
| Tracking | `text.set_value` on `text.flightradar24_add_to_track` with the number; the call returns after the lookup, and `sensor.flightradar24_additional_tracked` already lists the flight (same second) |
| A number it does not know | the call succeeds; only Home Assistant's log says `Add Track - No flight found by - <number>`; the sensor is unchanged |
| A flight in the air ("live") | in the attribute `flights`: `flight_number`, `callsign`, airports (city, IATA), `time_scheduled_departure`, `time_real_departure`, `time_scheduled_arrival`, `time_estimated_arrival`, `time_real_arrival` (Unix seconds), `airport_destination_terminal`, `has_landed`, `on_ground`, `altitude` |
| A flight not yet flying ("schedule") | the number and `tracked_type: schedule`, no airports and no times |
| No gate, no delay | neither is a field; a delay is `time_estimated_arrival` minus `time_scheduled_arrival` |
| The estimate | changes while the flight comes in (SN2587: 17:57 → 17:58 → 18:01 within ten minutes); no event for it |
| Landing | the event `flightradar24_tracked_landed` with the whole flight (`has_landed: true`; `time_real_arrival` was still empty in it) about a minute after touchdown (SN2587 18:00:21, LX970 18:14:45); `flightradar24_tracked_arrived_gate` came seven minutes after SN2587's landing |
| Rate limits | the integration logged `get_flight_details is rate limited (HTTP 429)` once while three flights were added within a minute, and paused that call for 30 s |
| The data source | unofficial Flightradar24 endpoints (the README: no subscription needed, for one's own use); they may change or refuse |

## What it does, step by step

**Asking or tracking** (a voice turn waits for it):

1. The model opens the skill `flug` and calls the script with `aktion` and `flugnummer` (no spaces, upper case).
2. The script: for `verfolgen` `text.set_value` on the add entity, for `vergessen` on the remove entity (1 call to
   the integration, which asks Flightradar24 once: a search, and for a live flight its details); then one template
   over the sensor's attribute. `stand` makes no call to Flightradar24.
3. For `verfolgen`, a flight the answer shows landed is removed from tracking again (1 more call to the integration;
   decision 7).
4. The answer: `flugnummer`, `verfolgt` (whether that number is tracked now), `schon_gelandet`, `fluege` (each: von, nach, stand,
   abflug_geplant, abflug, ankunft_geplant, ankunft_erwartet, verspaetung_minuten, ankunft, terminal). It grows with the number of tracked
   flights, which is a handful.
5. The model says it; `schon_gelandet` true means it landed already and is not tracked; otherwise `verfolgt` false
   after `verfolgen` means the number is not known now.

**The landing** (no turn waits):

1. The integration fires `flightradar24_tracked_landed`.
2. The automation sends `mira.send_message` ("Der Flug SN2587 aus Brüssel ist gelandet in Berlin.", importance 2,
   `dedupe_key` per flight id, expires in 6 h) and removes the flight from tracking.

**What the model is told:** the tool's description (one or two sentences from the declaration: what it does, its
fields) and the skill's prose, which alone holds the rules. The tool comes only with the skill, so the prose is there
whenever the tool is (decision 27 of Skill files; before it, the exposed script was called without opening the skill,
and the prose did not reach the model): the
tool `flug__abfrage`, its three actions, the fields of the answer, a delay over 5 minutes said always, that a
number has no date, that there is no gate, that "noch keine Daten" means no times yet, and that a landed flight is no
longer tracked.

## Errors

| What fails | What happens | Who learns of it |
|---|---|---|
| The integration is missing | the script fails (its text entities are not there) | the model says that the integration Flightradar24 is needed (skill prose) |
| Mira could not make or expose the script | the tool is not there | a problem of the skill (repair) names the script and the reason ([Skill files](skill-files.md), Errors); the model says the integration is needed |
| The number is not known | `verfolgt` is false | the model says so and asks whether the number is right; Home Assistant's log has the integration's line with the number |
| Flightradar24 refuses (429) or does not answer | the integration pauses that call and logs it; a flight added then may have no details yet | Home Assistant's log; the model says what the answer has |
| `mira.send_message` fails | the automation stops there; the flight stays tracked | Home Assistant's automation trace and log |
| A flight without times ("schedule") | the answer says "noch keine Daten" | the model says so |

## Open

- The flight a number means when it flies twice a day, or tomorrow: the integration picks it (assumption: the one
  flying now, else the next by plan); there is no date.
- A delay notice before landing (decision 5).
- Whether `flightradar24_tracked_landed` comes for a flight added before take-off (checked only for flights added in
  the air).
- (Settled by decision 7:) a flight tracked after it landed got no landing event and stayed tracked (seen 2026-11-18
  with A3822, added eight minutes after its landing).

## Steps

1. **The skill `flug`**, its blueprint `gelandet.yaml` and the automation it declares; the script and the setup in the
   guide. FS-01 to FS-03. Built 2026-11-18.
2. **The run on the test HA** with real flights through the fake Show: asking, tracking, a number not known,
   forgetting, the landing reported and the tracking ended. FS-10 to FS-14. Run 2026-11-18: all five as written; in
   FS-10 Mira named the expected arrival and the terminal but not the delay against the plan (16 minutes).
3. **The skill brings its script** (decisions 2 and 6, 2026-11-18): the script blueprint `flug.yaml`, the declared
   script `abfrage` with the tool's description, `verspaetung_minuten`, the guide without the script to copy. Needs
   [Skill files](skill-files.md), step 5. FS-04; FS-10, FS-11 and FS-13 run again. Built 2026-11-18. Run 2026-11-18:
   at the first start the script was made, exposed and a tool at once; A3822 (landed, 48 minutes late): "… um 19:08
   Uhr in Terminal 1 gelandet, mit 48 Minuten Verspätung"; SN2588 in the air: "… landet voraussichtlich um 20:23 Uhr,
   das sind 24 Minuten später als geplant"; forgetting both left nothing tracked. Decisions 7 and 8 the same day,
   run again: FS-05 with LH1962 fourteen seconds after its landing: "Der Flug ist schon gelandet.", not tracked (the
   script's answer: `schon_gelandet: true`, arrival 19:49, `verspaetung_minuten: -5`; Mira did not name the arrival);
   a number tracked 15 and 22 minutes after its landing already meant its next flight (`tracked_type: schedule`), so
   decision 7 covers only the minutes right after a landing. FS-13 as a follow-up in one conversation: "Den Flug
   AY1406 verfolge ich nicht mehr." – "SN2588 verfolge ich ebenfalls nicht mehr.", nothing tracked; said as a new
   conversation without history, "Und SN 2588 auch" was taken as tracking.
   Changed the same day with Skill files' decision 27 (by the maintainer): the script is not exposed but the tool
   `flug__abfrage` of the skill; its description is two sentences, the rules are in the prose only; FS-05 names the
   arrival and the delay.
   Run 2026-11-18 after the change, through the fake Show: every flight turn opened the skill and then called
   `flug__abfrage`; Home Assistant's MCP server listed 33 tools again (the script not among them). FS-05 with LG9473
   about three minutes after its landing: "Flug LG 9473 ist gestern um 20:15 Uhr in Terminal 1 gelandet, neun Minuten
   früher als geplant." – arrival and delay named, not tracked, but "gestern" is wrong (it landed that evening; the
   answer said `18.11. 20:15`). FS-10 with DE433: "… ist in der Luft, hat 33 Minuten Verspätung und wird
   voraussichtlich um 20 Uhr 23 am Terminal 1 erwartet …". FS-11 with EW2006 tracked. FS-13 as a follow-up in one
   conversation: "Flug DE 433 verfolge ich nicht mehr." – "Flug EW 2006 verfolge ich ebenfalls nicht mehr.", the second
   without opening the skill again; nothing tracked.
   Decision 9 the same day: the times as "heute 20:15" or "Do 19.11. 06:40". Run again: FS-05 with U25120 four minutes
   after its landing: "U2 5120 ist heute schon um 20:29 Uhr gelandet, 15 Minuten früher als geplant, Terminal 1.", not
   tracked; FS-10 with LH202 in the air (the answer: "heute 20:40" expected, `verspaetung_minuten: -9`): "Der Flug
   kommt um 20:40 Uhr an und ist neun Minuten früher als geplant in der Luft …". No "gestern".
