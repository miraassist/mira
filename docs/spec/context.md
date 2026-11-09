# Context – Spec

Status 2026-11-05: built in 0.6.9; device part of HA's prompt (section 1.1) in 0.6.48; without HA's prompt an error (section 1.2), built, not released; a list of exposed entities rebuilds the house at once (section 1.3), built, not released. Applies to all channels. See also [Threads](threads.md) and [Channels](channels.md).

The **context** is everything the model sees with a request. Mira does two things with it:
- **Composition**: what goes into the context window in which order;
- **Compaction**: what drops out when the history gets too long.

## 1. Composition: what the model sees

In this order (`internal/prompt`, `prompt.Build`):

| # | Part | Content | changes when | Layer |
|---|---|---|---|---|
| 1 | **System message** | Role and rules; house structure (HA's Assist prompt; without it Mira does not answer, section 1.2); house profile (condensed house memory) | Rules: never; structure: device/area new or renamed; profile: nightly condensation | 1, 4, 5 |
| 2 | **Tools** (own field of the request) | HA's tools via MCP, Mira's own (memory, messages, reminders, tasks, skills …), further MCP servers | Tool list changes | 3 |
| 3 | **Example conversation** | a fixed short exchange that shows tone and tool use | never | – |
| 4 | **Channel style** | how to reply on this channel (voice: short, without Markdown; Telegram: longer, lists, links), as a message with a fixed confirmation | never (fixed per channel) | 2 |
| 5 | **History** | the previous turns of this session, verbatim, with tool calls and results (sections 4, 5) | every turn appends; compaction cuts | 6 |
| 6 | **Current turn** | time; channel and room; the lines that HA's Assist prompt appends for this device (1.1); who is speaking and their personal profile; matching memory facts; state of the running thread; context of a message just announced; open confirmation; the text | every request | 7 |

### 1.1 HA's Assist prompt: the common prefix in part 1, the device in the turn

HA's Assist prompt (part 1, house structure) is not the same for every device. Home Assistant appends lines **at the end** that depend on the asking device: which area it is in ("You are in area Wohnzimmer and all generic commands … should target this area", otherwise the request to name an area) and whether it can start timers ("This device is not able to start timers", if not). HA puts this at the end because its own cache likewise depends on the same beginning. Measured (HA 2026.10): around 3.5 KB of prompt, of which only the last one or two lines depend on the device.

Mira therefore fetches the prompt via MCP (`prompts/get` with the one prompt `prompts/list` offers: Home Assistant names it after the LLM API, "Assist" until 2026.10, "Device control" since 2026.11) **not only without a device**, otherwise it would get the "cannot do timers" version and the model would refuse timers even where the device can do them (this happened). Instead:

1. It fetches it **once without a device** and **once as each satellite** (the id goes into `_meta` with `io.home-assistant/device_id`, as with the tools).
2. **The common beginning of all versions**, up to the first difference and cut at a line boundary, is **part 1** (the prompt cache `internal/ha/assistprompt`). It is the same for all rooms and channels, so the cache behind it (tools, examples, history) is preserved.
3. **The rest of the asking satellite's version** is its piece and goes into the **turn** (part 6), under the heading "Hinweise von Home Assistant für dieses Gerät:", after the line with the channel. A channel without a device (Telegram) gets the piece of the version without a device. There "cannot do timers" is also correct. Background jobs (assessing, skill schedules) get no hints.
4. **Detection is by comparison**, not by knowledge of the text: Mira knows nothing about room or timer sentences, it takes what differs between the versions. If HA changes the wording or another device-dependent line is added, Mira follows without a change.

Properties:
- **Part 1 only changes when the house changes** (new or renamed entity), not when a device is added or the pieces change. Only a change of part 1 triggers the warm-up (`prefix_hash` changes then).
- **Assumption "at the end":** Should the difference come further forward in future, the common beginning gets shorter and more moves into the turn. That stays correct, only the cache gain shrinks. If the common part is below 90 % of the length, a warning appears in the log.
- **Failure:** If a satellite does not answer, it keeps its old piece; if the version without a device does not answer, everything stays as it was.
- **Without satellites** (none known) the whole prompt is part 1, as before.
- **Without HA's prompt** Mira does not answer at all (section 1.2).
- **Cost:** one or two lines in the turn per voice request (estimated about 40 tokens) and one extra fetch per satellite when the prompt is fetched anew (every few minutes and on changes).

The same rule applies to the **tools**: the list is fetched once without a device and once as each satellite and merged, and every call carries the satellite's id, so that HA serves it like its own agents (timers, room for commands without a room; [Scheduled actions](scheduled-actions.md)). **What Home Assistant offers we do not rebuild, and nothing is lost because the context does not come along.**

### 1.2 Without HA's prompt: an error

Decided 2026-11-05: HA's prompt is part 1's house structure, and nothing replaces it. Until then Mira silently used its own structure block instead (areas, entities, aliases, ids). That block lacked HA's instructions for its tools, the device's lines (its area, whether it can start timers) and the names as HA's tools expect them; after Home Assistant 2026.11 renamed the prompt, Mira ran with it for hours and nobody learnt of it. The block is gone, and so is the setting `ha.assist_prompt_server`: the prompt comes from the MCP server `ha`, the one whose tools are `ha__…`.

- **A turn without HA's prompt** answers with a fixed sentence and calls no model: "Mir fehlt gerade die Beschreibung des Hauses von Home Assistant, deshalb kann ich gerade nichts tun. Unter Reparaturen in Home Assistant steht, woran es liegt." (in English: "I don't have Home Assistant's description of the house right now, so I can't do anything. Repairs in Home Assistant say why."). Every channel, voice and Telegram alike.
- **A repair** appears in Home Assistant once a fetch of the prompt has failed and there is none: the problem `prompt` of the source `ha` in `GET /v1/status`, with the reason (Home Assistant's answer, or that its MCP server is not reachable). It goes away when the prompt comes. Before the first fetch (at startup, while the MCP server connects) there is no problem yet, only the sentence.
- **A background job** (assessing, skill schedules, helpers) fails like a failed model call; it does not run with less.
- **The warm-up** warms nothing then and logs it.
- **A short outage keeps the last prompt** (as before, `internal/ha/assistprompt`): the text is Home Assistant's own, only not fetched again; a failed fetch does not empty part 1. Only a prompt that was never fetched counts as missing.
- **Interface:** the source `ha` is new. An older integration shows it with the text of a skill's problem and does not remove it when the prompt comes back (it removes only `skill_`, `agent_` and `hook_` issues); service and integration are released together, so this is the nuisance of a mixed update only.
- **Cost:** nothing new; the prompt is fetched as before. A turn without it costs no model call.

Steps:
1. **The service:** the own block and the setting go, the fixed sentence, the problem, background jobs and warm-up fail. Scenarios HP-01 to HP-05. Built.
2. **The integration:** the repair text for the source `ha` (`ha_problem`), and it removes `ha_` issues that are gone. Scenario HP-06. Built.

Testable rules: [context.feature](context.feature).

### 1.3 When the house changes

Status 2026-11-09: built (steps 1 and 2), not released.

HA's prompt follows the house: when the house structure (`internal/ha/structure`) is rebuilt and differs, the prompt is
fetched again ([Mira in Home Assistant](ha-integration.md), exposure). The structure is rebuilt after a change in the
registries (area, device, entity), 30 s after the last one (`ha.structure_debounce`), so a burst (renaming ten
entities) costs one rebuild.

What happened (live, 2026-11-09): a shopping list was created in Home Assistant at 08:01:38 and exposed; the
integration sent the new list of exposed entities at 08:01:44; a voice turn at 08:01:56 asked to put four things on
the shopping list. The structure was rebuilt only at 08:02:09 and the prompt at 08:02:10, so the turn's prompt knew
only the other list ("Aufgaben"), and the four things went there without an error.

Decided 2026-11-09:

| # | Question | Decision |
|---|---|---|
| 1 | When a list of exposed entities from the integration comes | The structure is rebuilt at once, not after the 30 s; the integration already gathers a burst into one send (1 s). A rebuild still waiting for registry changes is dropped: the one at once reads the registries too |
| 2 | Changes in the registries | As before: one rebuild 30 s after the last change |
| 3 | What stays | A turn between the change in Home Assistant and the integration's send still has the old prompt: about a second (the integration's gathering) and the rebuild. Checked on the test HA before decision 4 (2026-11-09): a new list "Baumarkt" created at 09:40:22, the rebuild after the registry change at 09:40:27 found nothing new, because the structure lists only what the integration's list names; the integration sent only with its status check at 09:41:12, the structure and the prompt followed within 40 ms, and a turn at 09:41:13 put the item on the new list |
| 4 | A new entity (2026-11-09) | Home Assistant exposes a new entity on its own (the default for new entities) without an exposure event, so until now it reached Mira only with the integration's next status check, up to a minute later. The integration now sends the list when the entity registry reports a new entity of any domain, gathered with the other changes (1 s). Checked on the test HA (2026-11-09): a new list created at 11:11:48.34, sent at 11:11:49.35, HA's prompt with it at 11:11:49.41 |

Steps of a send of the integration (`PUT /v1/ui/ha` with `exposed`):
1. The list is kept in the database, as before. When that fails, the integration gets the error (500) and sends again
   with its next status check (the fingerprint differs); no rebuild.
2. The structure is rebuilt at once, in the background: the three registries are read from Home Assistant (areas,
   devices, entities; three calls over the WebSocket). The send does not wait for it. When a read fails, the last
   structure stays, the log has "structure rebuild failed" with the error, and the next send or registry change
   tries again.
3. When the structure differs, HA's prompt is fetched again (as before: once without a device and once as each
   satellite, one MCP call each, so it grows with the number of satellites), and with a changed prompt the warm-up
   runs. A failed fetch keeps the last prompt (1.2).

**Cost:** per send of the integration three registry reads, and the prompt fetches only when the house changed. The
integration sends after a change of the exposure, the rooms or a satellite, after a new entity, and when the
fingerprints differ; not on a schedule. A burst of new entities (an integration added with fifty) is one send. No turn
waits for it.

Steps:
1. **The service:** a send of the integration rebuilds the structure at once. Scenarios HU-01 to HU-03. Built.
2. **The integration:** a new entity in the entity registry sends the list (decision 4). Scenario HU-04. Built.

What is not in the context: **states of devices.** The model queries them when needed via tools; the rules require that.

## 2. Rendering of entities

Status 2026-10-17: built are stages A, B and D, the basic form "value" and the timeline with `calendar_events` and `view: upcoming` (0.6.19). `history`, `weather_forecast` and `list_attribute` follow.

An entity reaches the model at different times by different paths, each in its own rendering. For each path it says here whether HA delivers it (Mira does not change it) or Mira builds it (Mira determines what the model sees).

### 2.1 Four stages

| Stage | When | What the model sees | Who renders |
|---|---|---|---|
| **A. Structure** | always, in the prefix (part 1, cache) | that the entity exists: name, area, type. **Never states**: they would break the cache on every change. | HA (Assist prompt) |
| **B. Turn** | every request (part 6) | only what belongs to the place and the person: area of the satellite, waiting messages, running thread. No states of devices. | Mira |
| **C. Query** | when the model calls a tool | **now**: `GetLiveContext` from HA, state and selected attributes. **Over time**: Mira's tools for appointments, history, forecast, list attributes (2.3). | HA or Mira |
| **D. Background** | assessment of a task (`mira.assess`, `mira.decide`) | a condensed view for exactly one question (`data[].view`, see [Tasks](tasks.md)) | Mira |

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
| Time format | in the language of the conversation ([Language](language.md)), relative where it helps | "heute 14:12", "morgen" |
| Keys | short keys (`t1`, `t2` …) instead of HA IDs, where one can act on an entry | cancel appointment |

The code keeps the HA IDs for the keys; the model does not see them. Only what really differs remains type-specific (2.4).

### 2.3 Timeline tools

All with the same parameters for window, selection, sorting, limit and fields:

| Tool | Source in HA | Spec |
|---|---|---|
| `calendar_events` | REST `/api/calendars/<entität>` (with appointment ID) | [Calendar](calendar.md) |
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

## 5.1 A tool result that is too large

Decided 2026-11-01. Before, every tool result was cut at a fixed 8,000 characters with only "[…]" at the end, never
specified: the full sensor list of a house with 210 entities (`GetLiveContext`) lost everything after about 50
entries, and neither Mira nor the user knew.

- **The limit follows the model:** a tool result may take a quarter of the context window of the model that runs the
  turn or the helper, counted as characters: `context_tokens` characters (a quarter of the tokens, at about four
  characters per token). With the default 16,384 tokens that is 16,384 characters; Gemini's large windows let a
  whole house's list through.
- **When a result is longer, it is cut, and Mira is told:** the result ends with a note in Mira's language that
  names how much she sees and how much there was, and how to ask more precisely: "[Gekürzt: Du siehst 16384 von
  23400 Zeichen; der Rest fehlt. Frag gezielter, etwa nach einem Bereich, einer Domain oder einem Namen.]" So she
  does not take a cut list for the whole.
- **The log says so** as a warning `tool result cut`: tool, characters before and after, the model's context.
- A model without `context_tokens` gets the default (16,384); there is no other fixed limit.

## 6. Configurable values

| Value | Default | Meaning |
|---|---|---|
| `sessions.max_turns` | 50 | from here on it is cut |
| `sessions.keep_turns` | 30 | this many turns remain after the cut |
| `models.<name>.compact_at_fraction` | 0.9 | fraction of the context window (`context_tokens`) from which it is cut |
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

**Cost:** It depends on the input tokens, and those are limited by `context_tokens` (with `compact_at_fraction`). There is no second cost limit. Since 2026-10-25 Mira asks the model's server for the context size when the model is set up ([Models](models.md), Context size); for Gemini that is the model's full input limit, so the history is cut by `max_turns` long before 90 % of the context is reached.

**Ideas**
1. **Default `context_tokens` for cloud models:** done 2026-10-25. The default of 16,384 cut a cloud model's history after about 20 turns; now the size comes from the model's server at setup ([Models](models.md), Context size).
2. **Shorten old tool results in the history** (calendar lists, states), replaced by a short text after the last few turns. That would keep the history small without shortening the conversation. It contradicts the sentence in section 7 ("Old turns stay verbatim") and would have to be weighed against it.
3. **Small context for the reply after a confirmation.** If a tool ends the request (`thread_confirm`, `thread_cancel`, `thread_undo`), the second round could get only personality, the turn and the result of the call, without tools, house and history. The reply would go into the log as before. Open: only for cloud models, because a deviating request displaces the cache on a local server. On errors and with several calls in a row it stays with the full context. The result texts of the finishers would have to be understandable on their own. The gain lies more in cost than in time. Not built for now.
4. **Cut down tools.** 51 definitions are 72 % of every request. Home Assistant's MCP descriptions are long, many tools are never needed. A filter by channel or usage would shrink every round, but would go against the common prefix of all channels (section 3), which keeps the cache locally.
