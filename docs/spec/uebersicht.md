# Skills and agents at a glance

Status 2026-10-23: built (stage 1, the navigation, Lit for the new parts).

## Goal

Mira has many skills and agents by now: 19 skills, among them threads (`termin`, `kochen`), schedules
(`briefing`, `warnungen`), mail skills (`pakete`) and skills with an MCP server (`paperless`), plus the agents Mira
brings and those described in YAML. Nowhere can one see what is there: not on Mira's page, not in Home Assistant's
options. What is switched on, what did not start and why, which agent takes which model, when a schedule runs next:
today that is in `mira.yaml`, in the `SKILL.md` files and in the log.

A view on Mira's page shows it, **read only**. Changing stays where it is (`mira.yaml`, the skill folders); see Ideas
for later.

## What the view shows

### Skills

One entry per skill, in three groups:

1. **Active:** the skills that started.
2. **Not started:** switched on, but with a problem; the problem's text, as Home Assistant shows it under Repairs.
3. **Available:** a folder in `skills.dirs` that is not switched on; only name and description, so one finds new
   skills without reading the folder.

Per active skill:

| Part | Shown |
|---|---|
| Name and description | as in `SKILL.md` |
| Kind | what it brings: guide only, thread (target with fields or plan), schedules, mail, MCP server, agents |
| Settings | name, description and value; a secret one shows only whether it is set |
| Schedules | the cron expression in words (`täglich 18:00`), per person or not, the next run |
| Target | the fields (name, description, optional, check, agent) or "plan", the finish, `confirm`, the timeout |
| MCP server | address without secrets, connected or not (with the error), the tools Mira takes, missing tools, `conversation` |
| Agents | the skill's agents, as below |
| Guide | the `SKILL.md` body, folded, as text (settings filled in, secrets not) |

### Agents

One list of all agents Mira can call, in three groups by origin: **Mira's own** (`conversation.voice`,
`delegate`, `mail.read`, …), **from `mira.yaml`**, **from a skill** (with the skill's name).

Per agent:

| Part | Shown |
|---|---|
| Name and description | |
| Model | the model's name and its privacy (`local` / `cloud`), the fallback if any; `privacy: local` of the agent |
| Context | isolated or background |
| Tools | none, read only, or the list; for a skill's agent, which of them come from the skill's MCP server |
| Rights | as words (read the house, act on the house, remember, messages, web) |
| Called from | conversation (`conversation: true`; a skill's once the skill is opened), threads (which target and field), Home Assistant (`mira.agent`, every agent), other agents (whose tool list names it) |
| Limits | `max_rounds` or the model's `max_tool_rounds` |
| Runs | a link to its runs in the tab "Agenten" (filtered to this agent) |
| Not started | the problem's text, for a YAML agent that did not start |

## Navigation

Mira's page had a tab per view in one row; with the skills it would be ten. Instead, as Home Assistant's own settings
page does it:

- **A start page** with a card per group. Each card names the group's pages and says in a line or two how things
  stand: messages waiting, plans running, tasks; skills active, skills not started, skills available.
- **Inside a group** its pages are tabs on top, with the way back to the start page. A group with one page has no
  tabs.
- **The groups:**

  | Group | Pages |
  |---|---|
  | Alltag (day to day) | Nachrichten, Aufgaben, Pläne, Gedächtnis |
  | Einrichtung (setup) | Satelliten, Personen, Benachrichtigung |
  | Fähigkeiten (abilities) | Skills, Agenten |
  | Anleitung (guide) | Anleitung |

- Someone who is no admin sees the start page with the pages they may open (Pläne, Gedächtnis, Anleitung).
- Nothing on the left: inside Home Assistant the left is HA's sidebar.

## How the page is built

- The new parts (start page, navigation, skills, the agents' descriptions) are built on **Lit**, the library Home
  Assistant builds its own frontend with: small web components, no build step. Lit is one file
  (`lit-core.min.js`, BSD-3-Clause) in the binary, served with the page's own files; nothing is loaded from the
  internet.
- The components render into the page itself (no shadow DOM), so the page's styles apply to old and new parts alike.
- The older views stay as they are for now and move over one by one; until then they run inside the new frame.

## Where and for whom

- **Mira's page** (the panel in Home Assistant and `/ui`).
- **Admins only**, as the tab "Agenten": settings carry calendar names, addresses and the like; the run view is
  already admin only.
- The view reads what Mira loaded at its start (or at the last change of the configuration); it changes nothing.

## Decided

1. A new tab "Skills"; the tab "Agenten" shows the agents' descriptions above its runs.
2. Available skills (a folder, not switched on) are shown with name and description.
3. The guide text (`SKILL.md` body) is shown, folded.
4. The navigation: a start page with a card per group, inside a group its tabs on top.
5. The page's new parts are built on Lit; the older views move over one by one.

## Open questions

None at the moment.

## Ideas for later

- **Stage 2, maintaining what users maintain:** switch skills on and off and set their settings in the view or in
  Home Assistant's options (one place for settings; decide which); the view's sections override `mira.yaml` as
  `notifier` and `persons` do today; the skills reload without a restart.
- **Moving the older views to Lit,** one by one.
- **Stage 3, editing definitions:** `SKILL.md` and YAML agents in the browser, with a writable skill folder, a check
  before saving and reloading. For now the editor and git are the better place.
