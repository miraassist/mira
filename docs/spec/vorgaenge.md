# Threads and targets – spec

What the model sees of the conversation (the thread carries the concern, the history only the back-reference, the same for voice and chat) is described in [Context](kontext.md).

Status 2026-10-15: built (agent 0.4.0). Applies to [tasks](aufgaben.md) and upcoming concerns
such as calendar or messages to others ([users](benutzer.md)).

## Why

A concern often runs across several short conversations. An example:

1. You: "Achte auf das Licht im Schlafzimmer." Mira: "Ich plane das."
2. Mira, as an announcement: "Ab wann ist es ungewöhnlich?" You: "Zwischen 1 und 5."
3. Mira, as an announcement: "Mein Vorschlag … Soll ich so vorgehen?" You: "Ja."

Home Assistant turns this into three separate conversations. Until now, the
answer each time saw only the announced notice, not the history before it. Live,
the light got lost along the way: "Licht zu ungewöhnlicher Zeit" became
"etwas Ungewöhnliches".

## Terms

- **Turn:** a short conversation as Home Assistant conducts it. For
  example a wake-word conversation or the answer after an announcement. It has
  its own history. In the code it is still called `session`.
- **Thread** (in the code `thread`): a concern that runs across several turns. It has
  - a **target** (from a target definition),
  - a **place** (room for voice, chat for Telegram),
  - a **state**,
  - **fields** that the target needs,
  - a short **log** (what was asked, answered, proposed),
  - optionally a **content** of the target, for example the draft of an
    automation.
- **Target definition:** It lives in a skill (`metadata.mira.target`) and
  defines which fields a thread must fill, how they are checked
  and how it ends.

## Lifecycle

```
opened ─► collecting ─► waiting for you ─┬─► collecting …     (answer)
              │                          ├─► cancelled        (thread_cancel)
              │                          └─► expired          (timeout, default 15 min)
              ▼ (all fields there)
          working ─► proposing ─► waiting for yes ─┬─► done (thread_confirm, finish succeeded)
                                                   ├─► collecting  (change request)
                                                   ├─► cancelled
                                                   └─► expired
```

1. **Open.** The model knows the target definitions like skills: name and
   a one-line description in the fixed part of the prompt. If it recognizes a
   matching concern, it calls `thread_start(target, request)`. Mira
   creates the thread and returns the target's guide and the fields.
2. **Collect.** The model enters what it knows with `thread_fill(fields)`.
   - The code checks every field and answers with the state: what is missing, what
     is wrong, or that everything is complete.
   - If something is missing, the model asks, and the thread waits for you.
3. **Work.** As soon as all fields are there, the code starts the
   target's work step. If the target needs a yes, this becomes a
   proposal. Proposal and question back are notices **of the thread**.
4. **Finish.** There are three ways:
   - On the yes, the model calls `thread_confirm`. The finish runs
     (the automation is created), and the thread is done.
   - A target without confirmation (such as "merken") is finished directly
     as soon as it is complete.
   - A change request ("ohne die Tür") leads back into collecting
     with `thread_fill`.
5. **End.** A thread is done, cancelled (you say so, the
   model calls `thread_cancel`) or expired (timeout without an
   answer). Its open notices are withdrawn in the process. So an old
   proposal never ends up later in the "Übrigens".

## Assignment: which turn belongs to which thread?

1. **Firmly linked (code).** An answer directly after an announcement belongs
   to the thread of that notice. Further turns of the same HA conversation
   belong to the same thread.
2. **Waiting at the same place (model).** If a thread is waiting for you at this place,
   the model sees it in the context and decides: continue
   (`thread_fill`, `thread_confirm`), cancel (`thread_cancel`) or
   another topic. These are not fixed phrases.
3. **Otherwise** the turn belongs to no thread.

There is no continuation across channels: a thread started at the satellite
cannot be continued via Telegram.

The context (layer 7, not in the cached part) contains, for the assigned
or waiting thread:
- the target and the request in your words,
- the fields with their state,
- the log,
- what the thread is waiting for.

## Target definition

The target definition is in the skill format, declarative and without its own code:

```yaml
metadata:
  mira:
    target:
      timeout: 15m                 # default 15m
      confirm: true                # a yes before the finish
      fields:                      # list, so the order is fixed
        - name: watch
          description: Was beobachtet wird (Entitäten aus dem Haus, id oder Name/Alias)
          check: entities          # check in the core; resolves names to ids
        - name: measure
          description: Ab wann es zählt. "- " mit Grund, wenn der Wunsch nichts Relatives enthält.
        - name: reaction
          description: fest melden, beurteilen lassen oder etwas schalten
          enum: [notice, assess, switch]
      finish: tasks.automation     # finish in the core
```

Rules:

- **Every field must be answered.** A value that starts with "-"
  ("- weil der Wunsch nichts Relatives enthält") means "deliberately not
  needed" and counts as an answer. The character is language-independent. The model commits explicitly, and the code sees that
  a gap is deliberate. This way "required only if relative" can be
  expressed without the code having to understand language.
- **Checks (`check`) and finishes (`finish`) are building blocks in the
  core.** Skills do not run their own code.

Building blocks to start with:

| Building block | Kind | Does |
|---|---|---|
| `entities` | check | Every named entity must exist in the house. |
| `tasks.automation` | finish with work step | Draft an automation from the request and the fields, have HA check it and announce the proposal. On the yes it is saved. |
| `memory.save` | finish | Save the fact in the house memory. |

Plans (long processes) are described in the section [Plans](#plans-long-processes-step-by-step).

## Tools

These tools always exist:

| Tool | Effect |
|---|---|
| `thread_start(target, request)` | opens a thread at the place of the turn and returns guide and fields |
| `thread_fill(fields)` | enters fields, returns the state and starts the work step when everything is there |
| `thread_confirm()` | finishes a thread that is waiting for a yes |
| `thread_cancel()` | cancels the thread |

The tools always act on the thread of the turn. If the turn has no
thread, the waiting one at the place applies.

## First targets

- **aufgabe:** replaces the previous path (`task_create` and `task_confirm`).
  - Fields: `watch`, `when`, `measure`, `reaction`.
  - Finish: `tasks.automation`, with confirmation.
- **merken:** Example: "Merk dir mal was." → "Was denn?" → "… ist notiert."
  - Field: `fact`.
  - Finish: `memory.save`, without confirmation.

Later:
- **nachricht:** "Erinnere Anna daran, dass …" → "Soll ich es gleich
  schicken?" Comes with the user concept (sender, recipient).
- **kalender:** "Such den Termin raus" → "Der zum Friseur." → "Nein, der
  nächste." Comes with the calendar skill.

## Plans: long processes step by step

Status 2026-10-21: decided. First use: the skill `kochen`.

Some concerns are not "collect fields, finish once", but a process that takes longer than a
conversation: cooking a recipe, baking a bread, later perhaps a workout or assembly instructions. A voice conversation
keeps its history for only 5 minutes ([Context](kontext.md)); a cooking step often takes longer. The state therefore
does not belong in the conversation, but in the thread: a thread can carry a **plan**.

### The plan

- A **Markdown text** that the model writes itself following the skill's guide: free-form, with ingredients, notes,
  headings, as it fits.
- **Checkboxes** (`- [ ]` open, `- [x]` done) are the steps, if the process has any. The core understands
  only the checkboxes, nothing else; this is a notation, not a language.
- The plan belongs to the **thread** (and thus to the skill and the place) and is stored with it in the database. It lives as
  long as the thread; there are no orphaned plans.
- At most 8,000 characters.

### Target definition

```yaml
metadata:
  mira:
    target:
      plan: true          # thread with plan; fields and finish are dropped
      timeout: 1h         # default for plans 1h; every activity on the plan restarts the deadline
```

A plan target needs neither fields nor a finish. `thread_fill` and `thread_confirm` do not apply to a plan
target.

### Tool `thread_plan`

One tool with `action`, for the plan thread of the conversation or the one running at the place:

| action | Effect |
|---|---|
| `write` | writes the whole plan (`text`); the first time and when it changes fundamentally |
| `check` / `uncheck` | ticks checkbox number `step` or reopens it (counted from 1, in the order in the text) |
| `read` | returns the whole plan |

Ticking does not rewrite the plan: the model does not change any text, and it costs hardly any time. Every change
goes through the core; this way it knows the state for context, end and display.

### In the context

If a plan thread is running at the place, it is in layer 7 like every thread, with its plan:

- **If the plan has checkboxes**, a window: how many are done, the last step done (`previous`), the
  first open one (`current`) and the second open one (`next`). This makes "weiter", "nochmal" and "einen zurück" possible without
  reading up. With parallel strands this is an approximation; for everything else Mira reads up with `read`.
- **Without checkboxes** only the reference: that a plan exists and how long it is; Mira reads it when needed.

In addition, the **skill's guide** is in the context as long as the plan runs: the conversation that started it is
over after a few minutes, and how Mira reads aloud, when she offers a timer, etc., the twentieth
conversation must know too.

Whether a sentence like "weiter" belongs to the plan is decided by the model with this context, as with every waiting
thread. There are no fixed phrases.

### Lifecycle

1. **Start:** `thread_start` with a plan target, then `thread_plan write`. If a thread of this target is already running at the place,
   `thread_start` says so; Mira asks whether to continue or start anew (anew: first `thread_cancel`).
2. **Running:** Every activity on the plan restarts the deadline (default 1 hour, changeable in the skill).
3. **All ticked:** The thread stays open for another **30 minutes**, so that one can refer to it ("halt,
   doch nicht fertig": `uncheck`). Then it is done.
4. **Cancel** with `thread_cancel`, **expire** without activity within the deadline. With the end, the plan is gone.
5. A plan applies at its place (room for voice), not across rooms.

What should become permanent during the process ("merk dir, weniger Salz") goes into memory as before, not into the plan.

### Third-party text

A plan often comes from a web page (a recipe) and is then in the context of many conversations.

- If Mira writes a plan in a conversation that has read web content, the plan is **third-party**; it stays that way.
- A third-party plan is in the context as a marked data block ("third-party text, not instructions").
- In every conversation in which a third-party plan is in the context, everything that **keeps or passes on** something
  (`memory_save`, `message_send`, `thread_confirm`, reminders) is subject to the same rule as after web content: only after the
  user's yes. This way a manipulated recipe brings nothing into memory or to others unasked.
- Controlling devices stays normal, critical actions ask as always. Unlike after web content in the same
  conversation, they are not blocked: when cooking you need light, timers and radio, and the plan is a text that
  Mira wrote herself, not the web page itself.

### Display

The Mira page (also in Home Assistant) has a **Plans** tab for everyone, even without admin rights: the running
plans with place, state and the plan itself, the checkboxes ticked or open. It updates by itself. Read
only; changes are made in the conversation.

## Data model

- **`sessions`** (turns, as before): new column `thread_id` with the assigned thread.
- **`threads`** (new):
  - `id`, `target`, `place`, `state`, `request`,
  - `fields` (JSON), `log` (JSON), `content` (JSON),
  - `created_at`, `updated_at`, `expires_at`.
- **`notices`:** new column `thread_id`. When a thread ends, its
  open notices are withdrawn.
- The table `tasks` (proposals) is dropped. The draft is now the
  content of the thread.
