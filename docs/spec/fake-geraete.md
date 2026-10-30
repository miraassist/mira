# Fake satellites that behave like the real ones – Spec

Status 2026-10-30: steps 1–4 built; FG-06 waits until `media_title` is in a released firmware; then the contract is recorded anew (decided 2026-10-30), never from a development build. Scenarios: [fake-geraete.feature](fake-geraete.feature). Builds on the
fake TECHO5 devices ([testenv/README.md](../../testenv/README.md), Fake TECHO5 devices) and on
[How a TECHO5 satellite behaves](satellit.md#how-a-techo5-satellite-behaves).

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
| 2 | What the fakes follow | [satellit.md](satellit.md), "How a TECHO5 satellite behaves": that section is the fakes' spec. When the real device turns out different, that section changes first, then the fake |
| 3 | A fake FRITZ!Box | Not now; the answering machine's flows are tested in Go against `fakeha` |
| 4 | The wake word | A test action; music is lowered (seen at the speaker's `volume_level`), a turn after it comes through Mira's conversation API |
| 5 | Commands in `cmd/testha` for the test actions | None; they are Home Assistant actions |
| 6 | How a playback ends | By the media's length, read by the fake (`ffprobe`); no test action for it |
| 7 | Speaking with a fake | A voice turn as a test action, through the preferred pipeline's agent with the satellite's states, not Mira's conversation API: the whole timing of a flow counts, not only the answer |
| 8 | How long a fake speaks | By the length of the text (about 15 characters a second, at least one second) |

## Two sources of truth

- **The contract** (`techo5_fake/contract/*.json`, recorded from a real device) says *what* a fake offers: actions,
  fields, entities. It stays as it is.
- **satellit.md** says *how* the device behaves: which states, at which transition. Each row there that a fake
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
3. **satellit.md** names the scenario next to each behaviour, and the README of `testenv` describes the test action
   and the test files; no `testha` commands of their own (decided 2026-10-30).
4. **A voice turn** (`techo5_fake.say`) and speaking by the length of the text (FG-15 to FG-17), tested in pytest; the
   answering machine's "all messages, one after the other" played through with it at the test HA.
