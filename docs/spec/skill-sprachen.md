# Skills in several languages

Status 2026-10-26: first step built: a translation's description and guide, in `skill_open`, `thread_start` and a
thread's context; question 1 decided (A). Settings, fields, schedules and agents in a translation, the skills view,
`languages` and questions 2–4 are open. First translation: `quiz`. See also [Language](sprache.md) (which language Mira speaks when)
and the guide, "Eigenen Skill erstellen".

## Goal

Mira speaks the language that is spoken (sprache.md), but every skill is written in German. An English-speaking
household gets German skill descriptions in the prompt, in the skills view and in Home Assistant's repairs, and the
model has to translate the guide while it works. A skill should be able to bring its texts in further languages,
without copying the whole skill.

## Two kinds of content

A skill has parts that are the same in every language, and texts:

| Same in every language (the skill's **metadata**) | Depends on the language (its **texts**) |
|---|---|
| name, settings' names, `required`, `secret` | description, `chapter` (the guide's chapter about the skill, linked on Mira's page) |
| schedules: `cron`, `per_person` | the guide (the Markdown body) |
| target: fields' names, `check`, `enum`, `optional`, `agent`, `input`; finish and undo with their data; `confirm`, `timeout`, `plan` | settings' and fields' descriptions |
| mail, MCP server, privacy | schedules' `task` texts |
| agents: name, model, context, tools, rights, `conversation`, `max_rounds`, schemas | agents' `description` and `instructions` |

Copying a skill per language (`skills/de/…`, `skills/en/…`) would copy the metadata too; the copies drift apart as
soon as one changes a time or an action in one of them. One file with both languages mixed was rejected as hard to
read.

## A translation file next to the skill

- `skills/termin/SKILL.md` stays the whole skill: metadata and texts in its **own language** (`metadata.mira.language`,
  German if not said).
- `skills/termin/SKILL.en.md` holds **only texts**: frontmatter with the translated texts by key, and the translated
  guide as its body.

  ```markdown
  ---
  description: Add an event to a calendar ("Put the dentist in for Friday at 3 pm", …).
  settings:
    calendar: { description: The calendar … }
  target:
    fields:
      title: { description: The event's title, short, e.g. Dentist }
      start: { description: … }
  schedules:
    - task: >-
        …
  agents:
    kochen.rezept: { description: …, instructions: … }
  ---
  # Adding an event
  …
  ```

- **Keys** are those of `SKILL.md`: settings and fields by name, agents by name, schedules by their order.
- **Placeholders** (`{{ calendar }}`, `{{ person }}`) work in the translation as in the skill.
- **What a translation may not do:** change metadata. A key that is not a text (`cron`, `action`, `tools`, …), or a
  setting, field, agent or schedule that `SKILL.md` does not have, is a problem of the skill, shown under Repairs; the
  skill starts with its own language for that language.

## Which text when

- The skill's texts follow the **language of the turn** (sprache.md): a conversation in English gets the English
  description in the prefix and the English guide on `skill_open`; a schedule runs in the language last spoken in
  the house (or per person, for `per_person`), as Mira's own texts do.
- **Without a translation** for that language the skill's own texts apply. A skill without any translation works as
  today.
- **Mixed is fine:** a key the translation leaves out keeps the skill's own text; a translation may cover the
  description and the guide only.
- The skills view on Mira's page shows the texts in the page's language.

## Skills that belong to a country

Some skills depend not on text but on a country: `ferien` (German states, German school holidays), `warnungen` (NINA,
German district keys), `nachrichten` (tagesschau.de), `fussball` (mostly German leagues). A translation does not
make them fit elsewhere; their settings and interfaces would be different.

- A skill may say for which languages it is meant: `metadata.mira.languages: [de]`. In a conversation in another
  language Mira does not offer it (it is not in that prefix); the skills view shows it as "only in German".
- An English counterpart with other sources is a **skill of its own** (`news` with the BBC), with its own metadata
  and `languages: [en]`.

**The rule:** only the text differs, even if it names another source (a recipe site in English for `kochen`) → a
translation. Settings, schedules, actions or interfaces differ → a skill of its own.

## Built (first step, 2026-10-26)

- **The skill's own language** is `metadata.mira.language` (decided: A), German if not said; a language Mira does
  not speak is a problem of the skill. A skill written only in English says `language: en`; a German translation of
  it would be `SKILL.de.md`.
- A translation `SKILL.<lang>.md` carries for now its `description` and the guide (its body); other keys are not read
  yet. Placeholders are filled as in the skill. A translation that cannot be read is a problem of the skill (stricter
  than "starts with its own language" above, for now).
- The texts follow the turn's language where the model meets them: the skill list in `skill_open`, the target list
  in `thread_start` (both built per turn's language now, not once at the start; the prefix differs per language
  anyway), the guide on `skill_open`, at a thread's start and in every later turn of a plan's thread.
- Not yet: schedules (they run with the skill's own texts), the skills view, `languages`.

## Open questions

1. ~~**The skill's own language**~~: decided A, see above.
2. **Schedules by order:** A: by their order in `SKILL.md` (simple, but moving one breaks the translation); B: an
   optional `id` per schedule that the translation names.
3. **Which skills first:** the ones that fit everywhere (termin, termin-absagen, wecker, todo, merken, aufgabe,
   kochen, geschichte, quiz, vokabeln, briefing, abfall, paperless).
4. **`languages` on the built-in skills** that belong to Germany (ferien, warnungen, nachrichten, fussball).
