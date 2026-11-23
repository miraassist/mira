# Fake satellites that behave like the real ones – Spec

Status 2026-11-23: steps 1–11 built (step 11: odd registry entries, 2026-11-23; step 10: speaking as the Show does, 2026-11-18; step 9: a question, 2026-11-17) (step 5: the watched devices, extended with the device watch's step 7; step 7: the answering machine, 2026-11-14; step 8: the fake A2A agent, 2026-11-14); the Show's contract recorded anew from TECHO5 v1.6.18 (with `media_title` and `media_station`); the Echo's stays as it is (no screen). A contract is never recorded from a development build (decided 2026-10-30). Scenarios: [fake-devices.feature](fake-devices.feature). Builds on the
fake TECHO5 devices ([testenv/README.md](../../testenv/README.md), Fake TECHO5 devices) and on
[How a TECHO5 satellite behaves](satellite.md#how-a-techo5-satellite-behaves).

## Goal

A flow that plays, speaks and listens can be run end to end by anyone, without a real satellite: against the Fake
Show and the Fake Echo in the test HA. For that the fakes report the same states, at the same transitions, as a real
TECHO5 device; today they only log what they were told.

On 2026-10-29 the answering machine took four hours, because each signal was assumed and then found missing at the
real Show. A test that runs a flow against fakes catches that early only if the fakes behave as the device does.

## Decided (2026-10-30)

| # | Question | Decision |
|---|---|---|
| 1 | End-to-end tests in the repository | Only against fake devices; a script for one real setup stays local (`*.local.py`) |
| 2 | What the fakes follow | [satellite.md](satellite.md), "How a TECHO5 satellite behaves": that section is the fakes' spec. When the real device turns out different, that section changes first, then the fake |
| 3 | A fake FRITZ!Box | Not now; the answering machine's flows are tested in Go against `fakeha` |
| 4 | The wake word | A test action; music is lowered (seen at the speaker's `volume_level`), a turn after it comes through Mira's conversation API |
| 5 | Commands in `cmd/testha` for the test actions | None; they are Home Assistant actions |
| 6 | How a playback ends | By the media's length, read by the fake (`ffprobe`); no test action for it |
| 7 | Speaking with a fake | A voice turn as a test action, through the preferred pipeline's agent with the satellite's states, not Mira's conversation API: the whole timing of a flow counts, not only the answer |
| 8 | How long a fake speaks | By the length of the text (about 15 characters a second, at least one second) |

## Two sources of truth

- **The contract** (`techo5_fake/contract/*.json`, recorded from a real device) says *what* a fake offers: actions,
  fields, entities. It stays as it is.
- **satellite.md** says *how* the device behaves: which states, at which transition. Each row there that a fake
  follows names the scenario that tests it here.

## What the fakes do

### The speaker (`media_player.<node>_speaker`)

- `play_media` **without `announce`** plays as background: the speaker goes `playing` at once. The satellite stays
  `idle`.
- `play_media` **with `announce`** plays in the foreground: the speaker goes `playing`; the satellite stays as it is
  (`idle`), as the Show's did on 2026-11-22 (before, the fake set it `responding`).
  The wake word neither lowers nor stops it.
- **The end of a playback** comes from the media, as on the real device: the fake fetches the media it is given once
  and reads its length (`ffprobe`, which the Home Assistant image has; any format), plays nothing, and when the
  length is over the speaker goes `idle` (and the satellite `idle`, if it was `responding` for it). Media without a
  length (a stream) or that cannot be read plays until it is stopped, like a radio. Tests use short files of known
  length in `testenv/`. The time lives in the fake, as in the device; Mira still only sees the state change.
- `media_stop` and `media_pause` → `paused`; a `play_media` that replaces one still playing reports no change of
  state (stays `playing`), as the real device.
- `esphome.<node>_media_title` names the next `play_media` once: the speaker shows it as `media_title` while it plays
  or is paused, and it is gone when the playback ends.
- `esphome.<node>_media_station` names the next `play_media` once as a station: the speaker shows its name as
  `media_channel` (the real device shows the station's name and logo on its screen), gone when the playback ends.

### The satellite (`assist_satellite.<node>_assist_satellit`)

- **Speaking takes as long as the text**: about 15 characters a second, at least one second, like a voice. It is the
  fake's own time, not a signal Mira uses: Mira waits for the state.
- **A plain announcement** (`assist_satellite.announce`): `responding`, then `idle`; the call returns when it is
  over.
- **`start_conversation`**: `responding`, then `idle` for a moment, then `listening`, then `idle` when no turn comes.
  The short `idle` is there, so a flow that takes it for the end shows its mistake against the fake too.
- **A question** (`assist_satellite.ask_question`, since 2026-11-17): `responding` while the question is spoken, a short
  `idle`, then `listening` (as the real Show on 2026-11-16). A `techo5_fake.say` while it listens is the answer: it goes to Home Assistant, which matches it against
  the question's `answers` as for a real device, and the satellite goes `idle`; the text goes to no conversation agent
  and there is no `processing` (the real Show showed none on 2026-11-16). Nobody answers within the listening time:
  `idle`, and Home Assistant's action fails with "No answer received from the satellite", as at the real Show (which
  listens longer, about 15 s).
- **The wake word**: a test calls `techo5_fake.wake_word` for the device. The satellite goes `listening`; a background
  playback is lowered (the speaker's `volume_level` attribute shows it lowered by the device's 15 dB while it lasts),
  a foreground playback is not. Without a following turn the satellite goes `idle` again and the background
  playback carries on at its volume. A test that wants a turn sends it through Mira's conversation API with the
  fake's device id, as the real device would after the wake word.
- **A voice turn** (the action `techo5_fake.say`, with `device` and `text`): what the device does when someone speaks
  after the wake word, without a microphone. The satellite goes `listening`, then `processing`; the text goes to the
  conversation agent of Home Assistant's preferred Assist pipeline (in the test HA: Mira) with the fake's device id
  and satellite, as Home Assistant's pipeline sends it; then `responding` for as long as the answer takes to speak,
  then `listening` again if the answer asks for it (`continue_conversation`, in the same conversation), else `idle`.
  Background music is lowered for the whole turn. The action returns when the answer is spoken, with the answer's
  text and whether it listens on. A `say` while the satellite listens after an answer continues that conversation,
  as a follow-up does at the real device. This is what a flow test speaks with: the satellite reports the states of
  a voice turn, so Mira's waits for them are tested, not their safety nets.
- **A device without a screen** (Fake Echo) takes `notify` and shows nothing; its ring is the light on its own
  sub-device, as on the real Dot.

### The test actions

The fakes get two actions of their own, for tests only (the real device has nothing to compare them with, so they
are not in the contract): `techo5_fake.wake_word` (`device`, the node: `fake_show`) and `techo5_fake.say` (`device`,
`text`), as above. By hand they are called like any action in Home Assistant (developer tools, Actions).

### The watched devices (`testenv/watch_fake`)

For Mira's device watch ([Device watch](device-watch.md)): the test HA has no water meter and no plug whose power
follows a day, a cycle or a programme. The fake integration `watch_fake` (`watch_fake:` in the test HA's
configuration) brings four sensors, without a device, exposed to Assist by `testha up`:

- **A water meter** `sensor.fake_wasserzahler` (device class `water`, state class `total_increasing`, litres; keeps its
  value over a restart). It changes only while water runs: `watch_fake.water_run` (`litres`, default 10; `every`
  seconds, default 300, as a meter that reports every 5 minutes; `for` seconds, optional, else until stopped) counts up
  at once and every `every` seconds; `watch_fake.water_stop` ends it.
- **A plug's power** `sensor.fake_aquarium_leistung` (device class `power`, state class `measurement`, watts):
  `watch_fake.power` (`watts`) sets it and ends a routine; `watch_fake.routine` (`windows`, e.g.
  `07:40-11:40, 14:20-21:00`; `watts`, default 50) lets it follow a daily routine: `watts` within the windows, else
  0, set at once and at every full minute. The routine lives in memory: a restart of the test HA ends it.
- **A fridge's plug** `sensor.fake_kuhlschrank_leistung` (Küche): `watch_fake.cycle` (`watts`, default 45; `on` and
  `off` seconds) switches it on and off again and again, starting on, until `watch_fake.power` sets it.
- **A dishwasher's plug** `sensor.fake_spulmaschine_leistung` (Küche): `watch_fake.program` (`steps`, seconds and
  watts each, e.g. `600:80, 1200:2000, 300:0, 1800:80`) runs a programme through its steps, then 0 W.
- `watch_fake.power`, `routine`, `cycle`, `program` and `past` take the plug's `entity_id` (default the aquarium's for
  `power`, `routine` and `past`, the fridge's for `cycle`, the dishwasher's for `program`). The cycle and the
  programme live in memory, as the routine.
- **A plug's past** for its device profile: `watch_fake.past` (`windows`, empty for never on, or `cycle`, minutes on
  and off as `20:40`; `watts`, default 50; `days`, default 10; `stats_days`, default 90; `peak_every` hours, default
  0, and `peak` watts, default 2000: an hour of a device run in the statistics every so many hours) writes into the
  test HA's recorder the plug's states every 5 minutes (on the full 5 minutes) of the last `days` days before its
  first recorded state, and its hourly statistics (mean, min, max) of the last `stats_days` days
  (`async_import_statistics`), both following the windows or the cycle. The states go straight into the
  recorder's tables (Home Assistant has no action for a past state); for the test HA only.

### The watched group (`watch_fake`, for the [Group watch](group-watch.md))

The fake house a group watch watches, made by `watch_fake` at its setup (test only):

- **Floors and areas:** Kellergeschoss (Keller), Erdgeschoss (Wohnzimmer, Küche, Flur, Bad, Garage), Dachgeschoss
  (Schlafzimmer, Büro); Garten and Terrasse without a floor. Made when missing; an area that has a floor keeps it.
- **Openings:** `binary_sensor.fake_fenster_wohnzimmer`, `_kuche`, `_schlafzimmer`, `_buro` (windows),
  `binary_sensor.fake_kellerfenster` (window, Keller), `binary_sensor.fake_haustur` (door, Flur),
  `binary_sensor.fake_gartentur` (door, Wohnzimmer), `binary_sensor.fake_garagentor` (garage door).
- **Motion:** `binary_sensor.fake_bewegung_garten` (motion, Garten), `binary_sensor.fake_prasenz_buro` (presence).
- **Temperatures:** `sensor.fake_temperatur_keller` (12 °C), `sensor.fake_temperatur_wohnzimmer` (21 °C).
- **Lights:** `light.fake_licht_kuche`, `_schlafzimmer`, `_terrasse`, `_garten`, each in its area; switched as every
  light.
- Each entity goes into its area unless it has one. `watch_fake.set` (`entity_id`, `state`: `on`, `off`, a
  temperature, `unavailable`, `unknown`) sets any of them.
- **Group helpers** (the test HA's `configuration.yaml`): `binary_sensor.fake_fenster_gast` (the windows of the
  bedroom and the office) and the light group `light.fake_lichter_aussen` (terrace and garden).
- **Phones:** `notify.fake_handy_anna` and `notify.fake_handy_ben`, legacy notify services as a companion app's; what
  each was sent is kept in `sensor.fake_handy_anna` / `_ben` (the count; the last ten in the attribute `messages`).
  Anna's and Ben's channels are set in the test HA's Mira integration (Mira's page, Persons), as every person's.
- Nobody home: the dashboard's switches "Sim zu Hause …" (`input_boolean.sim_da_*`) move the persons' trackers;
  with all off, `zone.home` is 0.
- **Devices** (for the battery and the availability watch): unlike the rest of the fake house these entities
  belong to devices, under the config entry "Fake Geräte" that `watch_fake` makes for itself at its setup; each
  device is in its area. "Fake Rauchmelder Flur" (`sensor.fake_rauchmelder_flur_batterie`, battery 90 %), "Fake
  Wassermelder Keller" (`binary_sensor.fake_wassermelder_keller_batterie`, battery low off, and
  `binary_sensor.fake_wassermelder_keller_wasser`, moisture), "Fake Klima Garten"
  (`sensor.fake_klima_garten_batterie`, 70 %, and `sensor.fake_klima_garten_luftfeuchtigkeit`), "Fake Steckdose
  Garage" (`switch.fake_steckdose_garage` and `sensor.fake_steckdose_garage_signal`). `watch_fake.set` sets any of
  their entities (a percentage, `on`, `off`, `unavailable`, `unknown`); `watch_fake.reachable` (`device`: its name
  or key, `reachable`) loses a whole device (every entity `unavailable`) or brings it back. A lost device stays
  lost over a restart of Home Assistant, as a device without a battery would.

As every fake's time (a playback's length, speaking), the meter's and the routine's is the fake's own: Mira and the
blueprints only see the state change. The blueprints' runs are tested in pytest with these fakes and the time moved
on (`tests/ha/test_watch_blueprints.py`, CW-11 to CW-15, CW-26 to CW-29), and on the test HA with shortened
durations.

### The answering machine (`testenv/fritz_fake`, as `mira_fritz`)

For the evals of the answering machine's messages ([Evals](evals.md), Messages and their kinds; decided 2026-11-14):
the fake takes the place of Mira FRITZ!Box in the test HA, under its domain `mira_fritz` (mounted as
`custom_components/mira_fritz`, `mira_fritz:` in the configuration), because the Mira service knows the answering
machine by that domain. The real Mira FRITZ!Box and its box are no longer in the test HA.

- **Two sensors without a device**, as Mira FRITZ!Box's: new messages (unique id ending in `_new_messages`) and missed
  calls (`_missed_calls`), each the count as its state.
- **Mira FRITZ!Box's actions** the Mira service calls, with the same fields and answers: `get_messages` (`new_only`;
  each message with `id`, `name`, `date`, `minutes`, `new`, `media_content_id` and `answering_machine_name`),
  `get_calls` (`kind`, `new_only`; each call with `kind`, `name`, `date`), `mark_heard` (`message_ids`; the count
  goes down), `mark_calls_seen`, `delete_messages` (`message_ids`) and `get_phonebook` (no books, no contacts). A message's
  `media_content_id` is the fake's two-second WAV of the test HA.
- **Test actions:** `mira_fritz.fake_add` (`recordings`, a list of names; `minutes`, default 1) adds new recordings
  dated now, all at once, so the count rises once; `mira_fritz.fake_reset` empties it. Everything lives in memory: a
  restart of the test HA empties it.

### The fake OAuth service (`internal/testsupport/fakeoauth`)

For the skills' connections ([Connections](oauth.md)), in the Go tests only: a login that answers at once with a
code, a token address that exchanges it (checking PKCE) and renews with a new refresh token each time, as X does,
the client secret as HTTP Basic or in the body (refusing the other), and a web API that wants a current access
token. A test lets the tokens run out (`Expire`), withdraws the app (`Revoke`) and reads the requests. The fake
MCP server takes its tokens (`Authorize`). The test HA does not use it: it would have to trust the fake's
certificate; there the service is X itself.

### The fake A2A agent (`testenv/a2a_fake`)

For Mira's remote agents ([Remote agents](remote-agents.md), decisions 23 to 25): an A2A agent without a model,
built with the official Go SDK (`github.com/a2aproject/a2a-go/v2`), deterministic. The Go tests start it in the
process (`a2afake.New`, `httptest.NewServer`); the test environment runs it as the container `mira-test-a2a`
(`testenv/compose.yaml`, port 8791; `-wait` sets how long the skill `slow` works).

- **One agent, one skill per behaviour.** The skill a message names in its metadata (`skill`, decision 27 of the
  remote agents) picks it: `complete` (completed at once, one artifact with a fixed text), `slow` (working until
  released, or until `-wait` is over, then completed), `ask` (input-required with "Wie groß ist die Wohnfläche?";
  a message with the same task id completes it with the answer in its artifact), `ask-twice` (as `ask`, but the
  first answer makes it ask again, "Wie alt ist das Haus?"; the second completes it), `login` (auth-required), `fail`
  (failed with a status text), `reject` (rejected), `push-twice` (as `complete`, every push sent twice), `message`
  (a message instead of a task). A message that names no skill fails with a status text that says so.
- **The cards** are served under several paths, one per kind of card (the simplest of the ways: no start flag, so
  one fake serves every test): the card at `<path>/.well-known/agent-card.json`, its JSON-RPC endpoint at
  `<path>/a2a`, both on the fake's address as the request reached it. The root: push and streaming, a bearer token;
  `push-only`, `stream-only`, `neither`: the capabilities they name, a bearer token; `oauth2`: an OAuth2 login
  (authorization code); `api-key`: an API key in the header `X-API-Key`; `open`: no login. The fake accepts one
  token for all of them (`-token`, by default `fake-a2a-token`), and answers 401 to a missing or wrong one.
- **What it received** is kept in order: each card read, each JSON-RPC call with its method and the credential that
  came (refused or not), a message's text, skill, ids and push configuration, each cancel, each push it sent (to
  where, with which token, the task's state). The Go tests read it (`Received`); by hand
  `GET /fake/received`; `POST /fake/release` ends the work of the waiting `slow` tasks, `POST /fake/reset` forgets
  what was received.
- **Pushes** go out with the SDK's sender, which refuses private addresses by default; the fake allows them, since
  the Mira service it pushes to is in the home network. The SDK sends the push token in the header
  `A2A-Notification-Token`.

## What it costs

Nothing for Mira: these are fakes in the test HA. Per `play_media` the fake fetches the media once and runs
`ffprobe` on it (in the executor, not on Home Assistant's event loop), then keeps one timer for its length; the
satellite keeps its "speaking" time. The wake word is one call to the test HA; a voice turn one call, which runs one
turn at the conversation agent.

## Steps

1. **The speaker** as above, with `media_title` and the end by the media's length, and short test files in `testenv/`
   (FG-0x), tested in pytest against the fake integration.
2. **The satellite**: announcement, `start_conversation` with its short `idle`, the wake word with lowering (FG-1x),
   tested in pytest.
3. **satellite.md** names the scenario next to each behaviour, and the README of `testenv` describes the test action
   and the test files; no `testha` commands of their own (decided 2026-10-30).
4. **A voice turn** (`techo5_fake.say`) and speaking by the length of the text (FG-15 to FG-17), tested in pytest; the
   answering machine's "all messages, one after the other" played through with it at the test HA.
5. **The watched devices** (`watch_fake`): the water meter and the plug's power, for the device watch (built
   2026-11-13 with its step 3; tested with the blueprints in pytest, CW-11 to CW-15); the fridge's and the
   dishwasher's plugs and the past of a cycle and of device runs with its step 7 (CW-26 to CW-29).
6. **The watched group** (`watch_fake`): floors and areas, openings, motion, temperatures, lights, two group helpers and
   the persons' phones, for the group watch (built 2026-11-13 with its step 1; tested with its blueprints in pytest,
   GW-01 to GW-03, GW-08 to GW-16).
7. **The answering machine** (`testenv/fritz_fake` as `mira_fritz`): the two sensors, Mira FRITZ!Box's actions and the
   test actions, for the evals of the messages' kinds (2026-11-14); tested in pytest (FG-30 to FG-32).
8. **The fake A2A agent** (`testenv/a2a_fake`): the behaviours, the cards, what it received, its container
   (2026-11-14, step 0 of [Remote agents](remote-agents.md)); tested in Go (`testenv/a2a_fake`).
9. **A question** (`assist_satellite.ask_question`, 2026-11-17): answered by `techo5_fake.say`, or no answer within
   the listening time (FG-18, FG-19), tested in pytest; for the skill `angehoerige`, whose automations ask.
10. **Speaking as the Show does** (2026-11-18, for [Streaming](streaming.md), steps 10 to 12): while the satellite
    speaks an answer its speaker reports `playing` (the Show's does, streaming row 12), the wake word cuts the answer
    off (speaker `idle`, then satellite `listening`; `techo5_fake.say` answers with `interrupted`), and an empty
    answer is not spoken (Home Assistant's pipeline skips it). Tested in pytest (FG-20 to FG-22).
11. **Odd registry entries** (2026-11-23, issue 8; [Context](context.md), 1.3, decision 5): an integration may store
    a value of another type than Home Assistant declares, and Home Assistant keeps it; one such device made Mira lose
    the whole device registry. `watch_fake` has such entries on purpose, under "Fake Geräte", so that a reader that is
    too strict fails on the test HA first: the device "Fake Wechselrichter" with the identifier `4711` as a number, an entity of the registry only, without a state, whose `unique_id` is
    the number `4712`, and a device whose name is the number `4714` (Home Assistant does not check a device's name,
    [Reading what comes in](parsing.md)) (Home Assistant logs an error for it at its creation and keeps it). Nothing watches them; they
    have no area. Not here: `model`, `sw_version` and `hw_version` as numbers, which Home Assistant 2026.11 turns into
    text itself (with a warning that it refuses them from 2026.12; checked 2026-11-23), so they never reach Mira as
    numbers. A further kind found in a user's Home Assistant joins this list. Tested in pytest (FG-33); Mira's
    reading of them in Go (AD-06, HU-05).
