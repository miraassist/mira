# Skill source – Spec

Status 2026-11-16: specified, nothing built. Decisions 1 to 4 and 15 by the maintainer; 5 to 14 taken while writing and confirmed by
him the same day. The testable rules are numbered scenarios in [skill-source.feature](skill-source.feature); each Go test
for one carries the number in its name (`TestSQ03_…`).

## Goal

Some skills are to be sold as a subscription instead of being part of the open repository. Mira stays open
(Apache 2.0) with all her building blocks; a paid skill comes from a server, the **skill source**, and only while a
subscription holds.

1. Anna takes out a subscription and gets a **subscription key**. On Mira's page, tab "Verbindungen", she enters the
   source's address and the key.
2. At her next start Mira loads the subscription's skills from the source. They show on the tab "Fähigkeiten" like
   the others; Anna switches one on like any skill.
3. The skills live only in Mira's memory: there is no file to pass on. A new version comes at the next start.
4. The subscription ends: at the next start the source refuses the key, the skills are gone, and a repair says so.

## Building blocks

The concept it hangs on: **a skill** ([Glossary](../glossary.md), Capabilities), loaded at Mira's start from the skill
directories (`skills.dirs`) by `skills.Load`, switched on by an entry under `skills` in `mira.yaml`, its problems
shown as repairs. A skill from the source is such a skill in every way once loaded: its target, agents, schedules,
MCP server, settings and connection work unchanged.

Departures, each with its reason:
- **Where it comes from:** a server instead of a folder, so that no copy lies on the user's disk (decision 2).
- **What Mira shows and keeps of it:** less than of a skill from a folder (decisions 8 to 11): its text is the thing
  paid for.

The server itself (subscriptions, payment, the watermark) is not part of Mira and not of this repository; this spec
names only what Mira expects of it (section "The source's interface").

## Decisions

By the maintainer (2026-11-16):

| # | Topic | Decision |
|---|---|---|
| 1 | The line | Mira and every building block stay open. What is sold is the feature as a skill (its instructions, plan, tasks); a building block a paid skill needs goes into the open repository |
| 2 | How it is protected | The skill is loaded from a server with the subscription, not installed as a file. Copying is made hard, not impossible: the Mira service is open, and the skill's text goes to the model. Whoever wants it must change Mira's code on purpose; passing on a file is no longer possible |
| 3 | The watermark | The source marks each subscription's copy of a skill, so a copy that turns up names its subscription. Done by the server; Mira passes the text on unchanged |
| 4 | A service behind it | A skill may use a service of the provider (a remote agent, an MCP server) where it needs one, but need not |
| 15 | A start without the source | A source that cannot be read at Mira's start means its skills are missing until the next start where it can; no loading during the run, no copy on disk (2026-11-16, 1A) |

Taken while writing, confirmed by the maintainer (2026-11-16):

| # | Topic | Decision |
|---|---|---|
| 5 | One source | `skills.source` holds one address and one key. A second provider is not foreseen; a list can come later without breaking the setting |
| 6 | When Mira loads | At her start only, before the skills are loaded. No polling: a new version, a new skill or an ended subscription takes effect at the next start. Reason: skills are wired at the start (MCP servers, tools, schedules) |
| 7 | Switched on like any skill | A skill from the source runs only when it has an entry under `skills` in `mira.yaml` (the tab "Fähigkeiten" writes it), as a skill from a folder; there it is marked "aus dem Abo" |
| 8 | Never on disk | Mira keeps a source's skill in memory only: no cache, no copy in the database, no export. A source that cannot be read means its skills are missing until a start where it can (decision 15) |
| 9 | Not shown | Mira's page shows a source's skill with name, description, settings and problems, never its text; editing it is not offered |
| 10 | Not in the history | `skill_open` for a source's skill stores in the turn's history a placeholder ("skill <name> from the subscription, opened") instead of the text; when the history is sent to the model, Mira puts the text back in from memory. Reason: tool results are kept in the database (`chat_messages`), where the text could otherwise be read |
| 11 | Not in the log | An error in a source's skill names the skill, the source's address, the file and the line, but not the text (an exception to decision 5 of [Silent fallbacks](fallbacks.md): the text is what is protected) |
| 12 | A name taken twice | A source's skill with the name of a skill from a folder is left out; the problem names both. The user's own skill wins: it is the one he can see |
| 13 | What Mira sends | The key and Mira's version, nothing about the house or its persons. The source learns who loads which skills and when Mira starts |
| 14 | Only https | The address must be `https`; `http` only for an address on the same machine (tests). Reason: the key and the skills travel over it |

## The source's interface

One request at Mira's start:

```
GET <url>/v1/skills
Authorization: Bearer <subscription key>
Mira-Version: 0.25.0
```

Answer `200`, JSON:

```json
{
  "skills": [
    {
      "name": "angehoerige",
      "version": "1.2.0",
      "files": { "SKILL.md": "---\nname: angehoerige\n…" }
    }
  ]
}
```

- `files` maps a path inside the skill's folder to its text; only text files (as a skill folder holds today:
  `SKILL.md`, an MCP server's description, prompts). A path with `..` or a leading `/` is refused.
- `401` or `403`: the key is not (or no longer) valid. `404`: no skill source at this address.
- The answer holds the subscription's copy (decision 3); Mira does not look for the watermark.

## What it does

### A. The setting

- `mira.yaml`: `skills.source: { url: https://…, key: ${MIRA_SKILL_SOURCE_KEY} }`; the key goes into the secrets file
  (as every secret the page writes).
- Mira's page, tab "Verbindungen": a card "Abo" with the address and the key, a hint that it takes effect after a
  restart, and the state of the last load (loaded n skills, or the problem).

### B. Loading at the start

1. Before `skills.Load`: if `skills.source` is set, one `GET <url>/v1/skills`.
2. Check the answer: JSON, every skill with `name` and `SKILL.md`, every path allowed.
3. Hand the files to `skills.Load` as one more skill directory that lives in memory (an `fs.FS`), after the folders
   from `skills.dirs` (decision 12).
4. From here a source's skill is loaded, switched on and wired like any other.

What it costs: one HTTPS request at the start, its answer (all of the subscription's skills, typically tens of
kilobytes) kept in memory for the service's life. The start waits for it, at most the retries' bound (below). No
turn waits for the source.

### C. Opening a source's skill

`skill_open` returns the text as for every skill; the history keeps the placeholder (decision 10). The model sees the
text at every turn that carries it, as with any skill.

## Errors

| Fails | What happens | Who learns of it |
|---|---|---|
| The source cannot be reached, a timeout, `429`, `5xx` | Retried at the start: three tries, waiting 2 s and 8 s (a `Retry-After` is kept, up to 30 s); then Mira starts without the source's skills ([Retries](retry.md)) | the log per try (address, error, next wait); after the last a repair "Die Skills aus dem Abo konnten nicht geladen werden; Mira versucht es beim nächsten Start wieder" |
| `401`, `403` | Not retried; Mira starts without them | a repair "Dein Abo ist abgelaufen oder der Schlüssel stimmt nicht", and the log with the address and the status |
| `404`, an answer that is not the expected JSON | Not retried; Mira starts without them | a repair naming the address; the log with the address, the status and the answer's start (the answer is not yet a skill's text) |
| A skill in the answer without `name` or `SKILL.md`, or with a path refused | That skill is left out, the others load | a problem of the source naming the skill and the path |
| A source's skill does not load (its `SKILL.md` is wrong) | As for every skill: left out, a problem | the problem names the skill, the file and the line, not the text (decision 11) |
| A name taken twice | The source's skill is left out | a problem naming both (decision 12) |
| `skills.source.url` not `https` and not on this machine | The source is not asked | a repair naming the address |

## Not part of it

- The server: subscriptions, payment, the watermark, which skills a subscription holds.
- Protection against a changed Mira: not possible with an open service (decision 2).
- Several sources, loading during the run, a copy kept for a start without the source.
- Binary files in a skill (sounds, pictures).

## Steps

1. **The setting and loading** (A, B; decisions 5 to 8, 12 to 14): `skills.source` in `mira.yaml` and the secrets
   file, the request with its retries and errors, the skills from memory in `skills.Load`, the repairs. A fake
   source in `testenv/` for the tests. SQ-01 to SQ-08.
2. **Protecting the text** (C; decisions 9 to 11): the placeholder in the history, the page without the text, the
   errors without it. SQ-09 to SQ-11.
3. **The card "Abo" on the tab "Verbindungen"** (A), German and English. SQ-12.
4. **The guide** (German and English): what a subscription brings, where the key goes, what happens when it ends.
