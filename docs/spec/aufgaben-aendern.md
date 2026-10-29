# Changing tasks in the conversation – Spec

Status 2026-10-29: specified, not built. Scenarios: [aufgaben-aendern.feature](aufgaben-aendern.feature). Builds on
[Tasks](aufgaben.md) and [Threads](vorgaenge.md); protection by [Hooks](hooks.md).

## Goal

A household changes its tasks by voice, not only the ones Mira made: the times, the sun, the conditions, also what a
task switches.

1. Anna: "Das Abendlicht im Wohnzimmer soll erst 30 Minuten nach Sonnenuntergang angehen."
2. Mira finds the task, has the change drafted, and Home Assistant checks the draft.
3. Mira: "Die Aufgabe Abendlicht Wohnzimmer: bisher bei Sonnenuntergang, neu 30 Minuten danach. Soll ich das so
   ändern?"
4. Anna: "Ja." Mira: "Geändert. Ab heute Abend gilt die neue Zeit."

## Terms

- **Task** (Aufgabe): a Home Assistant automation, as the user sees it. Mira's own tasks carry the label "Mira"; the
  household's own come from HA's automation editor. To the user Mira says "Aufgabe" / "task" for both, never
  "Automation"; the word stays in technical places (the integration's commands, logs).

## Decided (2026-10-29)

| # | Question | Decision |
|---|---|---|
| 1 | Which tasks Mira may change | Every task HA's automation editor saved (`automations.yaml`, with an `id`), whatever its labels; protection by the hooks |
| 2 | What Mira may change | The whole task: triggers, conditions, also actions. New actions follow the same security rules as when creating |
| 3 | Who may change | Every person Mira knows. What reaches critical targets needs a yes anyway (`critical.star`) |
| 4 | Undo | None, as in HA's editor |
| 5 | Naming | "Aufgabe" / "task" for the user, as when creating |
| 6 | The task list (`task_list`, the page's tab "Tasks") | Only Mira's tasks, as today; the household's are found when changing one |

## The skill `aufgabe-aendern`

A skill like `aufgabe` (`skills/aufgabe-aendern`, German): its description makes the model open it on "Das Abendlicht
soll erst um 19 Uhr angehen", "Bei der Aufgabe mit dem Wasser nimm den Sensor raus", "Ändere die Aufgabe …". No fixed
phrases in the code.

- **Fields:**
  - `aufgabe`: which task, by name, area or what it does; checked with `tasks.find`.
  - `aenderung`: what should change, in the user's words and complete.
- **Finish:** `tasks.edit`, in the background like `tasks.automation`: the turn waits a moment, else Mira says she
  comes back, and the proposal comes as a message of the thread.
- **Its guide** tells the model: look the task up rather than ask; ask back only what is unclear (which of two tasks,
  "später" by how much); never word the proposal itself, it comes from the thread.

## Finding: `tasks.find`

A check of the field `aufgabe`:

1. It looks among all tasks (`mira/automation/list` with `all`) by name, area and entity id. The value comes back
   completed with the entity id.
2. Several match: it names them, and the thread asks which one.
3. The one found was not saved by the editor (a YAML package, `configuration.yaml`): Mira says she cannot change it,
   and why. The thread ends.

## Drafting: `tasks.edit`

`Work`, once both fields are there:

1. Reads the task's configuration (`mira/automation/get`).
2. The drafting model gets it with the change, and the same rules and house context as when creating (entities,
   Mira's actions, the security rules). It returns the whole changed configuration and a sentence what changes.
3. Home Assistant checks it (`mira/automation/validate`); an error gives a second draft with the error text, as when
   creating.
4. **Security rules for what is new**: an action the draft adds or changes passes the rules of
   [aufgaben.md](aufgaben.md) (no critical targets, no actions without entity targets, nothing that runs code). An
   action that was there before and stays unchanged is the admin's decision and is not checked again; otherwise a
   household task that unlocks the door could never get a new time.
5. A draft that changes nothing ends the thread with that said.

**The proposal** is the drafting model's sentence (before → after), and what the code says itself, as when creating:
the conditions before and after (`describeConditions`), the actions it newly switches ("Neu schaltet sie außerdem: …")
and the ones it no longer switches ("Sie schaltet nicht mehr: …"). The yes covers exactly that.

## Saving

`Commit`, after the yes:

1. The change passes the hooks as a call of its own, `task_edit`, whose targets are the entities the task's actions
   reach, before and after:
   - the system hook `self.star` refuses an unknown voice, as for rules;
   - `critical.star` asks for a yes on a lock (the thread's yes is that yes), and after web content or other
     third-party text in the turn the change is refused (`web.star`), as for every critical call.
2. The integration saves the task under its id (`mira/automation/save`). Its labels stay as they are: a household
   task does not become one of Mira's.
3. The log says `task edited` with the id, the person and the change. Mira says: changed.

## Integration

All new fields optional both ways ([versionen.md](versionen.md)): an older integration answers an unknown command
with an error, and Mira says she cannot change tasks yet and that the integration needs an update.

- `mira/automation/get` (new): the configuration of one task by entity id, or the error "not editable" when it is not
  in `automations.yaml`.
- `mira/automation/list` with `all: true` (new field): every task, with name, area, labels and `editable`; without it,
  as today, only Mira's.
- `mira/automation/save` with `keep_labels: true` (new field): no label "Mira" is added.

## Where one sees it

- **HA**: the task in the automation editor, changed; its traces show the next run.
- **The guide**: a short section in the chapter on tasks, with the example above.

## Steps

1. **Integration**: `get`, `list` with `all`, `save` with `keep_labels`; tests in pytest.
2. **The building blocks** `tasks.find` and `tasks.edit`, the hook call `task_edit`.
3. **The skill `aufgabe-aendern`**.
4. **The guide**.
