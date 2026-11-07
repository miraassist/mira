# Random pick – Spec

Status 2026-11-07: steps 1 and 2 built (not released): the tool `random_pick`, the axes in the skill's
frontmatter, and the skills `quiz`, `vokabeln`, `musikquiz` and `geschichte` with their axes, as a trial. Step 3
open. Changed after the trial on the test HA (2026-11-07): the vocabulary draws once per round, the quiz no age,
the music quiz only year and origin (The skills, below). Scenarios: [random-pick.feature](random-pick.feature).

## Goal

A quiz, a vocabulary test, a music quiz and a bedtime story come out the same every time: asked for "ten questions
about animals", the model takes its ten most likely ones, round after round (seen 2026-11-06 in the music quiz: Billie
Jean, Take on Me, Madonna, Queen in nearly every round of the 80s). A model cannot draw by chance, but for every
narrow request it answers something else: "a question about birds, hard, asking how many" is not the question it gives
for "about sea animals, easy, true or false".

So the code draws, the model fills in. The skill names its **axes** (age, difficulty, topic, …) with their values;
what the user said is fixed, the rest is drawn per question; the model writes one question per drawn line.

## Decided (2026-11-07)

| # | Question | Decision |
|---|---|---|
| 1 | Remembering past rounds instead | No: it loads the context, still needs the model to sort out what it saw, and a good draw makes it rarely needed. Within a round the plan already knows what was asked |
| 2 | Who draws | Mira's code, in a tool `random_pick`; the skill's text says when to call it |
| 3 | Where the axes are | In the skill's frontmatter, `metadata.mira.random`: one file, shown on the page with the skill; the model does not see the lists (the frontmatter goes to the code only), so it neither copies nor shortens them |
| 4 | Nesting | One level: an axis may give each of its values a list of sub-values (topic → subtopics, decade → years) |
| 5 | A fixed value that is not in the list ("Harry Potter") | Taken as given, without sub-values; only the other axes are drawn |
| 6 | Which skills | As a trial: `quiz` (with the axis "Frageart", the form of the question, which also varies when the user fixed everything else), `vokabeln` (one word field per round, drawn once), `musikquiz`, `geschichte` |
| 7 | Music quiz from a real list (a Deezer playlist at a random position) | A trial of its own after this one, tried on Deezer first (step 3); a number range as a second form of an axis only if that trial needs it |

## Building blocks

- **The concept: a tool**, read-only, as `calculate` (exact where the model is not). It belongs to the skills
  (`internal/skills`, as `skill_open`): it reads the axes of a loaded skill.
- **A skill's metadata**, as `settings` and `target`: the code reads them, the page shows them with the skill.
- **A skill's translation** (`SKILL.en.md`, [Skill languages](skill-languages.md)). Departure: a translation's
  `random` replaces the whole block instead of matching it key by key, because the axes' names and values are all
  text, and a translation may name them differently.

## The axes: `metadata.mira.random`

```yaml
metadata:
  mira:
    random:
      alter: [Kinder 6–9, Kinder 10–13, Jugendliche, Erwachsene]
      schwierigkeit: [leicht, mittel, schwer]
      thema:
        Tiere: [Meerestiere, Vögel, Insekten, Haustiere, Dinosaurier]
        Geografie: [Hauptstädte, Flüsse, Gebirge, Flaggen]
      frageart: [Wer, Wo, Wann, Wie viele, Was ist größer, Richtig oder falsch]
```

- An axis is a name with either a **list** of values or a **map** from value to its list of sub-values. In the
  skill's own language; the order is kept.
- A translation gives its own `random` at the top of its frontmatter, the whole block (decision, Building blocks).
- **Checked when the skill loads**: at least one axis; every list has at least one value; a value is a short text
  (no list in a list, no deeper nesting); every value of a map has at least one sub-value. A skill whose `random` breaks
  one of these is a problem of the skill (as today for broken metadata: left out, named in the log and in
  `/v1/status`, shown as a repair). A translation whose `random` breaks them is a problem of the skill as well, as
  every translation that cannot be read ([Skill languages](skill-languages.md): stricter than "starts with its own
  language", for now).

## The tool `random_pick(skill, fixed, count)`

There when at least one enabled skill has axes. Read-only.

| Argument | |
|---|---|
| `skill` | the skill's name |
| `fixed` | optional: axis → value, what the user said or Mira knows (the player's age from memory) |
| `count` | how many lines, 1 to 30; default 1 |

What it does, step by step:

1. Finds the skill and its axes in the turn's language (the translation's, else its own).
2. For every axis not in `fixed`: draws `count` values. A value comes again only when all values of the axis were
   drawn (each axis is shuffled and dealt round by round): ten questions over twelve topics get ten different topics.
   A map axis draws its value, then a sub-value of it the same way.
3. For a fixed map axis: a value of the map is kept and only its sub-value is drawn; a sub-value (`Vögel`) is kept
   with its value (`Tiere / Vögel`); a value found in neither (`Harry Potter`) is kept as given, with no sub-value (decision 5).
4. Returns one line per draw, axes in their order, each as `name: value` (a map axis `name: value / sub-value`):
   `1. alter: Kinder 6–9 · schwierigkeit: schwer · thema: Tiere / Vögel · frageart: Wie viele`.

**Cost:** nothing outside: no call to Home Assistant, no download, no model. In memory, it grows with `count` × the
number of axes (at most 30 lines). The answer adds the lines to the turn: about 80 characters a line. What the turn
waits for: one model round more where a skill calls it (at the start of a round of the quiz, the vocabulary or the
music quiz, before a story), since what follows depends on the draw. The tool's description (about 600 characters)
is in the unchanging prefix of every request as long as an enabled skill has axes, as every tool's.

**What the model does with it** is the skill's text: call it once when it writes the plan (or before the story),
with what is fixed, and follow one line per question.

### When a step fails

The tool's error goes to the model as the tool's result (the engine logs it as `tool done` with `error=true`, as
every tool); the model then says the user's request again in the call, or does without the draw.

| Fails | What the model gets |
|---|---|
| No skill of that name, or it is not enabled | "There is no skill X." |
| The skill has no axes | "The skill X has no axes." |
| `fixed` names an axis the skill does not have | "The skill X has no axis Y; its axes: a, b, c." |
| `count` outside 1 to 30 | "count must be between 1 and 30." |

No other error: drawing cannot fail.

## The skills (trial)

The axes are proposals; whether they spread enough shows in play. The German texts:

- **`quiz`**: `schwierigkeit`, `thema` (about 12 topics with their subtopics), `frageart`. Before the plan:
  `random_pick` with the topic and the difficulty as far as said, `count` 10; each question follows its line (topic,
  difficulty, the form of the question). The age is no axis (decided 2026-11-07, after the trial: drawn per question,
  a round for one player mixed questions for small children and for adults): it is the players', from what Mira
  knows, else adults. Another round draws anew.
- **`vokabeln`**: `niveau`, `wortfeld`. Only when Mira makes the list up herself (not for the user's list, not from
  memory, not for the times tables): `random_pick` once for the round (no `count`), with the level and a field the
  user named; all words of the round come from the line's field, at its level; the model mixes the word classes.
  Changed 2026-11-07 after the trial: the word class was an axis drawn per word, and the guide said "the field of the
  first line holds for the whole round"; that contradiction made Gemini think about 980 tokens and answer nothing,
  twice.
- **`musikquiz`**: `zeit` (decade → years), `herkunft`. Before choosing the songs: `random_pick` with the decade
  as far as said, `count` the number of songs; each song is a well-known one of its line's year and origin, in the
  genre the user named. Where no song fits a line, the year stays and the origin gives way. Genre and fame were axes
  too until the trial (2026-11-07): lines such as "German hip-hop 1981" or "a nearly forgotten hit" made the model
  replace songs one by one, and the turn used up its 8 tool rounds after 31 s; the year alone spreads the songs.
- **`geschichte`**: `held`, `ort`, `problem`, `gegenstand`. Before the story: `random_pick` with what was wished for
  ("mit einem Drachen": `held: Drache`), `count` 1.

The English translations of `quiz`, `vokabeln` and `musikquiz` get the same axes in English.

## Steps

1. **The tool and the axes** (built 2026-11-07): `metadata.mira.random` read and checked, a translation's block, the
   tool `random_pick` with its errors. RP-01 to RP-15.
2. **The skills as a trial** (built 2026-11-07): the axes and texts of `quiz`, `vokabeln`, `musikquiz`,
   `geschichte`, played on the test HA. RP-20 to RP-23.
3. **Music quiz from a real list** (open, decision 7): try Deezer's playlists by decade and a random position on
   Deezer first; then decide.
