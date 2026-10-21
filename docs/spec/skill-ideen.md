# Skills to build – list of ideas

Status 2026-10-18. None of this is built or decided; the list collects what is worthwhile and
says what each one needs. The order is a suggestion. How skills are structured:
[Guide, "Eigenen Skill erstellen"](../guide/de.md); what Assist can already do and what Mira builds beyond that:
[Plan: further tools and skills](skills-plan.md).

The pattern: a skill bundles a guide, settings and schedules. It uses the tools that Mira
has anyway (calendar, lists, search, house control, memory, messages). Its strength compared to
Alexa skills is the dialog, the knowledge about house and persons, and reporting on its own.

## Must have (Alexa can do it, Mira should do it better)

### Continue listening to an audiobook

"Lies mein Hörbuch weiter." – "Welches Kapitel war ich?" – "Fang das Buch von vorn an." – "Spiel Kapitel 5."

- **Via Music Assistant.** It knows audiobooks (folders with chapters, m4b/m4a or many MP3s in CD1, CD2 …), keeps the
  queue and remembers the position itself. Whoever wants the feature installs Music Assistant; without it there is
  no skill. The show in Home Assistant cannot jump to a position and cannot keep a queue
  (the player's functions), so plain direct playback is not enough.
- **Mira is the voice in front of it:** She understands the request, chooses book and player (room), starts the audiobook and
  knows which book belongs to whom ("Annas Buch", "das Buch von Ben"). She says honestly when Music Assistant is missing
  or the book is not found.
- **To clarify before building:** Can Music Assistant in the household play audiobooks with position (test in the live HA)?
  Which tools does it offer Mira, or is a separate tool needed as with `radio_play`?
- **Plus the sleep timer** (below), because audiobook and falling asleep belong together.

### Sleep timer

Built in 0.6.45, see [Scheduled actions](geplante-aktionen.md).

"Schlaf-Timer 30 Minuten." – "Schalte in 20 Minuten alles aus." – "Noch zehn Minuten." – "Doch abbrechen."

- After the chosen time, playback stops (audiobook, radio, music), fading out if desired, and the light goes
  off. With an audiobook the position is kept.
- **New in the core:** Today Mira can **say something** at a time (reminder), but not **do something** at a time.
  The timer needs one-time, scheduled actions: for example as a short-lived automation in Home Assistant that
  disappears afterwards, or as a scheduled call in the agent. The same is needed by "Mach in einer Stunde das Licht aus" and
  scenes with a delay.
- Extending and cancelling in the conversation ("Noch zehn Minuten").

## Prerequisites that several skills need

- **Play an address.** A tool that starts a sound or stream on the speaker of a room,
  like `radio_play`, but for arbitrary addresses (sounds, news as audio). Needed by: scenes, falling asleep,
  news.
- **Age of persons (optional).** Whoever is a child gets different stories, quiz questions and tasks.
  The age is voluntary; without it Mira assumes an adult. Needed by: story, quiz, learning.
- **State for skills.** A skill should be able to remember something that is not in the conversation: which audiobook, which
  stories were already heard, quiz points, what has stuck when learning. Today a skill can only use memory (facts as text),
  without targeted changes; see the section "How a skill remembers something" below.
- **Import and export memory.** House knowledge as a file that one maintains by hand. Needed by: handbook.

## 1. Scenes: "Gute Nacht", "Guten Morgen", "Heimkino geht los"

At Alexa they are called scenes or routines: one sentence triggers several actions (light off, shutters down,
alarm system armed, alarm, sounds for falling asleep).

- **Home Assistant knows scenes and scripts** as entities that can be exposed to Assist. Whether
  Mira already activates them that way via "Schalte die Szene Gute Nacht ein" is to be checked.
- **What Mira can contribute:** Creating a scene in the conversation ("Lern die Szene Gute Nacht: …"), checked
  and created after a yes, just as tasks are created as automations. It then lives in Home Assistant and
  is changed there.
- **Important actions** (lock, alarm, garage door) in a scene still ask, as everywhere.

## 2. Morning and evening briefing

Built in 0.6.51 as skill `briefing` (`skills/briefing`): schedules 5:30 and 18:00 for the house, on request also with the calendar of the speaking person. Since 0.6.52 additionally 5:35 per person reachable by a personal message (`per_person`): own appointments (the calendar is named like the person) and headlines on their topics from personal memory.

At the chosen time Mira reports on her own: weather, appointments, garbage collection, waiting messages, a few
news items of the day. This is the flash briefing classic, only personal.

- Schedule in the skill, settings: which building blocks, in which room.
- Uses existing tools: calendar, weather, search, messages. On request also as audio (play an address).

## 3. Cooking with dialog

Search a recipe, read it aloud step by step, set timers, put ingredients on the shopping list. Allergies and
preferences come from memory ("Maria isst keine Nüsse"). Mira answers "Was kommt als Nächstes?" and
"Wie lange noch?" instead of following a fixed sequence.

## 4. Reading aloud: stories for children

Mira reads a story aloud, matching the age, the length and the occasion. There are two sources that
complement each other:

- **Told by herself.** Mira invents a story on request ("eine mit einem Drachen, zehn Minuten"), with the names
  of the children. No rights question, but new every time.
- **From a collection.** Example: [Zwergenstark, Kindergeschichten](https://zwergenstark.de/kindergeschichten/).
  The site organizes its stories the way a skill needs them:
  - **Topics:** good-night stories, animal stories, adventure stories, fantasy journeys (for falling asleep),
    popular stories, by season as well as Christmas and Halloween.
  - **Age:** every story has an age label ("ab 3 Jahren", "Ab 6-8 Jahre", "3-7 Jahre", "8-12 Jahre").
    The spellings are inconsistent; the skill converts them into an age range and compares it with the
    age of the person (see above). Without an age Mira asks once what fits.
  - **Duration:** "ca. 5 Min." up to about 37 minutes. The skill chooses the length that fits the occasion ("kurz vor dem
    Einschlafen" at most 10 minutes). Mira reads long stories in parts ("Ich lese morgen weiter") and
    remembers the position.
  - **Formats:** text for reading aloud, plus audio stories (audio), poems and join-in stories. Audio needs
    "play an address", the rest works via Mira's voice.
- **What Mira adds:** She remembers what a child has already heard and does not repeat it unasked. She suggests
  stories that fit the season (Christmas, Halloween). After the story she asks whether the light should go off
  and a sleep sound (see scenes) should play.
- **How she gets the stories:** As far as I can tell, the site has no feed and no interface, only the
  overview with tiles (title, age, duration) and a search. The skill would work with the internet search and reading the
  page, limited to this address.
- **Clarify first:** The content is protected by copyright, and about reading aloud or use in an application
  I found nothing in the site's information (I did not read the terms and the imprint). Before building, read the
  terms and ask the operator whether reading aloud in one's own household is fine, or whether there is
  a way (feed, permission). Until then, the self-told variant remains.

## 5. Quiz

Questions by topic and difficulty, score across several players, whom Mira recognizes by their names.
Age-appropriate if the age is known.

## 6. Learning and vocabulary (school)

Quizzing and practicing: vocabulary, times tables, questions on a school topic. Mira remembers what someone
already knows and what not yet, and quizzes weaker items more often. The material can come from a list in memory
(vocabulary of the week) or be asked freely. Needs the age and the memory per person.

## 7. House handbook

"Wo ist der Sicherungskasten?" – "Wie entkalke ich die Kaffeemaschine?" – "Wann kommt der Schornsteinfeger?"
Answer from the house knowledge that was imported into memory. Mira says honestly when something is not in there.

## 8. Timetable

"Wann fährt der nächste Bus?" Via sensors that an HA integration provides for the stop. The skill has
a setting for the sensor and answers briefly with the departure.

## Observed, but not in the plan

- **Skills with a brand connection** (ride service, delivery service): need services and accounts that Mira does not have.
- **Music** (artists, albums): a matter for Music Assistant, see [Plan](skills-plan.md).

## How a skill remembers something

A skill has no storage of its own and runs no code. What it has:

1. **Memory** (`memory_save`, `memory_search`, `memory_forget`): facts as text, per person or for the house,
   searchable via keywords. That is enough for "Anna hört gerade ‚Der kleine Prinz‘". Drawback: there is no
   "change", only saving anew and forgetting the old one, and finding works via words, not via fixed fields.
2. **The system that already keeps the state:** For audiobooks that is Music Assistant (position, queue). Mira
   then only has to remember which book is meant, and asks there for the rest. That is the cleanest way.
3. **A simple store per skill (open):** A skill would get a few named values per person ("book",
   "chapter", "points") that Mira can read and set. Several ideas here need that: audiobook (if MA does not provide the position),
   quiz, learning, stories (already heard). Whether it is necessary is decided with the first skill
   that really needs it.
