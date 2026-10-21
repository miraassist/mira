# Radio – Spec

Status 2026-10-18: decided and built in 0.6.37.

## Target

"Spiel SWR3" must work, like with Alexa. Home Assistant's Assist tools offer nothing for this: searching and playing exist there only for players that can "search media", and the ESPHome speaker of the satellites (such as TECHO5) cannot do that. Mira therefore searches itself in Home Assistant's "Radio Browser" source, which is always included, and plays the station with `media_player.play_media`.

## The tool

**`radio_play(station, room?, player?)`**

- **Room:** if not given, the room of the satellite being spoken to; otherwise the named room. If Mira knows no room (for example on Telegram without one being given), it says so and asks.
- **Player:** the `media_player` in the area that is exposed to Assist. If there is none, Mira says so honestly. If there are several, it asks which one (`player`); for that the tool names them with their name in Home Assistant and the entity ("Show Bad Speaker (media_player.show_bad_speaker)"), so that Mira does not guess the id.
- **Confirmation** names the speaker, not just the room ("Läuft: SWR3 auf Show Bad Speaker im Raum Bad"): Mira says where it is really playing, and a mix-up is noticed immediately.
- **Search:** WebSocket `media_source/search_media` on `media-source://radio_browser`, the most popular first. Only playable hits from the Radio Browser source count.
- **Selection:** If a hit matches the name (upper and lower case, spaces and punctuation do not matter, "SWR 3" = "SWR3"), it plays. If there is exactly one hit, it plays. Otherwise Mira names up to five names and asks back.
- **Playing:** `media_player.play_media` with the address and the type of the hit. Home Assistant fetches the stream and converts it.
- **Without Radio Browser** (integration missing or no connection) Mira says that searching does not work right now.

It asks nothing back: radio is not critical.

## What is not part of it

- **Stop, quieter, louder, pause:** Home Assistant's normal media tools. If there is a device that does not stop cleanly on pause, a stop tool is added.
- **Own favorite stations** with stream address and a Mira label for "plays music" per player do not exist; both can be added later.
- **Music** (artists, albums, playlists) is a matter for Music Assistant and still open.
