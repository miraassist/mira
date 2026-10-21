# Plan: further tools and skills

Status 2026-10-17: decided, not built. Same pattern as [Calendar](kalender.md): what HA can already do via Assist, Mira does not rebuild. How data is presented (levels, timeline): [Context](kontext.md), section 2. For the rest:
- **Reading** gets its own tool with a compact presentation: Mira decides how the model sees the data instead of passing on raw amounts.
- **Acting** becomes a [skill action](skill-aktionen.md), preferably without code.

Assist already offers today:
- lights, switching, covers, climate, fans, vacuums;
- media (pause, resume, volume, `search_media`, `play_media`);
- to-do (read, add, check off), broadcast, states (`GetLiveContext`), date and time;
- exposed scripts.

## Order

0. **Calendar and to-do**: see [kalender.md](kalender.md).
1. **History and logbook**: tool with view.
2. **Weather forecast**: tool with view.
3. **Music via Music Assistant**: skill.
4. **Camera**: onto the screen (skill) and understand (skill).
5. **Lists in attributes**: a general tool, if needed.

## 1. History and logbook

"Wann ging zuletzt die Haustür auf?" – "Wie lange lief heute die Waschmaschine?" – "War heute jemand im Keller?"

- Assist cannot do that. HA has logbook and history (REST `/api/logbook`, `/api/history/period`); raw, these are large amounts.
- **Tool `history`**:
  - Parameters: entity (or name), period (default: today), question type.
  - Question types: `last_change` (last change into a state), `changes` (changes in the period, at most N) and `duration` (how long in a state, e.g. "on").
- **Output:** compact, with time in the conversation's language, for instance "Haustür: zuletzt geöffnet heute 14:12, 3-mal seit 6 Uhr". Attributes are not included.
- **Visible** are only entities exposed to Assist.
- **For tasks** usable as view `view: history`.

## 2. Weather forecast

"Brauche ich morgen einen Schirm?" – "Wie wird das Wochenende?"

- `weather.get_forecasts` returns hourly or daily lists with many values.
- **Tool `weather_forecast`**: parameters period (today, tomorrow, the next days) and hourly or daily.
- **Output per day:** condition, low and high, rain probability and amount. Wind only if notable. Hourly only the hours in which something changes.
- **Which weather:** the exposed weather entity; with several, the house's (zone home) first.

## 3. Music via Music Assistant

"Spiel was von Norah Jones im Schlafzimmer" – "Spiel meine Playlist Abendessen" – "Spiel Radio Bob" – "Spiel das auch in der Küche"

- Assist's `play_media` can do little. Music Assistant knows artists, albums, playlists and radio and can group rooms.
- **Skill `musik`:**
  - Fields: what (artist, track, album, playlist, station), type (optional), room (default: the satellite of the request), shuffle (optional).
  - Finish: `music_assistant.play_media` on the room's media player.
  - Mira takes the room's speaker via the location (placeholder `area`).
- **Grouping and volume:** "Auch in der Küche" becomes a second finish (`media_player.join`). Volume and pause stay with Assist.
- **Only with Music Assistant**; without MA the skill does not exist.

## 4. Camera

**Onto the screen:** "Zeig mir die Haustür."
- **Skill `kamera_zeigen`:**
  - Field: camera.
  - Finish: the action of the satellite with a screen (Echo Show with TECHO5: `esphome.<gerät>_home_show_camera` with duration).
- As with the alarm clock, at satellites without a screen Mira says that it does not work there.

**Understand:** "Wer steht vor der Tür?" – "Ist das Garagentor zu?"
- **Skill `kamera_fragen`:** the finish is HA's `ai_task.generate_data` with the camera image as attachment and the question. Mira says the result.
- This needs a finish that returns an **answer** (action with `return_response`). That is a small extension of skill actions: with `response: true` the finish passes the answer on to the model.
- **Privacy:** The image goes to the model configured in `ai_task`, possibly in the cloud. The guide says so.

## 5. Lists in attributes

"Wann ist Strom heute am billigsten?" – "Wann fährt der nächste Bus?" – "Wo ist Diesel gerade günstig?"

- Many integrations pack a list into an attribute of a sensor: electricity prices per hour, departures, petrol stations, parcels. The state alone does not answer the question, the raw list is too much.
- **One general tool `list_attribute`** instead of one skill per integration:
  - Parameters: entity, attribute, fields, sorting (e.g. price ascending) and count.
  - Times in the conversation's language, only the named fields.
- **For tasks** usable as a view ("Sag Bescheid, wenn Strom heute unter 20 Cent fällt").

## Deliberately not

- **Alarm panel and locks** as a skill: that works via Assist, and it stays "only after yes".
- **Notifications**: Mira does that herself via the person's channels.
- **Very setup-specific devices** (car, wallbox …) as bundled skills: the operator writes those himself, following the pattern of skill actions. The guide will get an example for this.
