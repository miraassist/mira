# Context – Spec

Status 2026-10-20: built in 0.6.9; device part of HA's prompt (section 1.1) in 0.6.48. Applies to all channels. See also [Threads](vorgaenge.md) and [Channels](kanaele.md).

The **context** is everything the model sees with a request. Mira does two things with it:
- **Composition**: what goes into the context window in which order;
- **Compaction**: what drops out when the history gets too long.

## 1. Composition: what the model sees

In this order (`internal/prompt`, `prompt.Build`):

| # | Part | Content | changes when | Layer |
|---|---|---|---|---|
| 1 | **System message** | Role and rules; house structure (HA's Assist prompt, otherwise Mira's own block: areas, devices, entities without states); house profile (condensed house memory) | Rules: never; structure: device/area new or renamed; profile: nightly condensation | 1, 4, 5 |
| 2 | **Tools** (own field of the request) | HA's tools via MCP, Mira's own (memory, messages, reminders, tasks, skills …), further MCP servers | Tool list changes | 3 |
| 3 | **Example conversation** | a fixed short exchange that shows tone and tool use | never | – |
| 4 | **Channel style** | how to reply on this channel (voice: short, without Markdown; Telegram: longer, lists, links), as a message with a fixed confirmation | never (fixed per channel) | 2 |
| 5 | **History** | the previous turns of this session, verbatim, with tool calls and results (sections 4, 5) | every turn appends; compaction cuts | 6 |
| 6 | **Current turn** | time; channel and room; the lines that HA's Assist prompt appends for this device (1.1); who is speaking and their personal profile; matching memory facts; state of the running thread; context of a message just announced; open confirmation; the text | every request | 7 |

### 1.1 HA's Assist prompt: the common prefix in part 1, the device in the turn

HA's Assist prompt (part 1, house structure) is not the same for every device. Home Assistant appends lines **at the end** that depend on the asking device: which area it is in ("You are in area Wohnzimmer and all generic commands … should target this area", otherwise the request to name an area) and whether it can start timers ("This device is not able to start timers", if not). HA puts this at the end because its own cache likewise depends on the same beginning. Measured (HA 2026.10): around 3.5 KB of prompt, of which only the last one or two lines depend on the device.

Mira therefore fetches the prompt via MCP (`prompts/get` "Assist") **not only without a device**, otherwise it would get the "cannot do timers" version and the model would refuse timers even where the device can do them (this happened). Instead:

1. It fetches it **once without a device** and **once as each satellite** (the id goes into `_meta` with `io.home-assistant/device_id`, as with the tools).
2. **The common beginning of all versions**, up to the first difference and cut at a line boundary, is **part 1** (the prompt cache `internal/ha/assistprompt`). It is the same for all rooms and channels, so the cache behind it (tools, examples, history) is preserved.
3. **The rest of the asking satellite's version** is its piece and goes into the **turn** (part 6), under the heading "Hinweise von Home Assistant für dieses Gerät:", after the line with the channel. A channel without a device (Telegram) gets the piece of the version without a device. There "cannot do timers" is also correct. Background jobs (assessing, skill schedules) get no hints.
4. **Detection is by comparison**, not by knowledge of the text: Mira knows nothing about room or timer sentences, it takes what differs between the versions. If HA changes the wording or another device-dependent line is added, Mira follows without a change.

Properties:
- **Part 1 only changes when the house changes** (new or renamed entity), not when a device is added or the pieces change. Only a change of part 1 triggers the warm-up (`prefix_hash` changes then).
- **Assumption "at the end":** Should the difference come further forward in future, the common beginning gets shorter and more moves into the turn. That stays correct, only the cache gain shrinks. If the common part is below 90 % of the length, a warning appears in the log.
- **Failure:** If a satellite does not answer, it keeps its old piece; if the version without a device does not answer, everything stays as it was.
- **Without satellites** (none known) the whole prompt is part 1, as before.
- **If Mira does not have HA's prompt** (it then uses its own structure block), there are also no hints.
- **Cost:** one or two lines in the turn per voice request (estimated about 40 tokens) and one extra fetch per satellite when the prompt is fetched anew (every few minutes and on changes).

The same rule applies to the **tools**: the list is fetched once without a device and once as each satellite and merged, and every call carries the satellite's id, so that HA serves it like its own agents (timers, room for commands without a room; [Scheduled actions](geplante-aktionen.md)). **What Home Assistant offers we do not rebuild, and nothing is lost because the context does not come along.**

What is not in the context: **states of devices.** The model queries them when needed via tools; the rules require that.

## 2. Rendering of entities

Status 2026-10-17: built are stages A, B and D, the basic form "value" and the timeline with `calendar_events` and `view: upcoming` (0.6.19). `history`, `weather_forecast` and `list_attribute` follow.

An entity reaches the model at different times by different paths, each in its own rendering. For each path it says here whether HA delivers it (Mira does not change it) or Mira builds it (Mira determines what the model sees).

### 2.1 Four stages

| Stage | When | What the model sees | Who renders |
|---|---|---|---|
| **A. Structure** | always, in the prefix (part 1, cache) | that the entity exists: name, area, type. **Never states**: they would break the cache on every change. | HA (Assist prompt) or, without it, Mira |
| **B. Turn** | every request (part 6) | only what belongs to the place and the person: area of the satellite, waiting messages, running thread. No states of devices. | Mira |
| **C. Query** | when the model calls a tool | **now**: `GetLiveContext` from HA, state and selected attributes. **Over time**: Mira's tools for appointments, history, forecast, list attributes (2.3). | HA or Mira |
| **D. Background** | assessment of a task (`mira.assess`, `mira.decide`) | a condensed view for exactly one question (`data[].view`, see [Tasks](aufgaben.md)) | Mira |

Tool results of stage C stay in the history (part 5) and count for compaction (section 5). They should therefore be concise.

### 2.2 Two basic forms

**Value**: a state with unit and "since when", condensed over a period if applicable (sum, mean, same hour as usual). These are the current views of the tasks (`state`, `same_hour`, `day`).

**Timeline**: a list of entries with a point in time and fields, for example appointments, changes in the logbook, hours of a forecast, electricity prices per hour, departures. These cases are built the same way, so there is **one** rendering with the same parameters:

| Parameter | Meaning | Example |
|---|---|---|
| Window | from/to; default per type | Calendar: today until in 7 days; history: today |
| Selection | search text, filter on state or field | Title contains "Arzt"; only "auf" |
| Sorting | by time (default) or by value | electricity price ascending |
| Limit | at most N entries, then "und N weitere" | 40 appointments, 10 changes |
| Fields | short (default) or detailed; text shortened, links removed | location shortened, description only detailed |
| Grouping | by day, with weekday | "Mo 20.10." |
| Time format | in the language of the conversation ([Language](sprache.md)), relative where it helps | "heute 14:12", "morgen" |
| Keys | short keys (`t1`, `t2` …) instead of HA IDs, where one can act on an entry | cancel appointment |

The code keeps the HA IDs for the keys; the model does not see them. Only what really differs remains type-specific (2.4).

### 2.3 Timeline tools

All with the same parameters for window, selection, sorting, limit and fields:

| Tool | Source in HA | Spec |
|---|---|---|
| `calendar_events` | REST `/api/calendars/<entität>` (with appointment ID) | [Calendar](kalender.md) |
| `history` | Logbook and history | [Plan](skills-plan.md) |
| `weather_forecast` | `weather.get_forecasts` | [Plan](skills-plan.md) |
| `list_attribute` | Attribute of an entity that is a list | [Plan](skills-plan.md) |

The task views use the same rendering: `view: upcoming` (calendar), `view: history`, `view: list`.

### 2.4 Type × stage

| Type | A. Structure | C. now | C. over time | D. View | Type-specific |
|---|---|---|---|---|---|
| Calendar | Name | next appointment (HA) | `calendar_events` | `upcoming` | all-day, series, read-only |
| To-do | Name | number open (HA) | `todo__get_items` (HA) | – | due date |
| Weather | Name | now (HA) | `weather_forecast` | – | weather conditions, wind only when notable |
| Sensor with number | Name, unit | Value (HA) | `history` | `state`, `same_hour`, `day` | Meters: consumption instead of reading |
| Sensor with list attribute | Name | State (HA) | `list_attribute` | `list` | which fields |
| Switch, binary sensor, light | Name | State (HA) | `history` | `state`, `history` | state names ("offen" instead of `on`) |
| Person | Name | home/away (HA) | `history` | `state` | – |

### 2.5 `GetLiveContext` stays, with a trigger

HA's `GetLiveContext` is already compact per entity: name, type, state, area and selected attributes, without raw lists. Measured on 2026-10-17 in a real house: 148 entities, about 18,000 characters without a filter, largest entry 255 characters. There is therefore no reason to replace it.

- **What is missing**: "since when". `history` covers that.
- **Trigger for a wrapper**: Mira only puts a thin wrapper around it once the log shows that calls without a filter (the whole house) noticeably bloat the history. The wrapper would then, on a call without a filter, summarize by area or require a filter.

## 3. Why this order: cache

Models (local as well as in the cloud) do not recompute an **identical beginning**, but take it from the cache. Therefore:
- **stable before volatile**: parts 1–4 change rarely, the turn on every request;
- parts 1–4 are **byte-identical** for identical inputs: no timestamps, IDs, random values or unsorted lists;
- **the common prefix (1–3) is the same for all channels**, only the channel style differs; that way voice, Telegram and background jobs share the cache;
- **volatile things only in the turn**, including the personal profile (otherwise the common prefix would break per person);
- **what depends on the device also only in the turn**: the lines that HA's Assist prompt appends for the room and the timers of the asking satellite (1.1); part 1 is only the part that all versions have in common;
- the history is **only appended**, never changed in the middle; nothing is inserted between history and turn;
- a fingerprint of parts 1–5 (`prefix_hash`) is in every "turn done" log entry: if it changes unexpectedly, the cache was broken accidentally.

## 4. History and thread

- **The thread carries the concern**, the same for voice and chat: its state appears fresh in part 6 on every turn. What a concern needs over many turns does not depend on the history.
- **The history provides the back-reference**: "und das andere Licht?", "wie alt ist der?". The last turns suffice for that; nobody simply refers to an ancient message.
- **House structure and memory** are added up to date on every turn (parts 1 and 6), regardless of how old the history is.

## 5. Compaction

What the model sees of the conversation: **the running thread + the last turns.** Older things drop out. No summarizing by a model, a simple cut:

- **Counting is in whole turns**, not in messages. A turn is the user's message with everything that follows from it (tool calls, results, reply). Cuts are only made between turns, never between a tool call and its result.
- **In jumps, not sliding**: when the history reaches `max_turns`, it is cut to `keep_turns`. In between its beginning stays the same, and the cache takes effect. (Sliding would drop something at the front on every turn and break the cache for the whole history.)
- **At the latest at 90 % of the context window** of the respective model it is cut as well, even if `max_turns` is not yet reached (large tool results, small local model).
- **The running thread always stays whole**, even if it began before the cut.
- **Every cut appears in the log** as `history compacted`, because it deliberately breaks the cache and things said drop out: session, reason (`max_turns` or `context` with `prompt_tokens`, `limit`, `context_tokens`), turns before and after, removed messages, from when removed and from when kept, and whether more was kept for a running thread (`kept_for_thread`). If it cannot be cut further because only `keep_turns` are left, that appears as a warning in the log.
- **Session duration**: voice 5 minutes (a new sentence a quarter of an hour later almost never connects); Telegram **without a time limit**, the cut limits the history itself. `/neu` still starts a new conversation.

## 6. Configurable values

| Value | Default | Meaning |
|---|---|---|
| `sessions.max_turns` | 50 | from here on it is cut |
| `sessions.keep_turns` | 30 | this many turns remain after the cut |
| `llm.compact_at_fraction` (per model) | 0.9 | fraction of the context window (`context_tokens`) from which it is cut |
| `sessions.voice_ttl` | 5m | session duration voice |
| `sessions.telegram_ttl` | off | session duration Telegram (off = no time limit) |

## 7. Deliberately not

- **No summary by a model**: costs a call, is not predictable, and what should remain is in memory or in the result of a thread.
- **Old turns stay verbatim**, including their part 6 from back then (time, memory hits); they are not slimmed down.

## 8. Observed and ideas (not decided)

Status 2026-10-18, measured with Gemini Flash via the OpenAI interface.

**Measured**
- **Growth:** The first request of a conversation had about 8,900 tokens, the 27th about 15,800: on average around 260 tokens per turn, for turns with calendar or thread up to 1,400. Over 90 % of the request is the unchanging prefix, of which about 72 % are the 51 tool definitions.
- **Time:** Gemini needs about 0.3 to 0.4 seconds per 10,000 additional tokens. The growth does not make Mira noticeably slower with Gemini. Measured with Mira's real request, padded with additional history, without cache, four calls per size:

  | History (pairs of question and answer) | Tokens | Response time (median) | Individual values |
  |---|---|---|---|
  | none | 8,959 | 2.0 s | 1.7 · 1.8 · 2.2 · 2.8 |
  | 30 | 15,680 | 2.05 s | 1.8 · 2.0 · 2.1 · 2.2 |
  | 60 | 22,422 | 2.25 s | 1.8 · 2.2 · 2.3 · 2.6 |
  | 100 | 31,382 | 2.45 s | 2.3 · 2.4 · 2.5 · 2.6 |

  A single round thus took under 3 seconds. A conversation with a tool has two rounds. The outlier of 13 seconds when deleting an appointment on 2026-10-18 is therefore not explained; the log (`model round`, `tool done`) should show whether it recurs and where it sits.
- **Cache:** Gemini reports cache hits only for a completely identical request. When only the end was changed, there was no hit in four attempts. Local servers cache the unchanging prefix.
- **Log:** `model round`, `tool done`, `context built`, `turn done` (docs/system.md) show time and size per round.

**Cost:** It depends on the input tokens, and those are limited by `context_tokens` (with `compact_at_fraction`). There is no second cost limit. For a cloud model, `context_tokens` is therefore not a window width but the limit of the request. In the test configuration it is set to 24,000 for Gemini (cutting from about 21,600 tokens).

**Ideas**
1. **Check the default `context_tokens` for cloud models.** The setup form does not set the value, the default 16,384 applies. The unchanging prefix alone has 9,000 to 11,000 tokens, the history reaches the 90 % limit (14,700) after about 20 turns. Then Mira wants to cut, but has fewer than `keep_turns` turns, and `history cannot be cut further` appears in the log on every turn. A suitable default for cloud models would be worth considering.
2. **Shorten old tool results in the history** (calendar lists, states), replaced by a short text after the last few turns. That would keep the history small without shortening the conversation. It contradicts the sentence in section 7 ("Old turns stay verbatim") and would have to be weighed against it.
3. **Small context for the reply after a confirmation.** If a tool ends the request (`thread_confirm`, `thread_cancel`, `thread_undo`), the second round could get only personality, the turn and the result of the call, without tools, house and history. The reply would go into the log as before. Open: only for cloud models, because a deviating request displaces the cache on a local server. On errors and with several calls in a row it stays with the full context. The result texts of the finishers would have to be understandable on their own. The gain lies more in cost than in time. Not built for now.
4. **Cut down tools.** 51 definitions are 72 % of every request. Home Assistant's MCP descriptions are long, many tools are never needed. A filter by channel or usage would shrink every round, but would go against the common prefix of all channels (section 3), which keeps the cache locally.
