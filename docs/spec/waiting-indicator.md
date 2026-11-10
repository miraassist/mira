# Showing that Mira waits – Spec

Status 2026-11-10: steps 1 to 4 built (not released), `internal/waiting`; every row of the state table checked on the
real Show and the Dot 2; the run through Mira at a real satellite is open. Scenarios: [waiting-indicator.feature](waiting-indicator.feature).

## Goal

Some threads wait for something outside for a while: a Zigbee device to join and be interviewed (Zigbee2MQTT took more
than a minute in the real run, 2026-11-09), a Wi-Fi device to turn up in the home Wi-Fi. Today nothing shows it: the
user does not know whether Mira still waits or has given up. Feedback of the real run: show the wait at the satellite,
so the user sees Mira is busy.

1. Stefan: "Ich hab ein neues Zigbee-Gerät." Mira opens pairing and says to press the device's button.
2. The Show plays a ring that runs down, with "Ich warte auf dein Gerät …" under it; an Echo's LED ring runs down.
3. The device turns up: the ring stops, Mira asks for the room.

## Decided (2026-11-10)

| # | Question | Decision |
|---|---|---|
| 1 | The Show | A silent video of a turquoise ring that runs down clockwise, the words "Ich warte auf dein Gerät …" under it, played with the TECHO5 action `esphome.<device>_play_video`. Tried on the real Show 2026-11-10: it plays full screen; at its end the Show goes back to its home screen; "that can be used for the Show as it is" |
| 2 | The Echo's LED ring | The TECHO5 timer: `esphome.<device>_timer_start` shows a ring that runs down (seen on the Dot 2, 2026-11-10: "sieht gut aus"), with two new parameters, silent (no alarm at the end) and the colour (the orange is too bright); the action `esphome.<device>_timer_start_silent` with `color`, turquoise (decided 2026-11-10). Cancelling a timer shortly before its alarm is no way: it came too late and the Dot refuses `timer_cancel` for a timer set through Home Assistant ("cancel it there"). A light effect (`Comet`) was "not bad, but that goes better"
| 3 | Where it is used first | The threads that wait for a device: Zigbee pairing (Zigbee2MQTT and ZHA) and the Wi-Fi hand-over. Other waits later, on the same building block |
| 4 | The length of the video | One video of 60 s, played again on its end while the thread waits |
| 5 | The Show's switch "Video" is off | Mira turns it on (`switch.<node>_video`) and logs that she did; it stays on |

## Building blocks

- **A thread with a plan** ([Threads](threads.md)) and its satellite: the wait belongs to the thread and is shown at
  the satellite where the thread began, the same one its question is asked at.
- **The sounds' way to the satellite** ([Sounds and cards on the satellite](satellite.md)): the video is a file in the
  binary next to Mira's sounds, passed through by the integration as the page's own files
  (`/api/mira/panel/ui/videos/<name>.mp4`) at Home Assistant's address, which every satellite reaches.
- **Departure: a video, not a card.** A card (`notify_picture`) cannot move; the Show's video player can, and it says
  when the video is over (`sensor.<node>_video_state`, a signal).

## What it does

1. **Start.** When a thread starts to wait for a device (Zigbee: pairing open, on Zigbee2MQTT and ZHA; Wi-Fi: the
   hand-over begins, joining the device's Wi-Fi through the discovery), Mira waits for the satellite to be idle after
   the turn's answer (`assist_satellite` `responding` → `idle`, the signal; at most 30 s as a safety net, then it shows
   anyway and the log says `the satellite did not get idle`): while Mira still speaks the ring and the screen show her
   answer, and the wait would have run part of its minute unseen (seen 2026-11-10 at the Dot 2: the timer started 13 s
   before the answer was over). Then Mira plays the
   video at the thread's satellite if it has the action `esphome.<node>_play_video` (one action call; the Show's
   switch `switch.<node>_video` must be on: when it is off Mira turns it on first, decision 5). An Echo gets a silent
   timer of 60 s in turquoise (decision 2, `esphome.<node>_timer_start_silent`), started again on its end while the
   thread waits, as the video; called with a response, which gives the timer's id at once (row 7); stopped with
   `esphome.<node>_timer_cancel` and that id (row 8).
2. **Running.** The Show plays it to its end and goes back to its home screen (`sensor.<node>_video_state` `playing`
   to `idle`, seen 2026-11-10). While the thread still waits, Mira plays it again on that signal (decision 4).
3. **End.** When the wait ends, Mira stops the video (`esphome.<node>_stop_video`) or cancels the timer before she
   says anything: Zigbee before every announcement but "I am getting to know it" (the device found or known, nothing
   joined, a failed interview); Wi-Fi before every announcement of the hand-over (all of them end the wait); and
   whenever the thread ends (cancelled, expired, done). A video that went from loading to idle without playing (the
   Show could not load it, an older integration that does not pass the video) is not played again: log
   `waiting video refused`.
4. **Words.** The video carries the words of the household's language: one video per language (German, English).

### Cost

Per wait: whether the satellite has the actions (Home Assistant's action list, read once and kept); for a Show one
read of its switch "Video" (and one action to turn it on when off), one action call to start, one more per minute
while the wait lasts, one to stop, and subscriptions filtered to its two sensors `sensor.<node>_video_state` and
`sensor.<node>_video_error`; for an Echo one action call with a response per minute, one to cancel, and a
subscription to `sensor.<node>_timers`. The turn waits for none of the replays; the first start runs in the turn that
opens pairing (a few calls).

### Fails

| Fails | Who learns of it |
|---|---|
| The Show's switch "Video" is off (`sensor.<node>_video_error` "video: videos are off on this device", seen 2026-11-10) | Mira turns it on and plays the video; log `turned the satellite's video on` once |
| The Show refuses the video for another reason (`sensor.<node>_video_error`) | Nothing is shown; log `waiting video refused` with the satellite and the error, once per thread |
| The action call fails | Log `could not show the wait` with the error; the thread goes on without it |
| Stopping fails | Log `could not stop the wait video`; the video ends by itself at its end |

## State table

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | Video plays | `esphome.show_play_video` with an address at Home Assistant | `sensor.show_video_state` `idle` → `loading` → `playing` (a second call replaces a video that plays) | subscription to the sensor | 2026-11-10 (real Show, by hand) |
| 2 | Video over | its end | `playing` → `idle`, back to the home screen | the same | 2026-11-10 |
| 3 | Video refused | the switch `switch.show_video` off | `sensor.show_video_error` holds the reason | the same subscription | 2026-11-10 |
| 4 | Video stopped | `esphome.show_stop_video` | `playing` → `idle` at once | subscription to the sensor | 2026-11-10 (real Show) |
| 5 | Mira speaks while it plays (an announcement, a question) | the notifier's `start_conversation` | `playing` → `paused` while the satellite responds and listens, `paused` → `playing` when it is idle again; the words are heard | the sensor | 2026-11-10 (real Show) |
| 6 | A wake word while it plays | the user | the wake word is heard; `paused` while listening, processing and responding, `playing` again after | the sensor | 2026-11-10 (real Show) |
| 7 | The Echo's silent timer runs and ends | `esphome.<node>_timer_start_silent` (`duration`, `label`, `color`; turquoise `#40E0D0`), called with a response (`return_response`) | the answer at once (16 ms) with the timer's `id` (`local:…`); `sensor.<node>_timers` holds it, then empty; the device logs "a silent timer ran out"; no alarm | the call's answer; subscription to the sensor | 2026-11-10 (the Dot 2: "sah gut aus"). Called without a response the call hangs until Home Assistant's 30 s timeout (a bug below the firmware): always with a response |
| 8 | The Echo's timer stopped before its end | `esphome.<node>_timer_cancel` with the `id` from row 7 | `sensor.<node>_timers` empty; the device logs "timer canceled from home assistant" | the sensor | 2026-11-10 (cancelled after 5 s). With the label instead of the id it is refused ("cancel it there") |
| 9 | The satellite idle after the turn's answer | the answer said | `assist_satellite.<node>` `responding` → `idle` | subscription to the satellite's state | 2026-11-10 (the Dot 2: `processing` 16:46:24, `responding` 16:46:31, `idle` 16:46:41) |

Built only when every row is checked.

## Steps

1. **Check the state table** on the real Show and the Dot 2 (done 2026-11-10).
2. **The video** in the binary (German and English, `internal/waiting/videos`, the recipe in its `SOURCES.md`); Mira
   serves it at `/ui/videos/`, the integration passes `ui/videos/<name>.mp4` through its open page (an interface
   change: an older integration answers 404, row "refused") (built 2026-11-10).
3. **The Show's part** in the service: start, replay, stop, every failure row; Zigbee pairing and the Wi-Fi hand-over.
   WA-01 to WA-05 (built 2026-11-10).
4. **The Echo's ring**: rows 7 and 8 checked 2026-11-10; built with step 3.
5. **The run through Mira** at the real Show and the Dot 2: pairing opened by voice, the wait shown, stopped by the
   device or "abbrechen" (open).
