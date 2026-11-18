# Speaking while the answer is written – Spec

Status 2026-11-18: built (steps 1 to 7, in 0.15.2; step 8 built, not released; steps 10 to 12 built 2026-11-18, not released); all rows of the state table checked except row 15 at the real Show. Scenarios: [streaming.feature](streaming.feature).

## Goal

Today Mira hands Home Assistant the finished answer, and the satellite starts speaking only when the last word is
written. With a long answer (a story, the briefing, the news, a web search) or a slower local model, that is seconds of
silence. Home Assistant can take an answer piece by piece and have it spoken while it is still being written; the
satellite then starts after the first sentences.

Measured on the test HA (2026-11-03, Google AI as a streaming agent, `tts.phonon`, the Show with the TECHO5 firmware's
reply delivery "Early"): a story of 1,225 characters, the Show played 1.14 s before the answer was complete; the gain
grows with the length of the answer and the slowness of the model.

## The concept it hangs on

A turn as Home Assistant conducts it, with Home Assistant's own streaming of the answer into its chat log
(`async_add_delta_content_stream`); nothing is rebuilt. Mira only hands over the answer in pieces instead of at once.
Everything else of a turn (tools, hooks, threads, "Übrigens", keep listening) stays as it is; the decisions say what
happens where a turn today relies on the finished answer.

## What Home Assistant does (checked 2026-11-03, HA 2026.10.0)

- A conversation agent that sets `_attr_supports_streaming` and writes its answer with
  `chat_log.async_add_delta_content_stream(...)`.
- The pipeline starts the speech early (`intent-progress` with `tts_start_streaming`) once the answer has more than
  60 characters, or when text is followed by a tool call (`STREAM_RESPONSE_CHARS`, `assist_pipeline`). A shorter
  answer goes to the speech engine whole, as today, so it can be cached: short answers gain nothing.
- The speech engine must stream too: `tts.phonon` (Wyoming) does, both Phonon servers report
  `supports_synthesize_streaming`.
- The satellite must play at the offer: the TECHO5 firmware does with reply delivery **Early** (since 2026-11-03).

## State table

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | answer starts streaming | more than 60 characters written | `intent-progress` with `tts_start_streaming` | not at all: Home Assistant decides, Mira only writes | 2026-11-03 (pipeline debug: 0.64 s after the start of a 3.4 s answer) |
| 2 | speech before the end | text arrives at the speech engine | first audio bytes of the reply's stream | — | 2026-11-03 (first audio after 1.02 s, answer complete after 3.41 s) |
| 3 | the satellite speaks early | the offer of row 1 | `media_player.<speaker>` → `playing` | state subscription, where a flow waits on it (as today) | 2026-11-03 (playing 4 ms after the offer, 1.14 s before `intent-end`) |
| 4 | satellite state | the answer is complete (`tts-start`) | `assist_satellite` → `responding` | as today | 2026-11-03: `responding` comes at the end of the answer, not at the early start; waits on it (e.g. `afterAnswer`) are unchanged |
| 5 | speech ends | the last audio played | speaker → `idle`, satellite → `idle` or `listening` | as today | 2026-11-03 |
| 6 | text, then a tool call | a piece of text, then a delta that carries `tool_calls` (the model's own sentence before its tools, decision 7) | `tts_start_streaming` before 60 characters | not at all | 2026-11-03 in Home Assistant's code (`default_pipeline.py`: `delta_character_count > 0 and delta.get("tool_calls")`) and on the test HA with a test agent: `tts_start_streaming` at once, first audio after 0.22 s; at the Show 11:40, `show_speaker` playing 4 ms after the request, 5.0 s before the answer |
| 7 | interrupted by the wake word | wake word while the streamed answer plays | the speaker → `idle` at once, then a new run (`listening`) | as today | 2026-11-03 10:52 (story of 3,020 characters: playing 2.9 s before `intent-end`; wake word: speaker `idle` 53.85, `listening` 54.61; the next answer played) |
| 8 | "Stopp" during the answer | as row 7, then "Stopp" | as row 7; "Stopp" is then a turn of its own | as today | 2026-11-03 10:53 (speaker `idle` at the wake word; "Stopp" then went to the agent like any request) |
| 10 | the speech engine speaks a short piece | a sentence end at least 20 characters in: `.`, `!`, `?` or `…` followed by a space, or a line break | first audio | — | 2026-11-03 at the Phonon server and in its code (`xn-ptts`, `wyoming.rs`: `MIN_PIECE_CHARS = 20`, fixed, not a setting): "Einen Moment, ich schaue nach. " spoken after 0.03 s; "Einen Moment. " and "Einen Moment bitte. " not until more text came |
| 9 | wake word while the model still writes | wake word before `intent-end` of a streamed answer | Home Assistant ends the turn: the connection to Mira closes | the turn's context ends | 2026-11-03 14:07 at the Show with qwen: the story ended (`answer ended by the caller`, round 1), the model stopped, what was said was kept; "Stopp" after it answered in 0.5 s |


## Ways to interrupt and to cancel

- **Wake word or "Stopp"** while the answer is spoken: the satellite stops at once and a new run starts (rows 7 and 8);
  "Stopp" is then a request of its own, handled as today. If Mira's model is still writing when Home Assistant ends
  the turn (row 9): the model stops, no fixed sentence is added, and the transcript keeps what was said until then
  (decided 2026-11-03).
- **What the log says when the caller ends a turn** (decided 2026-11-08): one info line `answer ended by the caller`
  with the round, the model, how long the round ran (`ms`) and how much was spoken by then (`spoken_chars`, 0 when
  nothing was said yet). It is not an error: no `model round failed`, no `turn failed`, no `conversation failed`, and the run of
  `conversation.voice` ends `cancelled`, not `failed` (seen live 2026-11-08: a turn ended before its first piece was
  logged as an error).
- **How long the integration waits:** its timeout (the option, 15 s by default) holds until the first piece and
  between two pieces, not for the whole answer: a story takes longer than any timeout (seen 2026-11-03: a story cut
  off after 30 s; decided 2026-11-03).
- **The model fails after the first piece:** see decision 5.
- **The connection between the integration and the service breaks:** the integration ends the stream with the
  pieces so far and a short fixed sentence ("Da ist etwas schiefgegangen.").

## Interrupted while it is spoken (decided 2026-11-18)

**Goal.** For an answer interrupted while the satellite speaks it (wake word, "Stopp"), the model knows how far the user
heard it. Today the whole text stays: the model writes a story in seconds, the satellite needs a minute to
speak it, and Home Assistant's pipeline run ends long before the wake word (row 11). The next turn ("Wie ging es
weiter?") then assumes the user heard all of it.

**The concept it hangs on:** "what was spoken counts" (decisions 9 to 11, step 8) and the context block of a turn
("Aktueller Stand"), which already tells the model facts of the moment. New is a line there about the interruption,
estimated by the time the satellite's speaker played. The estimate is the one of Hugging Face's speech-to-speech (its
Realtime API, "Interrupted replies": playback time with a speaking rate measured per session); unlike it, the answer is
not cut: "Wie ging es weiter?" and "Lies weiter" need the rest, and a story, the news or a mail read out could not go
on from a cut text.

**Why a time, not a signal** (rules: signals, not timing): neither Home Assistant nor the satellite reports how far an
answer was played. The pipeline run is over before the speech (row 11), and a position reported by a satellite would be
a time too. The time only says how much; when it happened is a signal (the speaker's state, a new run).

**Decided (2026-11-18):**

| # | Question | Decision |
|---|---|---|
| 12 | What stays of an interrupted answer | The whole answer stays. The next turns of that conversation get a line in the context block: the answer was interrupted, heard up to "… <the last heard sentence>". Not in the answer itself: a marker there could be taken for one of the style tags the answers carry ("[ruhig]") and be imitated. Decided 2026-11-18 after first choosing Hugging Face's cut: the rest is needed to go on |
| 13 | The speaking rate | Measured per satellite on answers played to the end, as a fixed part plus a rate: the playing time is a straight line over the answer's length (row 12: about 2.4 s + 1 s per 18.6 characters at the Show; a rate alone would be 7 characters per second for a short answer and 17 for a story). A start value until there are measurements (decided 2026-11-18 after the measurements of row 12) |
| 14 | A satellite whose speaker Mira does not know | No note; one log line per satellite |

## Silence when asked for it (decided 2026-11-18)

**Goal.** "Stopp" right after an interruption, "sei still", "genug": the user wants Mira to be quiet. Today the model
answers anyway (row 13: "[zügig] Stopp.", 5.68 s at the Show), and an empty answer would become a fixed sentence
(decision 3). Any confirmation is already too much (the maintainer, 2026-11-18).

**The concept it hangs on:** a tool of Mira's that changes how the turn ends, as `keep_listening` does
([Satellite](satellite.md)): the model calls it, the engine sees the call. No phrase lists: the model decides from what
was said and from the note of an interruption (above).

**Decided (2026-11-18):**

| # | Question | Decision |
|---|---|---|
| 15 | How the model asks for silence | The tool `stay_silent`, offered in every turn, for a satellite only. Its description says when: the user only wants quiet ("Stopp", "sei still", "genug", "lass gut sein"), above all right after an interrupted answer; never when they want something known or done. Called alone, without text |
| 16 | What Mira sends | Nothing: the answer is empty. No fixed sentence (the exception to decision 3), no "Übrigens", no listening on. The turn ends right after the tool's round: no further model round |
| 17 | The interface | Unchanged. The integration already passes an empty answer through as empty speech (`conversation.py`: what was streamed, nothing), an older integration too; Home Assistant's pipeline then skips the speech (`default_pipeline.py`: "Skip TTS" when the speech is empty) |
| 18 | Text written before the call in the same round | Already spoken, so it stays spoken; the turn ends after the tool all the same |

**Errors:** a call outside a satellite (a chat, Telegram, a turn without a device) is no silence: the tool answers that
it works at a satellite only, and the turn goes on as usual; the log has the tool call as every call.

**What it costs:** one tool in every turn's list (its description, about 400 characters in the shared prefix); a
silent turn costs one model round less than one with an answer.

**State table, the rows of this section:**

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 14 | an empty answer through the pipeline | the agent's answer has no text | `intent-end` with empty speech, no `tts-start`, `run-end` | — | 2026-11-18 (`assist_pipeline/run` from intent to speech with the fake Show's device, "Sei still": the model called `stay_silent`, `intent-end` with speech `''`, no `tts-start`, `run-end`. Without a device the same sentence was answered aloud, as decided: silence is for a satellite only) |
| 15 | the satellite after an empty answer | as row 14 | satellite `processing` → `idle` (or `listening` if asked), no `responding`, speaker not `playing` | — | 2026-11-18 at the fake Show ("Sei still": `listening` and `processing` 19:52:15.348, `idle` 19:52:16.384, no `responding`, the speaker stayed `idle`); the real Show: still to be seen at the maintainer's run |

**What it does, step by step:**

1. **Playing:** when a voice turn at a satellite has spoken something, Mira subscribes to the satellite's speaker
   (`subscribe_entities` filtered to that one entity, the speaker found as for sounds: the media player of the
   satellite's device). The first state Home Assistant sends tells whether it plays already (the answer starts while
   it is written, row 3) and since when (`last_changed`); otherwise she waits for `playing`. Then she waits for the
   speaker to leave `playing`. The subscription ends there; a newer voice turn at the same satellite ends it too.
2. **Played to the end** (row 12): the playing time is at least 85 % of what the satellite's line gives for the
   answer's length. The pair (characters, seconds) goes into the satellite's line (fixed part and rate, decision 13).
   Characters are counted without the style tags in brackets ("[ruhig]"), which are not spoken.
3. **Interrupted** (rows 11 and 13): the playing time is shorter than the line's time by more than 1.5 s and more
   than 15 %. Between the two bounds it is neither: no note, no measurement. The heard part is the playing time minus
   the fixed part, times the rate, in characters, back to the end of the last complete sentence (`.`, `!`, `?`, `…`).
   A new run of the satellite is not waited for: whatever ended the playing, the user did not hear the rest
   (changed 2026-11-18 while building, confirmed by the maintainer the same day; the first version of this step asked for it).
4. **The note:** Mira keeps, per conversation, where its last answer was interrupted (the last heard sentence), in
   memory: a restart of the service loses it, as it loses the conversation's running turn. The context block of the
   next turn of that conversation says so once: "Deine letzte Antwort wurde beim Vorlesen unterbrochen. Gehört hat
   der Nutzer sie bis „…“; den Rest nicht." With no complete sentence heard: "… gleich am Anfang unterbrochen; der
   Nutzer hat keinen ganzen Satz davon gehört. Geht es weiter, fang von vorn an." (the last sentence added
   2026-11-18 after a run at the fake Show: without it the model went on from the end of the story) The answer itself stays whole. The speaker leaves `playing` at the
   wake word, before the next turn's speech is even heard, so the note is there when the next turn begins.
5. **The line** is kept per satellite in `kv` (`speech_line.<device id>`, described in [Database](../database.md)):
   the sums of a straight-line fit over the measured pairs (count, Σx, Σy, Σx², Σxy), the older pairs weighing less
   once there are more than 50. Until there are 3 pairs at least 100 characters apart, or when the fit gives no
   plausible line (a negative fixed part, or a rate outside 5 to 40 characters per second), the start value applies:
   2.4 s + 1 s per 18.6 characters (row 12).

**What it costs:** per voice turn that spoke something, one subscription to one entity, ended when the speaker stops
(at most as long as the answer plays, with a safety net below); no other call. One write of the satellite's line
in `kv` per answer played to the end. One line in the context block of the next turn after an interruption. The turn
itself waits for none of it.

**What the model is told:** the line in the context block (step 4), which says it itself: "Deine letzte Antwort wurde
beim Vorlesen unterbrochen. Gehört hat der Nutzer sie bis „…“; den Rest nicht. Geht es weiter, mach dort weiter." The
tool `stay_silent` (decision 15) names the interruption as the moment for silence.

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| The satellite's speaker is not known | no note (decision 14) | one log line per satellite, at its first turn |
| The subscription fails | no note, no measurement | a log line with the speaker and the error |
| No `playing` within 30 s after the turn, or still `playing` after twice the line's time plus 60 s (music on the speaker, a stuck state) | no note, no measurement; the subscription ends (the safety net: no signal comes) | a log line with the speaker, the conversation and how long it waited |
| The line in `kv` cannot be read or written | the start value applies; the pair is lost | a log line with the key and the error |
| The line in `kv` does not parse | the start value applies, the next pair starts it anew | a log line with the key and the start of the value |

**State table, the rows of this section:**

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 11 | interrupted while spoken, after the turn ended | wake word while the speaker plays a streamed answer whose pipeline run has ended | speaker → `idle`, then a new run (satellite `listening`); nothing in the ended run | state subscription of the speaker and the satellite | 2026-11-18 at the Show (story of 956 characters: speaker `playing` 18:18:52.739, `run-end` 18:18:56.496, speaker `idle` 18:19:08.446, `listening` 18:19:09.201; 15.7 s played) |
| 12 | played to the end | the last audio | speaker → `idle`, no new run before the playing time matches the length | as row 11 | 2026-11-18 at the Show, answers played to the end (characters / playing time): 28 / 3.89 s, 79 / 5.41 s, 19 / 2.49 s, 667 / 38.27 s (a story; satellite `idle` 1 s before the speaker). The playing time is a fixed part plus the length: about 2.4 s + 1 s per 18.6 characters (from the shortest and the longest); the short ones lie up to 1.2 s off. A rate alone (characters per second of playing) would be 7 for a short answer and 17 for a story |
| 13 | "Stopp" while spoken | as row 8 | as row 11: the wake word ends the playing, "Stopp" is a turn of its own | as row 11 | 2026-11-18 at the Show (story of 663 characters: speaker `playing` 19:18:22.042, `idle` 19:18:28.886 at the wake word, `listening` 19:18:29.641; 6.84 s played, about 83 characters by row 12. "Stopp" then went to Mira as a request, answered "[zügig] Stopp." and played 5.68 s) |

## Decided (2026-11-03)

| # | Question | Decision |
|---|---|---|
| 1 | Which answers are streamed | Every voice answer, with or without a tool call; Home Assistant's rule (more than 60 characters, or text and then a tool call) decides when the speech starts. Possible because the check of unsupported answers goes (decision 8) |
| 2 | Thinking blocks (`<think>…</think>`) | Cut out of the running text, never streamed (today `stripThinking` cuts them from the finished text) |
| 3 | An empty answer | The fixed sentence as the only piece, as today |
| 4 | "Übrigens" | Appended as the last piece, once the model is done; the rules for when stay |
| 5 | The model fails after the first piece | No fallback to another model: the answer would be pieced together from two. A short fixed sentence ends it. Before the first piece the fallback holds as today |
| 6 | Which channels | Only voice; Telegram and background runs stay whole |
| 7 | Before a longer task | The model may say a short sentence itself before its tools (the prompt allows it before several tools, a search on the web, in the calendar or the mails; not before a simple switching). Mira adds no sentence of her own: helpers that take long (agents, such as the skill `nachdenken`'s, until 2026-11-15 `delegate`) announce themselves, and what else is slow is a search on the web, which the model may announce (decided 2026-11-03, after a fixed sentence was tried) |
| 8 | The check of unsupported answers | Removed, for every channel (step 2). The rest risk that a model claims something it did not do or look up is accepted: it is inherent in a model answering; the prompt keeps asking it not to. Sharpened 2026-11-09: a voice turn on the live house confirmed an item on a shopping list that did not exist yet, without calling any tool (no tool call in the turn's log). The prompt's rule now says that something counts as done only when a tool did it for this request, and that a missing target (no list of that name) is said instead of confirmed; the eval scenario `liste-fehlt` checks it |
| 9 | Text next to a tool call, said again (2026-11-05) | Some models (DeepSeek) write text and a tool call in one round, then write the same again after the tool's result; with decision 7 both were spoken (a quiz asked each question twice; Gemini writes no text next to its tools: 0 of 136 rounds in the test history). The early start stays. When a round with a tool call had text and it was handed on, Mira adds to that round's last tool result: "(Schon gesprochen: „…“. Wiederhole das nicht; sag nur, was danach noch fehlt, oder nichts.)" A turn that is not streamed gets no note: nothing was spoken. Tried at the Show with DeepSeek: a quiz without a repeated question |
| 10 | What a turn's answer is (2026-11-05) | All the text it spoke, from every round. A round with text and a tool is an answer: when the last round writes nothing, Mira adds no "Ich bin damit nicht ganz fertig geworden." That sentence comes only when nothing at all was said ([Silent fallbacks](fallbacks.md), FB-05) |
| 11 | Listening after the answer (2026-11-05) | Decided on the whole spoken text of the turn, not the last round's: the question may stand in a round with a tool. The satellite listens when the last sentence or the one before it ends with "?" (a model may add a filler: "Wie viele Beine hat eine Spinne? Ich warte auf deine Antwort."). The worst case is a nuisance: the satellite listens once for nothing |

Keep listening after a question is decided at the end of the answer, as today; Home Assistant reads the flag at the
end.

## The model's own sentence before its tools

- **The prompt** allows the model a short sentence before a longer task ("Ich schaue im Kalender nach.") and not
  before a simple switching or lookup.
- **Heard at once:** Home Assistant starts the speech of a text under 60 characters only when a tool call follows it
  (row 6). Mira's tool calls are written into the chat log as external tool calls, so the sentence is spoken while the
  tool runs. They also show in Home Assistant's debug view, each with the first 300 characters of its result (only
  there; the model gets the whole result as before).
- **The speech engine** speaks a piece only once a sentence ends at least 20 characters in (row 10): a sentence of
  the model shorter than that waits for more text.

## What we do not do, and why

- **A fixed sentence of Mira's after a time** ("Einen kleinen Moment, bitte." after 2 or 3 s of silence, built and
  heard on 2026-11-03). With qwen the model needs about 3 to 5 s before it calls a tool (it reads and decides; it says
  nothing then), and the tools of the house take milliseconds: the sentence came in almost every turn and right
  before the answer. A time says that one has waited, not that one will wait.
- **A fixed sentence before slow tools** (those that go to the web or run a model). Helpers that run a model
  announce themselves when they take long ("ich melde mich"), so the sentence would come twice; what remains is a
  search on the web, which the model may announce itself.
- **Spaces after a sentence** up to 60 characters to make Home Assistant speak it: it rests on every speech engine
  dropping the spaces as Phonon does; the tool call after it does the same.

- **Keep the check of unsupported answers.** It looks for words in the model's answer (German and English lists:
  "eingeschaltet", "gespeichert", "done"; a number with a unit; "ist an/aus/offen") when no tool was called, asks the
  model twice more and in the end replaces the answer ("Das habe ich nicht ausgeführt."). It came in on
  2026-10-14/15 when the local model made up readings, and was never specified. Tried on 2026-11-03, it also hits
  stories and knowledge: "Jeden Abend hat er die große Lampe eingeschaltet", "das Tor ist offen geblieben", "zu
  etwa 60 % aus Wasser", "Wasser kocht bei 100 Grad", "a dragon opened the gate". Such answers are asked for again,
  a tool call is forced, and a story can end as "Das habe ich nicht ausgeführt." The lists are language-bound
  phrases, which Mira does not want, and the check needs the finished answer, so it cannot go with streaming.
- **Check only the names of the house.** A story names the front door and the garage door too; whether a sentence
  speaks about this house is a matter of meaning, not of words.
- **Hold back the first characters and check them** (e.g. 100 or 200). It keeps the word lists, and costs time before
  the first sound: measured with qwen 2026-11-03, 60 characters after 1.16 s (where Home Assistant would start), 100
  after 1.48 s, 200 after 2.40 s.
- **A model that judges the answer.** One more model call in every turn without a tool, and it, too, needs the start
  of the answer before anything may be streamed.
- **Spaces after the sentence up to 60 characters** instead of the tool-call entry. It works with Phonon (measured
  the same), but rests on every speech engine dropping the spaces as Phonon does.
- **A fixed sentence before every tool call.** It would come before every switching ("Einen Moment, ich schaue nach.
  Das Küchenlicht ist an."), which does not sound natural.
- **A fallback model after the first piece** (decision 5), **streaming Telegram** (Later).

## The interface between the integration and the service

- The integration asks `POST /v1/conversation` with the new optional field `stream: true`.
- A service that knows it answers with one JSON line per piece (`{"delta": "…"}`) and a last line with what the
  whole answer carries today (`conversation_id`, `continue_conversation`, …). A service that does not know it ignores
  the field and answers as today; the integration takes both.
- An integration that does not send the field gets the answer whole, as today.
- Both directions are tested (the interface rule).

## What it costs

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| the model call | `stream: true` to the model's server; pieces as they come | no more tokens, no more calls | the turn is the stream |
| service → integration | one HTTP response held open, one line per piece | a few hundred small writes for a long answer | — |
| integration → Home Assistant | each piece into the chat log | as above | — |
| the tool calls in the chat log | one line each as it starts and as it ends | none | no |

Nothing grows beyond the answer itself; no subscription, no polling.

## Steps

1. **Check rows 6 to 8** on the test HA: done 2026-11-03 (rows 7 and 8 at the Show, row 6 in Home Assistant's
   code); row 9 follows with step 4.
2. **Remove the check of unsupported answers** (decision 8): the word lists, the extra rounds and the replacing
   sentences; `docs/system.md` says so.
3. **The model client streams** (`internal/llm`): server-sent events, tool calls assembled from pieces; llama.cpp, LM
   Studio and Gemini over the same OpenAI-compatible interface.
4. **The engine streams** (`internal/conversation`, through the agent runtime of `conversation.voice`), by decisions
   1 to 6; row 9 is checked with a slow model.
5. **Service → integration → Home Assistant**: the field `stream`, the lines, the integration's
   `async_add_delta_content_stream`; both directions of the interface.
6. **Before a longer task** (decision 7): the prompt's permission and Mira's tool calls in the chat log.
7. **Guide:** for answers that start early, the speech engine must stream (Phonon does) and the satellite must play at
   the offer (TECHO5: reply delivery "Early").
8. **What was spoken counts** (decisions 9 to 11): the note in the tool result, the answer as all spoken text, the
   listening on it. Scenarios ST-14 to ST-16.
9. **A turn ended by the caller is logged as such** (built 2026-11-08): the line with time and spoken characters,
   also before the first piece; the run `cancelled`. ST-17, ST-18.
10. **Interrupted while it is spoken** (decisions 12 to 14, 2026-11-18): rows 12 and 13 checked first, then the
    playing time per answer, the rate per satellite, the note in the context block. ST-19 to ST-23. Built
    2026-11-18, not released (`internal/heard`); run at the fake Show: a story of 730 characters cut after 14.7 s,
    the note named its first sentence, and "Wie ging es weiter?" went on with the second.
11. **Silence when asked for it** (decisions 15 to 18, 2026-11-18): the tool `stay_silent`, the engine's empty end.
    ST-24 and ST-25. Built 2026-11-18, not released; "Stopp" after an interrupted story and "Sei still" at the fake
    Show ended without a word.
12. **An empty answer through the integration and the pipeline** (decision 17): nothing to build, checked in pytest
    (ST-26: the integration passes it on as empty speech) and on the test HA (rows 14 and 15).

## Later

- Telegram: sending a long answer in parts as it is written.
