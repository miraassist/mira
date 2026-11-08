# Always a warm slot for local models – Spec

Status 2026-11-08: built (steps 1 to 3, in 0.15.2; step 4, the reading at the start, 2026-11-08, not released), checked on the test machine; the guide explains it under "Several models". Scenarios: [warm-slot.feature](warm-slot.feature).

## Goal

A local model reads Mira's fixed prompt (layers 1 to 5, about 15,000 tokens with the tools) once and keeps it in its
cache; a turn then only reads what is new. When that prompt changes (Home Assistant's tool list, the house structure,
the house profile), the model must read it anew: with qwen on llama.cpp about 40 s. Whoever needs the model then waits.
Today that can be a voice turn: on 2026-11-03 a "Stopp" waited behind a warmup of a new prompt and ran into the
integration's timeout (30 s), after Home Assistant had been restarted.

From now on a voice turn always finds a warm prompt: the new one is read in the background, in a second slot, while
turns go on with the previous one.

This is the last optimisation for local models (the maintainer, 2026-11-03); cloud models need none of it.

## What was measured (2026-11-03, qwen3-30b-a3b on llama.cpp, the test machine)

- **A cold prompt:** 9,309 tokens in 18.6 s (500 tokens/s); Mira's prompt of 14,816 tokens in 41 s (llama.cpp log).
- **Reading a cold prompt beside an answer stops the answer:** a story wrote 34.9 pieces/s before, 0.3 while a cold
  prompt of 9,328 tokens was read beside it (the reading kept its speed), with silences of up to 4.9 s. llama.cpp
  reads in blocks of 2,048 tokens (`--batch-size`) and puts one token of the answer between two blocks. So a warmup
  never runs beside a turn.
- **Slots share one memory:** the server ran with `--parallel 4 --kv-unified --ctx-size 29814`: the four slots share
  about 30,000 tokens. Two warm prompts of 15,000 tokens and an answer do not fit.
- **With a shared memory, a working slot empties a resting one:** with `--kv-unified` and `--ctx-size` 60000, reading
  a prompt into slot 1 made slot 0 cold, although both fit (a turn on slot 0's prompt: 0.2 s before, 38.2 s after;
  the server logged "making room for prompt cache entry" each time). Why, was not looked up in llama.cpp's code.
  Without `--kv-unified`, each slot has a memory of its own: `--parallel 2 --ctx-size 60000` gives each 30,208
  tokens.
- **The memory:** about 96 KiB per token (f16; 48 layers, 4 KV heads of 128 from the model's description, a
  calculation, not measured), so 60,000 tokens about 5.5 GiB instead of 2.7 GiB. The test machine's GPU takes it
  from the system memory (an APU); 20 GiB were free.
- **`id_slot` is honoured by llama.cpp:** a request with `id_slot` 2 and one with 1 each ran in that slot
  (`/slots`). LM Studio ignores it (tried 2026-10-14), so there this does not work.

## The concept it hangs on

The warmup of today (`warmup_on_start`, `warmup_on_prefix_change`, [Models](models.md)) and the prompt layers
([Context](context.md)). Two departures, each with its reason:

1. **Turns may run on the previous prompt** until the new one is warm. Reason: the only other way to a warm prompt is
   to wait for it. What Mira sees is then up to about 40 s behind: a tool Home Assistant just added is not there yet,
   a device just exposed not yet known; a tool just removed answers as unknown when called. This is named here and
   in the guide; it is the price of the warm slot.
2. **Mira chooses the slot** (`id_slot`) instead of the server choosing by the longest common prefix.

## Decided (2026-11-03)

| # | Question | Decision |
|---|---|---|
| 1 | The idea | Two slots take turns: the warm one serves the turns, the other is read into in the background; once warm, they swap |
| 2 | When the background reading runs | Only while the model is free. A turn that needs the model stops a running reading; it starts again once the model is free |
| 3 | Turns while the new prompt is not warm | On the previous prompt, in the warm slot |
| 4 | Which models | Local models on llama.cpp (they honour `id_slot`); not LM Studio, not cloud models |
| 5 | More optimisation for local models | None after this |
| 6 | When it is on | A setting of the model, `warm_slots: true` (default off), in `mira.yaml` and on the page "Models". Only for a local model; Mira does not guess the server |
| 7 | Which two slots, and `pin_slots` | Slots 0 and 1, for every channel (they share the prompt but for a short style message, [Context](context.md)); `pin_slots` is not used for a model with `warm_slots` |
| 8 | Mira's helpers (agents: delegate, skills' agents, tasks, the profile) on the same model | In the spare slot: the warm slot belongs to the conversation turns. A helper stops a reading there, as a turn does; it starts again once the model is free |
| 9 | Which prompt is read in the background | The voice channel's: the other channels find the shared part warm and read only their short end |

## How it works

Mira keeps, per such model:

- **the served prompt**: the layers 1 to 5 last read in, with their hash, and the slot that holds it (the warm slot);
- **the new prompt**, if the prompt changed since: the slot it is read into (the spare slot), and whether the reading
  runs.

| Event | What Mira does |
|---|---|
| The prompt changes (tools, structure, profile) | Notes the new prompt. If the model is free, starts reading it into the spare slot (a request with `max_tokens` 1, as the warmup today). A newer change replaces a note not yet read |
| A turn starts | Builds it on the served prompt, sends it with `id_slot` of the warm slot. A running reading is stopped (the request is cancelled) |
| A helper's request | Sent with `id_slot` of the spare slot; a running reading is stopped, as for a turn |
| A turn ends, the model is free | If a new prompt waits, starts reading it again |
| The reading ends | The new prompt is the served one, its slot the warm slot; the old warm slot is the spare one |
| Mira starts | Reads the prompt into a slot (the warmup of today); until then a turn waits, as today. `warm_slots` alone is enough for this: no `warmup_on_start` is needed (step 4) |

A turn of the layers 6 and 7 (history, the current turn) is unaffected: only the fixed part waits for its slot.

## What it costs

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| reading a new prompt | one request with the whole fixed prompt, `max_tokens` 1, in the spare slot | about 15,000 tokens of reading (qwen: about 40 s), only while the model is free; once per change of the prompt | no |
| a turn | as today, with `id_slot` | nothing more | — |
| memory | two prompts and an answer in the server's context | the server's `--ctx-size` about 60,000 instead of 30,000 (about +2.7 GiB with qwen in f16) | — |

Nothing grows with the number of turns.

## Setting up the server (guide)

- llama.cpp with two slots of their own: `--parallel 2`, `--ctx-size` twice what one of Mira's prompts and an answer
  need (about 60,000 for qwen), and **without** `--kv-unified` (with it, a working slot empties the resting one);
  then `warm_slots: true` on the model.
- The guide says what the warm slot costs (memory) and gives (no waiting after a change) and that after a change Mira
  may for up to about 40 s not know a device just exposed.

## What we do not do, and why

- **Switch it on by asking the server** (whether it answers on `/slots`). Decided against: a setting is plainer
  than a guess about the server (decision 6).
- **Read the new prompt beside a running turn.** Measured: the answer then stands still (0.3 pieces/s, silences of
  4.9 s) for the whole reading.
- **Only let a turn stop the warmup** (without a second slot). The turn then finds the cold prompt itself and waits
  the same 40 s.
- **More slots or smaller reading blocks** (`--batch-size`). More tuning for local models is not wanted (decision 5).

## Steps

1. **Check on the test machine:** done 2026-11-03 with `--parallel 2 --ctx-size 60000` without `--kv-unified`, two
   prompts of about 15,170 tokens: both warm at once (a turn on each 0.6 and 0.7 s, all but 10 tokens from the
   cache); after a story of 1,500 tokens in slot 0 the other still warm (1.1 s); a reading into slot 1 cancelled
   after 5 s left slot 0 warm (0.4 s). Memory: +2.7 GiB (GTT 23.5 to 26.2 GiB), as calculated.
2. **Build:** the setting `warm_slots` (`mira.yaml`, the page "Models"); the served and the new prompt, the slot per
   request, the reading only while free, stopping it for a turn.
   Checked end to end on 2026-11-03 (test Mira with qwen, renaming a light in the test HA): a turn right after the
   change ran in 3.3 s on the previous prompt (it did not know the new name yet, as decided); the new prompt was read
   into slot 0 in 40.3 s; the turn after it ran on it in 3.8 s with 14,808 of 14,928 tokens from the cache. A turn
   during a cold reading stopped it and ran warm, but took 7.6 s instead of about 4: llama.cpp stops a reading only
   at the end of its block of 2,048 tokens (`--batch-size`), about 4 s here. Smaller blocks would shorten that; not
   done (decision 5). The stopped reading started again on its own and finished in 28.8 s.
3. **Guide:** setting up the server, what it costs and gives.
4. **The reading at the start** (built 2026-11-08): seen live on 2026-11-08, a model with `warm_slots` set on the page
   read nothing after a restart: the start's reading ran only when some model had `warmup_on_start`, which the page
   cannot set. Now `warm_slots` is enough, as the table above says. When the reading fails, the log says so, as for
   every reading (`could not read the prompt`), and the first turn reads the prompt itself. A reading's start is
   logged too (`reading the prompt into a slot`, and `warming a slot` for a warmup without warm slots), so a reading
   that takes long shows in the log from its start. WS-08.

## Later

Nothing planned (decision 5).
