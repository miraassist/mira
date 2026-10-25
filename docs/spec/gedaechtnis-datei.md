# Memory as a file: export and import – spec

Status 2026-10-25: built (`internal/memfile`, page Gedächtnis, `mira memory`).

## Why

Mira learns the house in conversation, one fact at a time. A new household starts empty, and what someone knows about
the house (where the fuse box is, how the coffee machine is descaled, when the chimney sweep comes) is quicker written
down in one go than told. The other way round, whoever wants to look through or tidy up what Mira knows wants it in one
place, outside the page. The house handbook (skill idea, [skill-ideen.md](skill-ideen.md)) needs exactly this: house
knowledge brought in as a file.

## Principle

- **One file, written for people.** Markdown, one fact per line, a heading per owner. Nothing in it that a person
  would not write by hand: no ids, no times, no authors.
- **Import replaces, like loading a file into an editor.** Each owner whose section is in the file then has exactly the
  facts of that section; an owner without a section in the file stays as it is. Whoever wants to merge does it in a
  text editor before importing (decided 2026-10-25).
- **Who may export and import what** follows the page as it is ([benutzer.md](benutzer.md), Memory): inside Home
  Assistant everyone, admins too, the house's memory and their own; the page on its own (agent token) and the command
  line every memory.
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

1. Read the file, check every heading (known owner, allowed for whoever imports). One wrong heading stops the import
   before anything is written.
2. Per owner with a section: compare the section with the owner's facts now (text compared without case, spaces and
   final full stop).
   - A line that is already there stays as it is, with its author and time; changed tags are taken.
   - A line that is not there becomes a new fact, author: whoever imports (their person, none from the command line), source `import`.
   - A fact that is not in the section is deleted.
   An empty section (`# Anna` without lines) empties that owner's memory.
3. Show before writing, per owner: "Haus: 3 neu, 1 entfällt, 26 bleiben", the new and the deleted ones listed.
   Writing happens on a yes, all owners in one transaction.
4. After the import the profiles are made afresh at the next nightly compaction; "Jetzt verdichten" on the page
   does it at once.

## Where

- **Page, Gedächtnis:** buttons **Exportieren** (downloads `mira-gedaechtnis.md`) and **Importieren** (choose a file,
  see what would change, confirm).
- **Command line:** `mira memory export [-o datei]` and `mira memory import datei [--yes]`, for a fresh setup by
  script. Without `--yes` it shows what it would change and asks.

## Scenarios

- A new household imports a file with 30 house facts; Mira answers "Wo ist der Sicherungskasten?" right away.
- Exported, edited by hand (two lines added, one changed, one deleted), imported again: three new facts, two
  deleted (the old wording and the deleted line), all others stay with their author and time.
- A file with only `# Anna`: the house's memory and Ben's stay untouched.
- Anna imports in Home Assistant a file with `# Haus` and `# Anna`: both are taken. With `# Ben` in it: nothing is
  written, "Den Abschnitt „Ben“ darfst du nicht importieren."
- A file with a heading `# Oma` that is no person in Home Assistant: nothing is written, the message names "Oma".

## Not part of it

**Exporting the whole state** (decided 2026-10-25): editing by hand and moving Mira are two different things and stay
apart. Only memory is worth a file to edit: the configuration is a file already (`mira.yaml`), secrets must not leave
unencrypted, and the rest of the database is working state that expires (threads, histories, judgements, pending
messages). Tasks and person settings live in Home Assistant. A backup function is left out for now: the database is
one file (`mira.db`), and with `secrets.json` and `mira.yaml` that is all of Mira.
