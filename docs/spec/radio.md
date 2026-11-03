# Radio – Spec

Status 2026-11-01: decided and built in 0.6.37; step 2 (the station's name on the device) released in 0.15.0.
Scenarios of step 2: [radio.feature](radio.feature).

## Target

"Spiel SWR3" must work, like with Alexa. Home Assistant's Assist tools offer nothing for this: searching and playing exist there only for players that can "search media", and the ESPHome speaker of the satellites (such as TECHO5) cannot do that. Mira therefore searches itself in Home Assistant's "Radio Browser" source, which is always included, and plays the station with `media_player.play_media`.

## The tool

**`radio_play(station, room?, player?)`**

- **Room:** if not given, the room of the satellite being spoken to; otherwise the named room. If Mira knows no room (for example on Telegram without one being given), it says so and asks.
- **Player:** the `media_player` in the area that is exposed to Assist. If there is none, Mira says so honestly. If there are several and none is named, the satellite's own speaker plays when the room is the satellite's (a Music Assistant player for the same device would otherwise make every request a question; decided 2026-10-25). Otherwise it asks which one (`player`); for that the tool names them with their name in Home Assistant and the entity ("Show Bad Speaker (media_player.show_bad_speaker)"), so that Mira does not guess the id.
- **Confirmation** in the model's own words: on the speaker of the satellite being spoken to briefly ("SWR3 läuft."), since one hears where it plays; anywhere else with the room ("SWR1 läuft im Bad"), never with the device's name or the word "Lautsprecher", which nobody says (decided 2026-11-03, after "SWR3 läuft auf deinem Lautsprecher im Bad" every morning; before, the speaker was named so that a mix-up would be noticed).
- **Search:** WebSocket `media_source/search_media` on `media-source://radio_browser`, the most popular first. Only playable hits from the Radio Browser source count.
- **Selection:** If a hit matches the name (upper and lower case, spaces and punctuation do not matter, "SWR 3" = "SWR3"), it plays. If there is exactly one hit, it plays. Otherwise Mira names up to five names and asks back.
- **Playing:** `media_player.play_media` with the address and the type of the hit. Home Assistant fetches the stream and converts it.
- **The station's name on the device** (decided 2026-11-01): Home Assistant sends the stream through its ffmpeg
  proxy, which keeps neither the station's address nor its name, so a TECHO5 device shows "Radio" without a logo.
  Its firmware (from v1.6.16) has the action `esphome.<node>_media_station` with `name`: it names the next
  `play_media` within a minute as that station, looks it up on Radio Browser and shows its name, logo and song.
  Right before `play_media`, Mira calls it with the hit's title (the name it has on Radio Browser), if the
  player's device has the action; the node is the device's name as a slug, as for the answering machine's
  `media_title`. A device without it is played as before. If the call fails, the station still plays (the
  name is only for the screen) and the log says why.
- **Without Radio Browser** (integration missing or no connection) Mira says that searching does not work right now.

It asks nothing back: radio is not critical.

## Costs

Per started station: one `search_media` call, one `play_media` call, and on a device with the action one
`esphome.<node>_media_station` call before it. Whether the device has it comes from the list of Home Assistant's
actions, read once and cached until Home Assistant registers or removes an action. The turn waits for all of
them; nothing runs after it.

## Steps

1. **The tool** (0.6.37), before the scenario map.
2. **The station's name on the device.** Scenarios RA-01 and RA-02.
3. **A short confirmation on the satellite's own speaker.** Scenario RA-03.

## What is not part of it

- **Stop, quieter, louder, pause:** Home Assistant's normal media tools. If there is a device that does not stop cleanly on pause, a stop tool is added.
- **Own favorite stations** with stream address and a Mira label for "plays music" per player do not exist; both can be added later.
- **Music** (artists, albums, playlists) is a matter for Music Assistant and still open.
