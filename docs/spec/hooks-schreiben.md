# Writing hooks on request – Spec

Status 2026-10-28: built (steps 1 to 3); the guide's section and the demo video are next. Scenarios: [hooks-schreiben.feature](hooks-schreiben.feature). Builds on
[Hooks](hooks.md).

## Goal

A household should not have to write Starlark to get a rule. It says what it wants, Mira writes the hook, says in
words what it does, and switches it on after a yes:

1. Anna: "Merk dir eine Regel: Wenn du nicht weißt, wer spricht, schließt du nichts auf."
2. Mira lets a helper write the hook and checks that it loads.
3. Mira: "Ab jetzt schließe ich nichts auf, wenn ich nicht weiß, wer spricht. Soll ich die Regel einschalten?"
4. Anna: "Ja." Mira: "Die Regel ist aktiv. Getestet habe ich sie nicht; probier sie am besten einmal aus."
5. An unknown voice: "Schließ die Haustür auf." Mira: "Aufschließen nur, wenn ich weiß, wer spricht."

The rule applies at once, without a restart. It shows on Mira's page under Capabilities → Rules, with Anna's words
in its head.

## Decided (2026-10-28)

| # | Question | Decision |
|---|---|---|
| 1 | How Mira writes the hook | A skill `regel` with an agent `regel.schreiben`; the hooks' reference is in the skill, not in every prompt |
| 2 | Who may create, change, switch and delete rules | Every person Mira knows, however she knows them. Rules Mira writes can only make her stricter, and switching one off leads back to normal; the worst case is a nuisance, not a danger. The rule's head says who asked |
| 3 | Language of a rule Mira writes | Code and comments in English; `DESCRIPTION` and what the rule says to the model (the deny reason) in the household's language |
| 4 | The model that writes the code | The strongest one configured, the one `delegate` takes; no new setting |
| 5 | Reloading | Mira watches the folder `hooks/`: a file she writes or someone changes by hand applies at once, without a restart |
| 6 | Checking a new rule | It must load and not fail on a first call; no automatic comparison for now. Mira says that the rule is not tested and suggests trying it |
| 7 | Switching a rule off | A line `ACTIVE = False` in the file itself; the file stays the one truth |
| 8 | The default `critical.star` | Cannot be switched off and is never written by Mira; it can still be replaced by hand ([hooks.md](hooks.md)). The use case is a household rule that normally holds and just now should not |

## Rules in the file

```python
# Written by Mira on 2026-10-28 for Anna:
# "Wenn du nicht weißt, wer spricht, schließt du nichts auf."
DESCRIPTION = "Ohne bekannte Stimme schließe ich nichts auf."
ACTIVE = True

def pre_tool(call, verdict):
    if call.who.person:
        return None
    for e in call.targets + call.candidates:
        # Home Assistant's intents: HassTurnOff on a lock unlocks it.
        if e.domain == "lock" and call.tool.name == "HassTurnOff":
            return deny("Aufschließen nur, wenn ich weiß, wer spricht.")
```

- **`ACTIVE`** (new for every hook, optional): `False` means loaded but not in the chain. Without the line a hook is
  active, so every hook written before stays as it is. The page shows an inactive hook as "off".
- **The head comment** says who asked, when, and in which words. It is part of the file, so it stays with the rule
  when someone edits it by hand.
- **The file name** comes from the agent, in English, from the rule's content (`unknown_voice_no_unlock.star`); an
  existing name gets a number.

## The skill `regel`

A skill like the others (`skills/regel`, German, [skill-sprachen.md](skill-sprachen.md)): its description makes the
model open it on "Merk dir eine Regel", "Ab jetzt sollst du nie …", "Welche Regeln gibt es?", "Heute Abend gilt die
Regel mit der Musik nicht". No fixed phrases in the code.

- **The target** for a new rule: the field `wunsch` (what the user wants, in their words), the field `regel` filled
  by the agent `regel.schreiben` and checked with `hook.check`, the finish `hook.save`, which proposes the rule and
  saves it after the yes.
- **Its guide** tells the model: ask back when the wish is unclear (which device, which person, from when); never
  fill `regel` itself; when the agent could not write the rule, ask how the user means it and enter the new wording
  as `wunsch` (the agent then writes again); after the yes say that the rule is active and not tested.
- **The tools** it brings (offered once the skill is open):
  - `hook_list`: the rules with name, description, on or off, who asked; reading only.
  - `hook_switch(name, on)`: switches a rule on or off (the line `ACTIVE`).
  - `hook_delete(name)`: deletes a household rule.
  - `hook_reference` and `hook_try`, for the agent (both only read).

## The agent `regel.schreiben`

- **Context**: background (it sees the house structure; an isolated agent has no tools). **Model**: `model: delegate` in the skill, the model mira.yaml names for `delegate`; a
  skill's agent may name one of Mira's own agents instead of a model, and takes its model when it runs.
- **Tools**: `hook_reference` gives the hooks' reference, embedded in the binary (`internal/hooks/reference.md`: what
  `call` holds, the verdicts, Home Assistant's facts such as which intent unlocks a lock, labels by id, examples, the
  limits), and the house: areas, persons by key, the entities of the domains rules are mostly about.
  `hook_try(code)` says whether the code loads and passes a first call, or the error; the agent improves the code
  until it does, at most three times (its instructions).
- **Input**: the wish.
- **Output** (JSON): `file`, `summary` (one sentence for the user, in the wish's language: what the rule does),
  `code` (the whole file, with `DESCRIPTION`). Or `error`: one sentence why it cannot be a rule (a rule only prevents
  or asks; it cannot switch or remind).

## Checking: `hook.check`

A check of the field `regel` (a building block of the core, as the threads' other checks):

1. The answer is the agent's JSON; an `error` from the agent is refused with its sentence.
2. The file name is lower case letters, digits and `_`, ends in `.star`, and is neither one of the system hooks nor
   `critical.star`. A name that is taken gets a number (`tuer_2.star`).
3. The code compiles with the same engine that loads the hooks, with `DESCRIPTION` and `pre_tool`.
4. Two calls through it: a harmless read by someone nobody knows, and switching a light by a known person over
   Telegram. The hook must not fail on them.

If the check refuses, nothing is proposed: the thread asks the user how they mean the rule, and a new `wunsch` makes
the agent write again.

The proposal is the agent's `summary` and the question whether to switch the rule on. It names no tested cases,
since none were tested.

## Saving: `hook.save`

- Writes `hooks/<file>` with the head comment, `DESCRIPTION`, `ACTIVE = True` and the code.
- The hooks are loaded anew at once (see Reloading); the log says `hook written` with the file, the person and the
  wish.
- Mira says: active, not tested, try it.

## Reloading

- Mira watches the folder `hooks/` (and the default hooks do not change while she runs). A file that is written,
  changed, renamed or deleted makes her load the household's hooks anew, after a short pause so that an editor's
  several writes count once.
- The new chain replaces the old one in one step; a call in flight finishes with the old one.
- A file that does not load is reported as at the start (log, `/v1/status`, a repair in Home Assistant, the page);
  the others apply. The problem goes away with the next good version of the file.
- What `critical.star` says a task may not switch is read anew with it.

## Who may: the hooks protect themselves

Saving, switching and deleting a rule are calls of their own (`hook_save`, `hook_switch`, `hook_delete`) and pass the
hooks like every tool call. A new system hook `self.star`:

- **Only a known person**: someone Mira knows (`who.person`), however she knows them. An unknown voice is refused.
  Why that is enough: a household rule can only make Mira stricter (the strictest verdict wins), and switching one off
  or deleting it leads back to how Mira behaves anyway; `critical.star` and the system hooks stay out of reach. No
  admin flag, nothing new from the integration.
- **Always with a yes**: for a new rule the thread's proposal is that question; switching and deleting ask as every
  critical call.
- **Never after web content or other third-party text** (`turn.foreign`): a web page or a mail must not be able to
  slip in a rule or switch one off.
- **Only household rules**: never a system hook, never `critical.star`.

## Where one sees it

- **Mira's page, Rules**: a rule Mira wrote shows its head comment in its code; an inactive one is marked "off".
- **The guide**: a short section "Mira eine Regel geben" in the chapter "Eigene Regeln", with the example above.

## Steps

1. **`ACTIVE` and reloading** (built): the line `ACTIVE`, watching the folder (every 2 seconds, a change counts once
   it held for one more look), the page's "off".
2. **The system hook `self.star`** and the tools `hook_list`, `hook_switch`, `hook_delete` (built).
3. **The skill `regel`** with the agent `regel.schreiben`, the check `hook.check` and the finish `hook.save`; the
   reference `internal/hooks/reference.md` (built).
4. **The guide** and the demo video.

## Later

- **An automatic comparison** before the yes: what the rule changes, by the facts it reads (equivalence classes of
  entities and persons) and by the house's real calls of the last weeks. Discussed 2026-10-28, left out for now.
- **Rules for a time** ("heute Abend keine laute Musik"): a rule that switches itself off.
