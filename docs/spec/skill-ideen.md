# Skills to build – list of ideas

Status 2026-10-25. The list holds what is still open and what each idea needs. How skills are structured:
[Guide, "Eigenen Skill erstellen"](../guide/de.md); what Assist can already do and what Mira builds beyond that:
[Plan: further tools and skills](skills-plan.md).

The pattern: a skill bundles a guide, settings and schedules. It uses the tools that Mira has anyway (calendar,
lists, search, house control, memory, messages). Its strength compared to Alexa skills is the dialog, the knowledge
about house and persons, and reporting on its own.

## Built

| Idea | Where |
|---|---|
| Sleep timer | `sleep_timer`, [Scheduled actions](geplante-aktionen.md) |
| Scenes and routines | Home Assistant's scenes and scripts; guide chapter "Szenen und Skripte aus Home Assistant" |
| Morning and evening briefing | skill `briefing` |
| Cooking with dialog | skill `kochen`, cards on a TECHO5 Show |
| Stories told by Mira | skill `geschichte` |
| Quiz | skill `quiz` |
| Vocabulary and times tables | skill `vokabeln` |
| Mira's own sounds and cards | `sound_play`, `display_card`, [Sounds and cards](satellit.md) |
| Age of persons | memory; without it Mira assumes an adult |

## Open

### Continue listening to an audiobook (must have)

"Lies mein Hörbuch weiter." – "Welches Kapitel war ich?" – "Fang das Buch von vorn an." – "Spiel Kapitel 5."

- **Via Music Assistant.** It knows audiobooks (folders with chapters, m4b/m4a or many MP3s in CD1, CD2 …), keeps the
  queue and remembers the position itself. Whoever wants the feature installs Music Assistant; without it there is
  no skill. The show in Home Assistant cannot jump to a position and cannot keep a queue, so plain direct playback
  is not enough.
- **Mira is the voice in front of it:** she understands the request, chooses book and player (room), starts the
  audiobook and knows which book belongs to whom ("Annas Buch"). She says honestly when Music Assistant is missing or
  the book is not found.
- **To clarify before building:** Can Music Assistant in the household play audiobooks with position (test in the
  live HA)? Which tools does it offer Mira, or is a separate tool needed as with `radio_play`? Does the skill need
  state of its own per person, or is the position in Music Assistant enough (see below)?
- Goes with the sleep timer, which is built.

### House handbook

"Wo ist der Sicherungskasten?" – "Wie entkalke ich die Kaffeemaschine?" – "Wann kommt der Schornsteinfeger?"
Answer from the house knowledge in memory. Mira says honestly when something is not in there. Needs memory import
(house knowledge as a file maintained by hand).

### Timetable

"Wann fährt der nächste Bus?" Via sensors that an HA integration provides for the stop. The skill has a setting for
the sensor and answers briefly with the departure.

### Smaller open points of built skills

- **Learning a scene in the conversation** ("Lern die Szene Gute Nacht: …"): checked and created in Home Assistant
  after a yes, as tasks are created as automations.
- **Stories from a collection** (for example [Zwergenstark](https://zwergenstark.de/kindergeschichten/), with topic,
  age and duration per story): not built, rights unclear. Before building, read the terms and ask the operator
  whether reading aloud in one's own household is fine.
- **Play an arbitrary address** (news as audio, audio stories): `sound_play` plays only Mira's own sounds,
  `radio_play` only radio.

## Observed, but not in the plan

- **Skills with a brand connection** (ride service, delivery service): need services and accounts Mira does not have.
- **Music** (artists, albums): a matter for Music Assistant, see [Plan](skills-plan.md).

## How a skill remembers something

A skill has no storage of its own and runs no code. What it has:

1. **Memory** (`memory_save`, `memory_search`, `memory_forget`): facts as text, per person or for the house. Enough
   for "Anna hört gerade ‚Der kleine Prinz‘". No "change", only saving anew and forgetting the old one.
2. **A plan in a thread** ([Threads](vorgaenge.md)): quiz and vocabulary keep their points and progress there.
3. **The system that already keeps the state:** for audiobooks Music Assistant (position, queue). Mira only has to
   remember which book is meant.

A store of named values per skill and person stays open; it is decided with the audiobook, the first skill that may
need it.
