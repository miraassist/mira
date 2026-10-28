# Hooks – Spec

Status 2026-10-28: steps 1 and 2 built (the small version, skill actions). Scenarios: [hooks.feature](hooks.feature).

## Goal

Until now the guard (`internal/tools/policy.go`) decided on every tool call. It knew *what* is switched (domains,
entities, tools from `security.confirm_*`, the label "Mira: nachfragen"), but not *who* asks, *where* or *when*. What
a household wants beyond that ("an unknown voice does not unlock", "the garage door without asking at the garage's
satellite") had no place, and its own rules were Go code nobody outside reads.

Instead:

- **One concept, hooks**, replaces the guard's decisions completely. A hook is a small script that Mira calls before
  a tool call (`pre_tool`) and that says: let it run, ask for a yes, or refuse.
- **Hooks are written in Starlark** (`go.starlark.net`), a small language that reads like Python, made for
  configuration that is really code. No conditions in YAML.
- **What the guard decided becomes hooks too**: system hooks that cannot be changed, and a default hook that a
  household can replace. One place to read why Mira does or does not do something, the built-in reasons included.
- **What stays in Go is the mechanism**: collecting the facts a hook sees, resolving the targets of a call, keeping a
  pending confirmation and matching the user's yes to it, remembering that a turn read web content, carrying out the
  verdict. That is the guard now (`tools.Guard`); it asks the hooks (`internal/hooks`) instead of deciding itself.

This is the small version, decided 2026-10-28: only what is needed to replace the guard without losing anything,
plus the household's own hooks. What else was discussed is under "Later".

## Terms

Users see hooks as **rules** ("Regeln" in German): on Mira's page (Capabilities → Rules), in the guide and in what
Mira says. "Hook" is the technical word: in this spec, the code, the log and the folder `hooks/` (decided
2026-10-28).

| Term | Meaning |
|---|---|
| **Hook** | A Starlark file with a `pre_tool` function and a `DESCRIPTION`. |
| **Level** | Where a hook comes from: system, default or household. Decides its order and whether it can be replaced. |
| **Call** | One tool call of the model as the hooks see it: the tool, its arguments, the resolved targets and the facts of the turn. |
| **Verdict** | What the hooks decided together: allow, confirm or deny. |

## Levels

| Level | Where it lives | Change |
|---|---|---|
| **1. System** | in the binary (`internal/hooks/system/*.star`) | no; readable on the page, not replaceable |
| **2. Default** | in the binary (`internal/hooks/default/*.star`) | a household hook with the same file name replaces it |
| **3. Household** | the folder `hooks/` next to `mira.yaml` (`*.star`) | freely |

- **System hooks** carry the protection against injected instructions and against acting without anyone who could
  answer: an agent run acts only with its rights, in the background nothing changes the house without them, after
  web content nothing critical, no question when nobody can answer.
- **The default hook** `critical.star` says what needs a yes: locks, the alarm, and everything with the label "Mira:
  nachfragen" in Home Assistant (e.g. a garage door: as a cover it cannot be told apart from a shutter, so it gets the
  label). A household that wants the garage door without a question at the garage's satellite replaces it with its
  own `critical.star`.
- **Household hooks** are the household's own. Mira watches the folder: a file written, changed or deleted applies
  at once, without a restart; a line `ACTIVE = False` switches one off without deleting it
  ([hooks-schreiben.md](hooks-schreiben.md)). Mira also writes them on request (the skill `regel`).

## Order

The hierarchy runs from system to household: **the system has the last word.** The order of calling is therefore the
other way round:

```
household (A→Z) → default (A→Z) → system (rights, background, web, self, ask) → the engine carries out the verdict
```

- **Within a level** the files run in the order of their names. A household that needs a certain order names its
  files `10_…`, `20_…`.
- **All hooks see the same call**; a hook cannot change it. Each also sees the verdict so far.
- **A `deny` ends the chain.** Nothing can be stricter; the hooks after it are not called.

## Verdicts

| Return of `pre_tool(call, verdict)` | Meaning |
|---|---|
| `None` or `allow()` | no objection. Never loosens what an earlier hook decided. |
| `confirm(reason)` | the call needs the user's yes; `reason` is optional |
| `deny(reason)` | the call is not run |

### The strictest wins

- **deny > confirm > allow.** A hook can make the verdict stricter, never looser. An `allow()` after a `confirm` stays
  a `confirm`. Two `confirm` keep the first (its hook and reason).
- **Loosening is done by replacing**, not by order: a household that wants less asked replaces the default hook. A
  system hook cannot be replaced, so what it refuses stays refused.

### What the model reads, what the engine does

| Verdict | Engine | The model reads |
|---|---|---|
| allow | runs the call | the tool's result |
| confirm, and the call is the confirmed one | runs it: the user's plain yes in this turn approves exactly the call that was asked about, once (same tool and same arguments, or same resolved targets) | the tool's result |
| confirm, otherwise | parks the call as a pending confirmation for the session (`security.confirm_ttl`); the turn ends waiting for the yes | Mira's question frame: not performed, the call, the `reason`, ask the user and end with the question, call again with the same arguments after the yes |
| deny | does not run the call | `reason` as it is |

- **A deny stays a deny**, also for the confirmed call: a turn that read web content does not unlock, even after "Ja".
- **Texts**: `text(key)` gives Mira's own texts in the household's language, so the built-in hooks follow the language
  like everything else. Keys: `critical`, `ask_after_web`, `deny_after_web`, `deny_background`, `deny_no_confirm`,
  `no_right` (with the missing right). A household hook writes its own text.

## What a hook sees

Everything is read only.

### `call`

| Field | Meaning |
|---|---|
| `call.tool.name` | the tool's name without its server (`HassTurnOff`, `memory_save`, `search`) |
| `call.tool.server` | the MCP server (`ha`, `paperless`), empty for Mira's own tools |
| `call.tool.qualified` | the name as the model calls it (`ha__HassTurnOff`) |
| `call.tool.read_only` | the tool only reads |
| `call.tool.own` | one of Mira's own tools |
| `call.tool.web_content` | its result is third-party text (web search, web page) |
| `call.tool.keeps_or_passes_on` | it keeps or passes on what the model says: memory, messages to others, ending a thread |
| `call.tool.skill` | the skill whose MCP server brings it, if any |
| `call.tool.right` | the right it needs in an agent run: `read_house`, `act_on_house`, `remember`, `messages`, `web` |
| `call.args` | the arguments as a dict |
| `call.domains` | the domains the arguments name (`domain`, lower case) |
| `call.targets` | the entities the call surely reaches: from `entity_id`, from `name` (with `area`), or every entity of an `area` named alone (of the named domains, if any) |
| `call.candidates` | entities the call *could* reach and Mira cannot narrow down: for a floor, a device class or only a domain every entity (of the area and domains, if named); for a name that resolves to nothing, the entities whose names resemble it |
| `call.summary` | the call in one line, as the question names it |

### An entity

| Field | Meaning |
|---|---|
| `id`, `domain` | `cover.garage`, `cover` |
| `name` | its name in Home Assistant |
| `area`, `area_name` | its area's id and name, if any |
| `labels` | Mira's labels it carries, **by their id**: `"mira_confirm"` is the label "Mira: nachfragen" |

An `entity_id` Mira does not know (yet) is an entity with only `id` and `domain`.

**Labels by id, not by name.** A label in Home Assistant has an id and a name. The integration creates Mira's label
with the id `mira_confirm`; its name is "Mira: nachfragen" in a German house, "Mira: confirm first" in an English
one, and anyone can rename it. A hook that compares names breaks with the next language or rename; it compares the
id. Mira's labels have fixed ids: `mira_confirm` ("Mira: nachfragen") and `mira_private` ("Mira: privater Bereich",
on areas).

### `call.who`, `call.where`, `call.origin`, `call.turn`, `call.time`

| Field | Meaning |
|---|---|
| `who.person` | the person's key, empty when nobody is known |
| `who.name` | how Mira names them |
| `who.by` | how the person is known: `signed_in` (their HA user or their messenger), `voice`, `alone` (the only one at home), `conversation` (they started the conversation this turn continues); empty when nobody is known |
| `where.channel` | `voice`, `telegram`, `background`, … |
| `where.area` | the satellite's area of a voice turn, else empty |
| `where.device` | the satellite's device id |
| `origin.kind` | `conversation`, or `agent` for an agent's run |
| `origin.rights` | for an agent's run: its effective rights, as names |
| `origin.can_ask` | someone can answer a question: not in the background |
| `turn.web_read` | this turn read web content |
| `turn.foreign` | the turn has third-party text before it: web content it read, or a plan made from a web page |
| `turn.text` | the user's message of this turn |
| `time.hour`, `time.minute`, `time.weekday` (1 = Monday), `time.date` | now, local |

Persons have no role or age in Home Assistant ([benutzer.md](benutzer.md)). A hook tells persons apart by
`who.person` and `who.name`, and whether someone is known at all.

## Logging

| Log line | When | Fields |
|---|---|---|
| `tool call blocked` (warn) | the verdict is confirm or deny, as before | session, tool, outcome, `reason` (the hook that made it so: `system/web.star`), targets |
| `tool refused` / `tool blocked` (info) | the same in an agent's run: refused by `system/rights.star` (with the right it needs and the run's rights), blocked by another hook | agent, call id, tool, … |
| `hook log` (info) | a hook called `log(message, **fields)` (or `print`) | hook, message, the fields, tool |
| `hook failed` (warn) | a hook raised an error or ran too long | hook, error with line |
| `hooks loaded` (info) | at start | the chain in its order, the replaced default hooks, the number of problems |
| `hook not loaded` (warn) | a household file did not load | file, problem |

- **A plain allow has no log line**: it is the normal case on every call. `tool done` stays as it is.
- **The trace of a turn** (`mtrace`, used by the evals), the tool span (`mira.tool.blocked_reason`) and an agent's run carry
  the hook that decided, as they carried the guard's reason before.

## Errors and limits

- **A hook that fails counts as `deny`**, on every level: an error, more than the limit of steps (100,000), a return
  value that is no verdict. The model reads that a rule failed ("Die Regel household/x.star ist fehlgeschlagen"); the
  log has `hook failed` with the line. Simple and strict.
- **No `load()`, no file, no network, no clock but `call.time`, no randomness.** A hook only computes.
- **A household file that does not load** (syntax error, an error while it runs once at loading, no `DESCRIPTION`,
  no `pre_tool`) is not active. It is
  reported like a skill that cannot start: in the log, in `/v1/status` (`problems`, source `hook`), as a repair in
  Home Assistant, and on the page. A household `critical.star` that does not load leaves the default one in place.
- **Speed**: the files are compiled once when loaded. A call through all hooks takes microseconds.

## A hook file

```python
DESCRIPTION = "Eine unbekannte Stimme schließt nichts auf."

def pre_tool(call, verdict):
    if call.who.person:
        return None
    for e in call.targets + call.candidates:
        # Home Assistant's intents: HassTurnOff on a lock unlocks it.
        if e.domain == "lock" and call.tool.name == "HassTurnOff":
            return deny("Aufschließen nur, wenn ich weiß, wer spricht.")
```

- **`DESCRIPTION`** is the line the page shows; required. It is shown as written, not translated; the built-in
  hooks are written in English throughout, like the language they are written in.
- **`pre_tool(call, verdict)`** is required.
- **Other globals** (constants, helper functions) are allowed; they are frozen after loading.
- **Built-ins**: `allow`, `confirm`, `deny`, `text`, `log`; `print` writes to the log too.

## The built-in hooks

```python
# system/rights.star
DESCRIPTION = "An agent only does what it has the rights for."

def pre_tool(call, verdict):
    if call.origin.kind == "agent" and call.tool.right and call.tool.right not in call.origin.rights:
        return deny(text("no_right", call.tool.right))
```

```python
# system/background.star
DESCRIPTION = "In the background only an agent with rights changes something."

def pre_tool(call, verdict):
    if call.where.channel == "background" and not call.tool.read_only and call.origin.kind != "agent":
        return deny(text("deny_background"))
```

```python
# system/web.star
DESCRIPTION = "After web content nothing critical; keeping or passing something on only with a yes."

def pre_tool(call, verdict):
    if verdict.kind == "confirm" and call.turn.web_read:
        return deny(text("deny_after_web"))
    if call.tool.keeps_or_passes_on and call.turn.foreign:
        return confirm(text("ask_after_web"))
```

```python
# system/self.star (docs/spec/hooks-schreiben.md, Who may)
TOOLS = ["hook_save", "hook_switch", "hook_delete"]
PROTECTED = ["rights.star", "background.star", "web.star", "self.star", "ask.star", "critical.star"]

def pre_tool(call, verdict):
    if call.tool.name not in TOOLS:
        return None
    if not call.who.person:
        return deny(text("rules_unknown"))
    if call.turn.foreign:
        return deny(text("rules_foreign"))
    ...  # a protected name is refused
    return confirm(text("rules_confirm"))
```

```python
# system/ask.star
DESCRIPTION = "Ask only when someone can answer."

def pre_tool(call, verdict):
    if verdict.kind == "confirm" and not call.origin.can_ask:
        return deny(text("deny_no_confirm"))
```

```python
# default/critical.star
DESCRIPTION = "Locks, alarm panels and everything with the label mira_confirm only with a yes."

DOMAINS = ["lock", "alarm_control_panel"]

def critical(e):
    return e.domain in DOMAINS or "mira_confirm" in e.labels

def pre_tool(call, verdict):
    if call.tool.read_only or call.tool.own:
        return None
    if [d for d in call.domains if d in DOMAINS]:
        return confirm(text("critical"))
    for e in call.targets + call.candidates:
        if critical(e):
            return confirm(text("critical"))
```

### `critical.star` also says what a task may not switch

Tasks (HA automations Mira drafts, [aufgaben.md](aufgaben.md)) and the sleep timer may not switch what needs a yes in
the conversation; the integration refuses such an automation. What that is, `critical.star` says, the default one or
the household's:

- **`DOMAINS`**: the domains that are refused as a whole.
- **`critical(entity)`**: for every entity of the house; those it calls critical are refused too.

A household `critical.star` without both is a file that does not load (and the default one stays).

### What happened to the guard's settings

- **`security.confirm_domains`, `confirm_entities` and `confirm_tools` are no longer read.** What they said is
  `critical.star` now. A `mira.yaml` that still has them starts; the log says once that they are no longer read and
  what took their place. There is no carry-over of values (no data migration yet).
- **`security.confirm_ttl` stays**: it belongs to the mechanism of the pending confirmation.
- **`security.read_only_tools` stays**: it is a fact about tools (`call.tool.read_only`), not a decision.

## Skill actions

A skill whose thread ends with a Home Assistant action ([skill-aktionen.md](skill-aktionen.md)) calls it without the
model; until 2026-10-28 nothing checked it. Now the action passes the hooks as a call of its own:

- **The call**: tool name `skill_action`, `call.tool.skill` the skill, the action's data as arguments with `action`
  added; without an `entity_id` (or `domain`) the action's domain (`lock` of `lock.unlock`) counts as `domain`.
  Its targets are resolved as for any call.
- **The facts**: in a turn the turn's (who, where, web content read). Outside a turn (the yes on Mira's page, a
  field an agent filled later) the thread's person and room; someone can be asked, since the thread asks itself.
- **allow**: the action is called, as before.
- **confirm**: the thread proposes the action, as with `confirm: true` in the skill, and calls it after the yes.
- **deny**: the thread ends with the hook's reason; nothing is called.
- **The yes** (`thread_confirm`, or on the page) asks the hooks again: what they refuse now (after web content, for
  instance) stays refused; a question counts as answered by this yes.
- **Taking back** (`thread_undo`) has no question of its own: an undo the hooks refuse or would ask for is not done,
  and Mira says why.

## Where one sees the hooks

- **Mira's page, group Fähigkeiten, the page "Regeln" / "Rules"** (admins only), read only: the hooks by level, each with its
  description and its code (folded); a replaced default hook shows that the household's replaces it; a household file
  that does not load shows its problem.

## Steps

1. **The small version** (built): the hook engine with Starlark, the call with its facts, the built-in hooks, the
   household folder, problems, the page "Regeln" / "Rules". The guard keeps the mechanism and asks the hooks; the right check of
   agent runs moved from the agent runtime into `rights.star`. The guard's tests run against the hooks.
   `security.confirm_*` are no longer read.

2. **Skill actions** (built): a skill's action passes the hooks as a call of its own (see Skill actions).
3. **Writing rules on request** (built): `ACTIVE`, reloading without a restart, `system/self.star`, the skill `regel`
   ([hooks-schreiben.md](hooks-schreiben.md)).

## Later

Discussed, left out of the small version. Each comes only with a case that needs it.

- **`change`**: a hook changes the arguments (quiet hours: "only 30 %"). Needs an order in which hooks see the
  changed call and the targets resolved anew.
- **`post_tool`** with **effects** (`notify`, `event`) and a **memo** from `pre_tool` to `post_tool`: e.g. a message to
  the admins after the front door was unlocked by voice. A fixed Home Assistant event after every confirmed call was
  the smaller idea; also left out until someone asks for it.
- **Reading the house** in a hook: `state()`, `entities()`, `home()` (e.g. heating up with a window open).
- **More facts**: whether the person is an admin, whether the quiet hours are on.
- **Counters on the page**: how often a hook decided.
- **Mira writes hooks on request** (own spec): the user says what they want, Mira writes the hook, shows it in words
  and as code, tries it on examples, the user confirms. Writing a hook is a critical action, admins only, and refused
  after web content, by a system hook.
- **Skills bring hooks** (own spec).

## Deliberately not

- **Conditions in YAML** or another rule language of our own: nobody reads them well, and they grow into a language
  anyway.
- **Hooks as external programs or HTTP addresses**, as in Claude Code: too technical for a household, and a voice turn
  would wait for them.
- **Home Assistant as the hook** (a script that decides): a round trip on every call, and hard to explain.
- **Loosening a system hook**, from any level.
- **Hooks on what Mira does by herself**: the indicator light, the delivery of a message. They are no tool calls of
  the model.

## Open

1. **The yes**: matching a plain yes stays as it is (`Affirmative`). Whether that becomes a decision of the model is a
   question of its own, not of the hooks.
