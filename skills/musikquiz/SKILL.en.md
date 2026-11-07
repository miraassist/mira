---
description: "A music quiz like \"Hitster\": Mira plays short bits of songs, the players guess the artist, title, year or decade (\"Let's play a music quiz\", \"Music quiz with 80s hits\"); then it goes on with the answer, \"again\", \"next\" or \"What's the score?\". Only at a satellite with a speaker."
---
# Music quiz

You play a music quiz: you play 30 seconds of each song, the players guess.
The quiz is a thread with a plan: the plan holds the songs with their answers
and the points. Never give away beforehand which song plays.

Always call tools that do not wait for each other **together in one step**:
every extra step keeps the players waiting.

1. **Clarify briefly first**, with one question, only what has not been said: the
   genre or decade (or "mixed"), what is guessed (artist, title, year, decade or
   everything) and who is playing (names; alone none). As many songs as wished;
   without a wish ten.
2. **Choose the songs:** well-known songs that fit the choice, by different
   artists; no live, remix or cover versions. The year is the song's first
   release, not a reissue.
3. **Search and check the songs**, all at once:
   - In one step, for each song `web_page` on
     `https://api.deezer.com/search?q=ARTIST%20TITLE&order=RANKING&limit=2`
     (spaces as %20). Take the hit whose `artist.name` and `title` fit and whose
     title has no addition ("Live", "Acoustic", "Remix", a year). From it you
     need `id`, `preview`, `isrc` and `album.cover_xl`. No fitting hit or no
     `preview`: take another song instead.
   - In the next step, for each song `web_page` on
     `https://musicbrainz.org/ws/2/isrc/ISRC?fmt=json`: the earliest
     `first-release-date` of the `recordings`. If it is more than a year off
     yours, MusicBrainz's counts; otherwise yours stays. If the request fails,
     yours stays.
4. **Write the plan** with `thread_plan` (action `write`), in the same step as
   the first song (5):
   - the heading "# Music quiz: 80s · year" (choice · what is guessed),
     below it "Players: Anna" (several: "Players: Anna, Ben");
   - each song a box with its answer, Deezer id and cover:
     `- [ ] a-ha – Take on Me (1985) · id 664107 · Cover https://…`.
   There is no line with points: the points stand at each song as soon as it is
   rated (6), and are added up. With several players they take turns: write
   the name before the song ("Anna: …").
   **Who answers:** with only one player, every answer is theirs, even if you do
   not recognise the voice or hear another name. With several, the answer
   belongs to the one whose turn it is by the plan.
5. **Play a song:** say only "Song 3 of 10:" (with several, with the name:
   "Anna, song 3 of 10:") and call `clip_play` with its `preview`, title
   "Music quiz", subtitle "Song 3 of 10". The song plays after your answer. The
   `preview` address holds only for a short while: for every song but the first
   you fetch it fresh with `web_page` on `https://api.deezer.com/track/ID`.
6. **Rate the answer:** which song is playing stands in the plan under "Now",
   not in your conversation history: several are guessing, and what others
   answered you do not see in the history. Always rate against the song that is
   "Now" in the plan. If the answer is right (also in substance; a small slip in
   the name does not count), say "Right!"; otherwise the right answer.
   Points: artist, title, year (exact) or decade one each; with "everything" one
   each for artist, title and year (one year off still counts).
   Then in **one** step together:
   - `display_card` with the song: title "Song title · Artist" (short), line the
     year, image the cover from the plan, 20 seconds;
   - tick the song with its result: `thread_plan` action `check`, step the
     song's number, note "1 point" or "0 points" (with "everything" up to 3).
     Do not rewrite the plan for it;
   - `web_page` on `https://api.deezer.com/track/ID` for the next song.
   Then play the next song (5).
7. **What else comes:**
   - "next" or "I don't know": say the answer, no point, otherwise as 6.
   - "again": the same song once more, with a fresh address (5), without rating.
   - "What's the score?": the points, added up from the rated songs in the plan
     (with several, per player).
   - "Stop" or "Pause": only stop the music (Home Assistant's pause); the game
     stays and goes on with the answer or "next". Only "Cancel" or "Stop
     playing" ends the game with `thread_cancel`.
   - If the thread says the last clip could not be played, say so and ask
     whether you should try again.
8. **End:** rate the answer to the last song **exactly as 6**: card and the last
   song ticked with its result (`check` with note), only without a next song.
   Do not stop then, but close the game **in the same answer**:
   - add up the points from the plan (with several, per player);
   - a sound with `sound_play`, by the share of the possible points (with
     several, all together): all `fanfare_big`, from 80 % `fanfare`, below 50 %
     `too_bad`, in between none;
   - then say "Right!" or the answer, the points, with several who won, and ask
     whether another round is wanted (then a new plan with new songs). If nobody
     wants to play on, end the thread with `thread_cancel`.
