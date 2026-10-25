# Memory as a file: export and import – spec

Status 2026-10-25: proposed, not built. Open questions at the end.

## Why

Mira learns the house in conversation, one fact at a time. A new household starts empty, and what someone knows about
the house (where the fuse box is, how the coffee machine is descaled, when the chimney sweep comes) is quicker written
down in one go than told. The other way round, whoever wants to look through or tidy up what Mira knows wants it in one
place, outside the page. The house handbook (skill idea, [skill-ideen.md](skill-ideen.md)) needs exactly this: house
knowledge brought in as a file.

## Principle

- **One file, written for people.** Markdown, one fact per line, a heading per owner. Nothing in it that a person
  would not write by hand: no ids, no times, no authors.
- **Import merges, it never deletes.** New facts are added, facts that are already there are skipped. What is in
  Mira's memory but not in the file stays. Deleting stays where it is today: on the page, or "vergiss …".
- **Who may export and import what** follows the page as it is ([benutzer.md](benutzer.md), Memory): the house's
  memory and one's own; an admin also the persons' memories that the page shows them.
- **The profiles** (the nightly summary per owner) are not part of the file: they are made from the facts.

## The file

```markdown
# Haus

- Der Sicherungskasten ist im Keller links neben der Tür. #technik #keller
- Die Kaffeemaschine wird mit dem Entkalker aus dem Schrank über der Spüle entkalkt, etwa alle zwei Monate. #küche
- Der Schornsteinfeger kommt jedes Jahr im November. #termine

# Anna

- Anna ist allergisch gegen Birke. #allergie
```

- `# Haus` (English `# House`) is the house; any other heading is a person, by the name Home Assistant shows or the
  person's id (`anna_schmidt`). A heading Mira does not know stops the import before anything is written, with the
  name in the message.
- Each `- ` line is a fact, as a whole sentence in the third person, as Mira saves them herself.
- Words with `#` at the end of the line are the fact's tags; the text is what stays before them.
- Empty lines, text that is not a list item, and Markdown emphasis are ignored; so the file can carry notes of its
  own.
- Export writes the same form: house first, then the persons in the page's order; within an owner the facts in the
  order they were saved.

## Import

1. Read the file, check every heading (known owner, allowed for whoever imports).
2. Per fact: the same text (compared without case, spaces and final full stop) is already there for this owner →
   skipped. Otherwise → new fact, author: whoever imports ("Import von Stefan"), source `import`.
3. Show before writing: "12 neu, 3 schon da", the new ones listed. Writing happens on a yes.
4. After the import the profiles are made afresh at the next nightly compaction; "Jetzt verdichten" on the page
   does it at once.

Similar but not equal facts ("Der Sicherungskasten ist im Keller." next to the longer one) are both kept. Merging
them is the compaction's job, as for facts saved in conversation.

## Where

- **Page, Gedächtnis:** buttons **Exportieren** (downloads `mira-gedaechtnis.md`) and **Importieren** (choose a file,
  see what would be added, confirm).
- **Command line:** `mira memory export [-o datei]` and `mira memory import datei [--yes]`, for a fresh setup by
  script. Without `--yes` it shows what it would add and asks.

## Scenarios

- A new household imports a file with 30 house facts; Mira answers "Wo ist der Sicherungskasten?" right away.
- Exported, edited by hand (two lines added, one changed), imported again: the two and the changed line come in as
  new facts; the old wording of the changed line stays until it is deleted on the page or merged by the compaction.
- Anna (no admin) imports a file with `# Haus` and `# Anna`: both are taken. With `# Ben` in it: nothing is written,
  "Den Abschnitt „Ben“ darfst du nicht importieren."
- A file with a heading `# Oma` that is no person in Home Assistant: nothing is written, the message names "Oma".

## Open

1. **A changed line comes in as a new fact** and the old one stays (merge, never delete). Alternative: an option
   "Abschnitt ersetzen" that replaces an owner's facts with the file's. Riskier, but what one expects after editing.
2. **The whole state instead of only memory** (see below).

## The whole state: export and import of everything?

What Mira keeps, and where:

| What | Where | For a person to edit? |
|---|---|---|
| Configuration (models, agents, skills) | `mira.yaml` | yes, already a file |
| Secrets (tokens, passwords) | `secrets.json` in the data folder | no |
| Memory (facts, profiles) | database | yes: this spec |
| Pending messages and reminders | database | hardly; they expire |
| Threads, sessions, histories, judgements, agent runs | database | no; working state and logs |
| Tasks | Home Assistant (automations with the label "Mira") | in HA's editor |
| Person settings (channels, quiet hours) | Home Assistant (integration options) | in HA |

Two different needs hide behind "export everything":
- **Editing and seeding by hand:** only memory is worth it; everything else is either a file already (`mira.yaml`),
  must not leave unencrypted (secrets), or is working state that expires.
- **Moving or saving Mira as a whole:** that is the data folder (`mira.db`, `secrets.json`) plus `mira.yaml`, taken
  as they are; tasks and person settings move with Home Assistant's own backup. A file per kind of state would add a
  second format for each of them without anyone editing it.

Options:
- **A:** only this spec (memory as a file). Moving Mira stays copying the data folder and `mira.yaml`, documented in
  the guide.
- **B:** A, plus a button "Sicherung herunterladen" on the page: one archive with `mira.yaml`, `secrets.json` (as it
  is, encrypted if `MIRA_SECRET_KEY` is set) and a consistent copy of `mira.db`; restoring by putting the files back.
- **C:** a general export of every table in one readable file, memory being one part. Most of it nobody reads, and an
  import would have to merge histories and threads.
