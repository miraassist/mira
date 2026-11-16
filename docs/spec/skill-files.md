# Skill files – Spec

Status 2026-11-16: specified; steps 1 to 3 built 2026-11-16, step 4 (the guide) open. Decisions 1 to 4 by the
maintainer; 5 to 16 taken while writing, to be confirmed by him. The testable rules are numbered scenarios in
[skill-files.feature](skill-files.feature); each Go test for one carries the number in its name (`TestSB03_…`).

## Goal

A skill is a folder named as the skill with a `SKILL.md`. Everything else in the folder, in any subfolder
(`blueprints/`, `prompts/`, `scripts/`, …), is carried along with it as it is, wherever the skill comes from: a skill
directory on disk (`skills.dirs`) or a [skill source](skill-source.md). These are the **skill files**
([Glossary](../glossary.md)).

The first use of them: a skill brings Home Assistant **blueprints** and declares **automations** made from them. Mira
saves the blueprints into Home Assistant and makes the automations, with inputs from the skill's settings; when the
skill comes in a new version, she brings the new blueprints and Home Assistant's automations follow them.

- Anna switches on the skill "zu-ruhig" (a paid skill from the source) and sets its window to 22:00. At Mira's next
  start Home Assistant has the blueprint `mira_skills/zu-ruhig/zu_ruhig.yaml` and the automation "Zu ruhig" made from
  it; it shows among Mira's tasks.
- A month later the provider improves the blueprint. At the next start Mira saves the new version; Home Assistant
  reloads the automation, which now runs the new version with Anna's inputs.
- Anna changes the window to 23:00 on Mira's page; Mira restarts and saves the automation with the new input.

## Building blocks

The concepts it hangs on:

- **A skill** ([Glossary](../glossary.md), Capabilities), loaded by `skills.Load` at Mira's start, switched on by its
  entry under `skills` in `mira.yaml`, its problems shown as repairs.
- **Mira's blueprints** ([Device watch](device-watch.md), decisions 1, 7, 17, 22, 23, 27; `internal/watch`): saved by
  the service with `blueprint/save` when Home Assistant lacks them or has another version (the number after
  `mira-version:` in the description), with `allow_override` for a changed one; Home Assistant reloads the automations
  using it itself (decision 22).
- **Mira's own automations** ([HACS default](hacs-default.md), 3 and 4; `internal/ha/automation`): saved through Home
  Assistant's automation config API, loaded by `automation.reload` with the id, labelled "Mira" by the integration,
  noted in `own_automations` (kind `task`).

Departures, each with its reason:

- **A blueprint's version is read as Mira's are** (`mira-version: N` in its description), not compared by content:
  `blueprint/list` gives the metadata, not the file, and Home Assistant writes the file anew on saving (it dumps the
  parsed YAML), so a content compare would need a download per blueprint (decision 9).
- **The interface is checked before an update** (decisions 2, 10): Mira's own blueprints are checked by her own
  tests; a skill's come from elsewhere and could break the automations Home Assistant already runs.
- **A skill automation is not checked by the integration's `mira/automation/validate`** (decision 14): that check
  guards what a model drafted; a skill automation is the skill's, written by its author like the skill's actions.

## Decisions

By the maintainer (2026-11-16):

| # | Topic | Decision |
|---|---|---|
| 1 | Deleting | Nothing is deleted: a skill switched off, a subscription ended, a blueprint or an automation the skill no longer declares — the blueprints and automations stay in Home Assistant as they are |
| 2 | The interface | A blueprint's interface is its **inputs**. On an update it stays constant (compatible, decision 10), so that Mira can make every automation from it anew |
| 3 | The building block | General: every file of the skill's folder, in any subfolder, comes with the skill. Blueprints are one application |
| 4 | The transfer | Specified once, on the server's side: `mira-skills: REQUIREMENTS.md, Interface to Mira`. This spec and [Skill source](skill-source.md) refer to it |

Taken while writing, to be confirmed by the maintainer (2026-11-16):

| # | Topic | Decision | Reason |
|---|---|---|---|
| 5 | Where a skill's files are in Mira | In the skill's folder (a skill directory) or in memory (a skill source); `skills.Load` reads both through one file system (`fs.FS`), so the same skill loads the same either way | One way of loading; the source's skills never touch the disk ([Skill source](skill-source.md), decision 8) |
| 6 | Which files are skill blueprints | The files `blueprints/*.yaml` directly in the skill's folder (not deeper), each a Home Assistant automation blueprint (`domain: automation`) | A fixed place the author knows; script blueprints are not needed yet |
| 7 | Where Home Assistant keeps them | `blueprints/automation/mira_skills/<skill>/<file>.yaml`, saved with the path `mira_skills/<skill>/<file>` | Outside `mira/`, which is Mira's own (`internal/watch` reads every path under `mira/` as hers); one folder per skill, so two skills cannot collide. `blueprint/save` makes the folders (checked on the test HA, row 1) |
| 8 | When Mira saves them | At her start, once Home Assistant is connected, for every switched-on skill; in the background, no turn waits | Skills are loaded at the start ([Skill source](skill-source.md), decision 6); push instead of polling: a new version comes with a start |
| 9 | When a blueprint counts as changed | Its `mira-version:` differs from the one Home Assistant lists. Rule for authors: a changed blueprint gets a higher number | As Mira's own (Device watch, decision 7); see Departures |
| 10 | What a compatible update is | Every input the old blueprint has is still there with the same selector type; an entity selector's domains are not narrowed; an input with a default keeps one; a new input has a default. Anything else is incompatible (section The interface) | Every automation made from the old blueprint, by Mira or by the user, must still run unchanged |
| 11 | An incompatible update | Not saved: the old blueprint stays and keeps working; a problem of the skill names the skill, the blueprint and the inputs. Rule for authors: an incompatible change is a new blueprint file | Breaking automations that run is worse than an old version |
| 12 | How a skill declares an automation | `metadata.mira.automations`: a list of `id`, `blueprint` (a file under `blueprints/`), `alias` (optional) and `inputs`, whose values may hold the skill's settings as `{{ name }}` | The placeholders skills already use; the settings are the one truth (`mira.yaml`) |
| 13 | When Mira makes an automation anew | At the start, when Home Assistant does not have it, or when its `use_blueprint` (path and inputs) differs from what the declaration and the settings give now. Only `use_blueprint` is replaced; the alias and the description the automation has stay. A changed blueprint reaches the automation through Home Assistant's own reload (decision 9), not by saving it | A changed setting restarts Mira (Mira's page asks for the restart), so the start covers it; the user may rename it |
| 14 | No `mira/automation/validate` | The skill automation is saved as Mira's own automation is (`automation.Save`, label "Mira", noted as hers, kind `task`), without the integration's check | See Departures |
| 15 | Inputs without a value | An input whose value is one placeholder of a setting that is empty is left out: the blueprint's default applies. A required input (no default) without a value is a problem of the skill; that automation is not made | The blueprint's defaults are the author's; a missing value is said, not guessed |
| 16 | A source's blueprints in Home Assistant | A skill blueprint of a source's skill is written into Home Assistant's config folder like any blueprint: it must be, Home Assistant runs it. What the source protects is the skill's text ([Skill source](skill-source.md), decision 2); a blueprint is code that runs in the house | Without it a paid skill could bring no automation |

## The path, end to end

What happens from the skill server to an automation running in Home Assistant, and who does each step.

1. **On the server.** The skill is a folder in the server's skill directory (`skills/<skill>/` of the private
   repository): `SKILL.md`, its blueprints under `blueprints/`, any other file. The server reads the folder with every
   subfolder (hidden ones skipped) and checks that every file is UTF-8 text (`mira-skills: REQUIREMENTS.md, Skills`).
2. **The transfer.** At Mira's start one `GET <url>/v1/skills` with the subscription key; the answer maps every file
   of each skill folder, by its path relative to the folder (`blueprints/zu_ruhig.yaml`), to its text. The interface
   is specified once: `mira-skills: REQUIREMENTS.md, Interface to Mira`. Mira's side of the request (retries, errors,
   repairs): [Skill source](skill-source.md), B.
3. **In Mira.** A source's skills become one more skill directory that lives in memory (an `fs.FS` of the paths and
   texts), after the folders of `skills.dirs`; a skill being developed lies in such a folder on disk. Either way the
   skill's files are one file system rooted at the skill's folder (decision 5).
4. **`skills.Load`.** Reads `SKILL.md` from that file system, fills the settings, reads the declared automations
   (decision 12) and checks that each names a blueprint file the skill has. A skill that does not load is left out
   with its problem, as today.
5. **The blueprint in Home Assistant.** Once Home Assistant is connected, Mira lists its automation blueprints
   (`blueprint/list`, one call for all skills) and, per skill blueprint of a switched-on skill, compares the version
   (decision 9): missing → `blueprint/save` with the path `mira_skills/<skill>/<file>`; another version → the
   interface check (section The interface), then `blueprint/save` with `allow_override`; the same version → nothing.
6. **The automation in Home Assistant.** Per declared automation whose blueprint is in Home Assistant in the skill's
   version: the inputs are checked against the blueprint (every input it requires is given, none it does not have);
   Mira reads the automation by its id (`GET /api/config/automation/config/<id>`); missing or with another
   `use_blueprint` → saved as one of Mira's own automations (`automation.Save`: config API, `automation.reload` with
   the id, label "Mira", noted as hers). The automation is `{alias, description, use_blueprint: {path, input}}` with
   the id `mira_skill_<skill>_<id>`.

### The update

1. **On the server** the author changes the blueprint and raises its `mira-version:` (decision 9); for a change of
   its interface he writes a new file instead (decision 11). The skill's `metadata.version` goes up as the server
   requires.
2. **Mira's next start** loads the new skill (path, steps 2 to 4).
3. **The interface check**: Mira compares the inputs Home Assistant lists for the old blueprint with the new file's.
   Compatible → step 4. Incompatible → nothing is saved, the old blueprint stays, the automations keep running on
   it; a problem of the skill names the blueprint and the inputs (decision 11).
4. **`blueprint/save` with `allow_override`.** Home Assistant writes the file and reloads all automations
   (`automation.reload`, its event `automation_reloaded`): every automation made from the blueprint, Mira's and the
   user's, runs the new version with its inputs (checked on the test HA, rows 2 and 3).
5. **The automation**: Mira saves it again only when its declaration or the settings give other inputs than it has
   (decision 13), e.g. a new input the new version of the skill fills.

A changed setting takes the same way from step 2 of the update: Mira's page asks for a restart after a change, and
the start compares the inputs.

### Where each file is stored

| Where | The skill's files | Its blueprints | Its automations |
|---|---|---|---|
| The skill server | `skills/<skill>/…` in the server's repository (`skills_dir`) | `skills/<skill>/blueprints/<file>.yaml` | — |
| On the way | The answer of `GET /v1/skills`: `files`, path → text | `files["blueprints/<file>.yaml"]` | — |
| Mira, from a source | In memory only, for the service's life | in memory | — |
| Mira, from a folder | `<skills.dirs entry>/<skill>/…` | `<skill>/blueprints/<file>.yaml` | — |
| Home Assistant | — | `config/blueprints/automation/mira_skills/<skill>/<file>.yaml` | `automations.yaml`, id `mira_skill_<skill>_<id>`; noted in Mira's `own_automations` |

### What a skill writes

```yaml
---
name: zu-ruhig
description: "Meldet sich, wenn es nachts zu ruhig im Haus ist."
metadata:
  version: 1.2.0
  mira:
    settings:
      window_end: { description: "Bis wann es ruhig sein darf, z. B. 07:00" }
    automations:
      - id: nachts
        blueprint: zu_ruhig.yaml
        alias: "Zu ruhig in der Nacht"
        inputs:
          window_end: "{{ window_end }}"
          minutes: 30
---
```

- `id`: small words with `-` or `_`; unique within the skill.
- `blueprint`: a file under the skill's `blueprints/`.
- `alias`: the automation's name in Home Assistant when Mira makes it; without one, the blueprint's `name`.
- `inputs`: the blueprint's inputs. A value that is exactly one placeholder (`"{{ window_end }}"`) takes the setting's
  value as it is written in `mira.yaml` (a number stays a number); an empty setting leaves the input out (decision
  15). Other text has its placeholders filled. Secret settings are filled too: they go to Home Assistant, never to a
  prompt.
- The blueprint has `mira-version: N` in its description.

Nothing changes for the model: a skill automation is one of Mira's tasks to the user, listed and changed by the tools
that list and change tasks.

## The interface

A blueprint's interface is the map of its inputs (`blueprint.input`; inputs inside a section count as inputs). Per
input: its name, its selector's type (`entity`, `number`, `text`, `time`, …), for an entity selector its domains (from
`domain` or `filter`), and whether it has a default.

An update from the blueprint Home Assistant lists (old) to the skill's file (new) is **compatible** when:

1. every input of the old is in the new (none removed or renamed);
2. its selector type is the same;
3. for an entity selector, every domain the old allows the new allows too (an old one without domains: the new has
   none either);
4. an input with a default in the old has one in the new;
5. every input only the new has has a default.

Anything else is **incompatible**: the problem names each input and what changed ("removed", "selector text instead
of number", "no default"). A blueprint Home Assistant lists with an error (a file it cannot read) has no readable
interface: it is replaced without the check.

## What it costs

At the start, in the background once Home Assistant is connected; no turn waits for it.

| Step | Calls to Home Assistant |
|---|---|
| Listing | one `blueprint/list` for all skills (an answer with the metadata of every automation blueprint in the house, kilobytes) |
| Per skill blueprint, unchanged | none |
| Per skill blueprint, new or changed | one `blueprint/save`; for a changed one Home Assistant reloads all automations once per saved blueprint (its own reload) |
| Per skill automation | one `GET` of its config |
| Per skill automation, new or changed | one `POST` of the config, one `automation.reload` with its id, one `mira/automation/label` (as every task) |

It grows with the number of skill blueprints and skill automations of the switched-on skills, typically a few. Each
call is retried as Mira's blueprints are (below). Reading the skill files costs nothing extra: they are read with
`SKILL.md` (from a folder) or are already in memory (from a source).

## Errors

| Fails | What happens | Who learns of it |
|---|---|---|
| `metadata.mira.automations` cannot be read, an `id` is wrong or twice, a `blueprint` the skill does not have | The skill does not load (as any error of `SKILL.md`) | a problem of the skill naming the automation and the file; the log |
| A skill blueprint cannot be read: not YAML, not `domain: automation`, no `mira-version:` | Not saved; the skill's automations from it are not made or changed | a problem of the skill naming the file and the reason; the log (`skill blueprint not usable`, skill, file, error) |
| `blueprint/list` fails: a lost connection or a timeout | Retried as Mira's blueprints (Device watch, decisions 17 and 27): three tries, waiting 1 s, then 2 s; then nothing is saved this start | the log per try (what, attempt, wait, error); after the last, an error line and a problem of each skill with blueprints ("Home Assistant could not be asked; at the next start again"); its answer cannot be read: the log with the answer's start |
| `blueprint/save` refused, or fails after its retries | That blueprint is not saved: a new one is missing, a changed one stays in its old version; its automations are not made or changed | the log with the path and Home Assistant's answer; a problem of the skill naming the blueprint |
| An incompatible update | Not saved; the old version stays and works; its automations are not changed | a problem of the skill naming the blueprint and each input; the log (`skill blueprint refused`, skill, blueprint, inputs) |
| An automation's input the blueprint does not have, or a required one without a value | That automation is not made or changed | a problem of the skill naming the automation, the blueprint and the input |
| Reading the automation fails (not a `404`) after its retries | That automation is not made or changed this start | the log with the id and the error; a problem of the skill |
| Saving the automation fails (refused, not loaded, not labelled) | As for a task (`automation.Save`): not saved, or saved without being loaded or labelled | `automation.Save` logs it; a problem of the skill naming the automation and the error |
| Mira's token is not an admin's | `blueprint/save` and the config API are refused | as above; the repair for an access ([HACS default](hacs-default.md), 3) says what to do |

A problem of the skill does not stop the skill: it runs, without the blueprint or the automation. It shows as the
skill's repair until a start where it is gone.

## Checked on the test HA

| # | What | Call | Signal | Checked |
|---|---|---|---|---|
| 1 | A blueprint in a folder per skill | `blueprint/save` with path `mira_skills/probe/probe` | `success`, `overrides_existing: false`; Home Assistant made `blueprints/automation/mira_skills/probe/probe.yaml` | 2026-11-16, 2026.11.1 |
| 2 | An automation from it | `POST /api/config/automation/config/<id>` with `use_blueprint`, `automation.reload` with the id; `GET` of the config | `{"result":"ok"}`; an event of the blueprint fired; `GET` gives `{id, alias, description, use_blueprint}`, `404` for an unknown id | 2026-11-16, 2026.11.1 |
| 3 | A changed blueprint reaches the automation | `blueprint/save` with `allow_override` | `overrides_existing: true`, then the event `automation_reloaded`; the next run carries the new version's data | 2026-11-16, 2026.11.1 |

## Not part of it

- Deleting blueprints or automations (decision 1).
- Script blueprints; blueprints deeper than `blueprints/`.
- Binary skill files (the server delivers only text).
- Loading during the run: a new version comes with a start.

## Steps

1. **Skill files** (decisions 3 to 5): `skills.Load` over file systems, a skill's files with it, the same from a
   folder and from a source. Needs [Skill source](skill-source.md), step 1. SB-01, SB-02.
2. **Skill blueprints into Home Assistant** (decisions 6 to 11): listing, saving, the version, the interface check,
   the problems. SB-03 to SB-11.
3. **Skill automations** (decisions 12 to 15): the declaration, making and updating at the start, the problems.
   SB-12 to SB-21.
4. **The guide** (German and English), chapter on writing a skill: the skill's files, blueprints and automations, the
   rules for authors (the version, a new file for an incompatible change).
