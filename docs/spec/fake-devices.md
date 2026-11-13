# Fake satellites that behave like the real ones – Spec

Status 2026-11-13: steps 1–5 built (step 5: the watched devices, extended with the device watch's step 7); the Show's contract recorded anew from TECHO5 v1.6.18 (with `media_title` and `media_station`); the Echo's stays as it is (no screen). A contract is never recorded from a development build (decided 2026-10-30). Scenarios: [fake-devices.feature](fake-devices.feature). Builds on the
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
- `play_media` **with `announce`** plays in the foreground: the speaker goes `playing`, the satellite `responding`.
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
  Anna's and Ben's channels in `testenv/mira.yaml`.
- Nobody home: the dashboard's switches "Sim zu Hause …" (`input_boolean.sim_da_*`) move the persons' trackers;
  with all off, `zone.home` is 0.

As every fake's time (a playback's length, speaking), the meter's and the routine's is the fake's own: Mira and the
blueprints only see the state change. The blueprints' runs are tested in pytest with these fakes and the time moved
on (`tests/ha/test_watch_blueprints.py`, CW-11 to CW-15, CW-26 to CW-29), and on the test HA with shortened
durations.

### The fake OAuth service (`internal/testsupport/fakeoauth`)

For the skills' connections ([Connections](oauth.md)), in the Go tests only: a login that answers at once with a
code, a token address that exchanges it (checking PKCE) and renews with a new refresh token each time, as X does,
the client secret as HTTP Basic or in the body (refusing the other), and a web API that wants a current access
token. A test lets the tokens run out (`Expire`), withdraws the app (`Revoke`) and reads the requests. The fake
MCP server takes its tokens (`Authorize`). The test HA does not use it: it would have to trust the fake's
certificate; there the service is X itself.

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
