# Models

Status: built. Scenarios: [modelle.feature](modelle.feature).

## Goal

Today three names are special: the model under `llm` is `main`, `routing` sends roles (voice, telegram, background,
delegate, tasks, mail) to models under `models`, and whether a model is local is guessed from its address in one
place, from the setup form's kind in another, and from "main is the local one" in a third. Instead:

- **A model is a named entry** with an endpoint, a model, a token and a **privacy** qualifier (`local` or `cloud`).
  How many requests it takes at once is its own setting.
- **`main` always exists.** An agent without a model takes `main`.
- **Every agent names its model**, Mira's own ones too; `routing` goes.
- **A fallback** is configured by name, never implied.
- **Privacy is a rule:** an agent or a skill can require a local model.

## The models in mira.yaml

```yaml
models:
  main:                          # must exist
    endpoint: http://lmstudio:1234/v1
    model: qwen/qwen3-30b-a3b-2507
    privacy: local               # local | cloud, required
    parallel: 1                  # requests at once; default local 1, cloud 4
    cache_prompt: true
    context_tokens: 16384
  smart:
    endpoint: https://generativelanguage.googleapis.com/v1beta/openai
    model: gemini-flash-latest
    token: ${GEMINI_API_TOKEN}   # secrets from the environment, as everywhere
    privacy: cloud
    reasoning_effort: low
    context_tokens: 200000
    fallback: main               # only when configured
```

| Field | Meaning | Default |
|---|---|---|
| `endpoint` | the OpenAI-compatible address | required |
| `model` | the model's name at that address | required |
| `token` | the API key, usually `${…}` from the environment | none |
| `privacy` | `local`: the data stays in the own network; `cloud`: it leaves it | required |
| `parallel` | how many requests the model takes at once (Mira waits for a free one) | local 1, cloud 4 |
| `fallback` | another model's name, taken when this one fails | none |
| `max_tool_rounds`, `temperature`, `max_tokens`, `timeout`, `context_tokens`, `compact_at_fraction`, `reasoning_effort` | as today | taken from `main` when left out |
| `cache_prompt`, `warmup_on_start`, `warmup_on_prefix_change` | llama.cpp's prompt cache and warming it up | off |
| `pin_slots` | llama.cpp only: the slot per conversation channel (`voice`, `telegram`, `background`) | none |

- **`llm:` and `routing:` are gone.** A `mira.yaml` that still has them does not load; the error says what to write
  instead (`llm` → `models.main`; `routing` → `agents.<name>.model`). There are no users to migrate yet.
- **`parallel`** replaces the fixed "local 1, cloud 4" (agenten.md, Decided 26) and the guess from the address.
- **Warming up** happens for the models with `warmup_on_start` / `warmup_on_prefix_change`, for the conversation
  channels whose agent takes that model. A cloud model is never warmed (it costs money and buys nothing).
- **`pin_slots`** is the old `llm.slots` with `slot_pinning`, now per model; without it no `id_slot` is sent.

## Which agent takes which model

Every agent names its model; without one it takes `main`.

- **A YAML agent** names it in its description (`model:`), as today.
- **Mira's own agents** get it in `mira.yaml` under `agents`, by their name, with only `model` and `privacy`:

  ```yaml
  agents:
    conversation.voice:    { model: main }
    conversation.telegram: { model: smart }
    delegate:              { model: smart }
    mail.read:             { model: main, privacy: local }
    mail.match:            { model: main }
    tasks.plan:            { model: smart }
  ```

  Any other field for one of Mira's own agents is a problem of that entry (it does not start, Mira does).
- What used to be a role is now each agent on its own. The roles of today and their agents:

  | Role (`routing`) | Agents |
  |---|---|
  | `voice` | `conversation.voice` |
  | `telegram` | `conversation.telegram` |
  | `background` | `skill.task`, `notify.compose`, `tasks.assess`, `tasks.decide`, `profile.compact` |
  | `delegate` | `delegate` |
  | `tasks` | `tasks.plan` |
  | `mail` | `mail.read`, `mail.match` |

## Privacy as a rule

- **An agent** may say `privacy: local` (YAML agents in their description, Mira's own under `agents`): it runs only
  on a model with `privacy: local`.
- **A skill** may say `privacy: local` under `metadata.mira`: every agent it uses must run locally: its own agents,
  the agents its target names, and for a mail skill `mail.read` and `mail.match`.
- **When the model does not fit:** an agent whose model is `cloud` although it must be local does not start; a skill
  whose agents do not fit does not start. Both are problems shown in Home Assistant (Repairs), with the model and
  the rule named. Mira starts all the same.
- **Fallback:** an agent that must run locally never falls back to a `cloud` model; such a fallback is skipped (and
  the call fails as without a fallback).

## Fallback

- A model's `fallback` names another model. It is taken when the model **fails**: after its retries an error, or its
  time is up. A model that is only busy (all `parallel` requests taken) is waited for, not passed by.
- **Chains** are allowed (`smart` → `lite` → `main`); a **circle** is an error when loading the config.
- The call takes the fallback's own settings (`max_tokens`, `timeout`, …). The log line of the run names the model
  that answered and that it was a fallback (`model=main fallback_from=smart`).
- Conversations take fallbacks too, as every agent.

## What the integration sets

The integration's setup is unchanged: it picks one model (from Home Assistant, a provider or an own server) and
sends it to the agent. The agent keeps it as **`main`**, the setup form's kind giving `privacy` (provider → `cloud`,
from Home Assistant: the source's kind, own server → `local`). What the integration sets wins over `mira.yaml` for
`main`, as today for `llm`. Further models come from `mira.yaml`.

## Documentation

The guide gets a section "Models" (both languages): the YAML above, what `privacy` and `parallel` do, how an agent
names its model, fallback, and a section "Commands" with Mira's command line (`run`, `pair`, `check`, `skill`,
`chat`, `profile`), how to call them in the container (`docker exec mira /app/mira …`).

## Steps

1. **Models:** `models` with `main`, `endpoint`, `token`, `privacy`, `parallel`, `pin_slots`, the per-model settings;
   `llm` and `routing` refused with a clear error; the integration's model as `main`; warming up per model.
2. **Agents name their model:** YAML agents as today, Mira's own under `agents`; the runtime and the conversation
   take the agent's model; `parallel` instead of the address guess.
3. **Privacy:** `privacy: local` for agents and skills; problems when it does not fit.
4. **Fallback.**
5. **Documentation:** guide sections "Models" and "Commands"; example config, test config, eval setups.

## Decided

1. A model has a name, an endpoint, a token (from the environment) and a privacy qualifier; `main` must exist.
2. An agent without a model takes `main`; a fallback is configured by name.
3. `routing` goes; every agent names its model, Mira's own under `agents` in `mira.yaml`.
4. `llm:` is a hard switch: an old config is refused with what to write instead.
5. Privacy is a rule: agents and skills can require a local model; a local requirement never falls back to the cloud.
6. A fallback is taken when the model fails (error, time up), not when it is busy; chains yes, circles no.
7. The integration's model becomes `main`.
