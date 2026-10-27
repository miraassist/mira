# Tasks: spec

Status 2026-10-15: specified, not built yet. The user concept
is in [benutzer.md](benutzer.md).

## Why

Until now there were two concepts for the same concern "sag Bescheid, wenn …":

- **Watcher:** configured entities. Every state change goes to the model.
- **Assignments:** created in the conversation. The model plans rules once. Mira checks them itself (state, threshold, time window, normal value) and passes only candidates to the model.

For users it was not clear which applies when. Both concepts also rebuild
small parts of Home Assistant's trigger and condition system, only
to run them in Mira.

## Principle

A **task** is an ordinary Home Assistant automation with the
label **"Mira"**.

- **HA** decides *when*: triggers, conditions and templates, to their full extent. HA is the source of truth. Editing happens in HA's automation editor, with traces.
- **Mira** provides what HA cannot, as **actions** for automations:
  - judging with the model;
  - condensing data from HA so the judgment stays cheap;
  - delivering with Mira semantics: announcement, display, "Übrigens", the person's channels, quiet hours, escalation.
- Mira only keeps a **reference** (automation ID) and its log.

Watchers and the rule engine of assignments are dropped. The test data is deleted without migration.

Division: **HA is the senses, Mira is mind and voice.**

## Actions

All actions are provided by the Mira integration. They call the agent via its API.

### `mira.assess`: judge and report if needed

| Parameter | Required | Meaning |
|---|---|---|
| `goal` | yes | The target in the user's words, e.g. "Achte auf den Wasserverbrauch." |
| `observation` | no | What happened, as text with templates. |
| `data` | no | List `{entity_id, view}`: what Mira looks up and condenses itself (see "Views"). |
| `memory` | no | `relevant` (default), `profile` or `none` (see "Context"). |
| `cooldown` | no | For example `6h`. Within this time the same automation only reports if the importance rises. |
| `notify` | no | Default `true`. With `false` there is only the judgment, no message (dry run). |
| `audience`, `confidentiality`, `dedupe_key`, `expires_in` | no | As with `send_message`. |

**Flow**
1. Mira builds an **isolated** call, that is, without the house prompt. It contains:
   - target, observation and the condensed data;
   - the context (memory, presence, day and time);
   - the last judgments of this automation.
2. The model answers with `{importance 0–3, spoken, details}`.
3. With importance > 0 and `notify` (and outside the cooldown), a message is created. The source is `task`, the reference points to the automation.

**Response** (for `response_variable`): `{importance, spoken, details, message_id | null}`.

### `mira.decide`: judgment as a condition

| Parameter | Required | Meaning |
|---|---|---|
| `question` | yes | A yes/no question, e.g. "Läuft hier gerade unnötig Wasser?" |
| `observation`, `data`, `memory` | no | as with `assess` |

**Response:** `{answer: true|false, reason}`. No message is created.

A native condition (`condition: mira`) is not possible: HA checks conditions synchronously, the model needs seconds. Instead the automation uses the action with a response and then a condition:

```yaml
- action: mira.decide
  data:
    question: Läuft hier gerade unnötig Wasser?
    data: [{ entity_id: sensor.wasserzahler, view: same_hour }]
  response_variable: d
- condition: template
  value_template: "{{ d.answer }}"
```

### `mira.send_message`: report without judgment (already exists)

`text`, `importance`, `details`, `audience`, `confidentiality`, `dedupe_key`, `expires_in` → message. For cases in which the judgment is fixed, such as a water leak sensor.

## Views (`data[].view`)

Mira prepares data so that the model sees a few fitting numbers instead of raw data. That is its contribution against token consumption. Name, area and unit go along with every entity.

| `view` | Content |
|---|---|
| `state` | current state, since when |
| `same_hour` | last full hour (consumption for meters, mean for measurements) next to the same hour of the last 14 days: median, range, maximum. Source: `recorder/statistics_during_period` |
| `day` | today until now next to the same weekdays of previous weeks |
| `upcoming` | calendar only: the appointments from now until the end of tomorrow, in the timeline representation ([Context](kontext.md), section 2) |

Planned, with the same representation: `history` (history), `list` (list attributes); plus `collected` (collected data, see "Later").

## Context of every judgment

- **Memory** (`memory`):
  - `relevant`: the household profile plus at most 5 facts that match target, question or observation via full-text search.
  - `profile`: only the profile.
  - `none`: nothing.
  - Read only, judgments write nothing into memory.
  - Which memory goes along is determined by the owner of the task (`owner`, see [benutzer.md](benutzer.md)): always the house memory, for personal tasks also that of the person.
- **Presence:** who is at home, from the person entities.
- **Day:** weekday, time, whether it is quiet hours.
- **Memory of the task:** the last judgments of the same automation (when, importance, said). This way the model does not annoy with repetitions and can point out trends.

Size of a call: about 500 to 1,500 tokens. Hourly evaluation of a task costs roughly 0.5 cents per day with Gemini Flash.

## Creating in the conversation

Since agent 0.4.0, creating is a [thread](vorgaenge.md) with the target
`aufgabe` (skill `skills/aufgabe`): fields watch, when, measure, reaction;
if a measure is missing, Mira asks; the proposal belongs to the thread, the
yes is `thread_confirm`. The flow below is the original draft.

1. "Achte auf den Wasserverbrauch." → `task_create(goal)`. Mira answers immediately: "Ich plane das und melde mich mit einem Vorschlag."
2. In the background the model drafts the automation (`alias`, `description` = target, `triggers`, `conditions`, `actions`). It gets the catalog of entities and this spec of the actions.
3. The integration has HA check the draft (`mira/automation/validate`). On errors there is a second draft with the error text.
4. Mira delivers the proposal, in the same room or chat: briefly, what is observed and when she will report, and "Soll ich so vorgehen?".
5. On yes, the integration saves the automation (`mira/automation/save`), with the label "Mira". Mira remembers the ID.

**Where and by whom** (since 0.6.17): The draft learns at which satellite the request came in (area, satellite) and who spoke, if Mira knows it (Telegram, HA app, alone at home). This resolves "hier" and "ich", and the memory of this person goes along. The person becomes the **owner** of the task: the code enters them as `owner` in every `mira.assess` and `mira.decide`, and every judgment then also sees their memory.

Further tools:
- `task_list`: Mira automations from HA, plus open proposals.
- `task_delete`.
- On/off via `automation.turn_on/off`.
- Dry run: run the automation's actions with `notify: false` and read the judgment aloud.

## Integration

- **Websocket commands**, only for Mira's HA user (the integration learns the ID via `/v1/health`) or admins:
  - `mira/automation/validate`;
  - `mira/automation/save` (via the editor's path: replace the entry in `automations.yaml`, reload one automation, set the label);
  - `mira/automation/delete`;
  - `mira/automation/list`.
- **Security rule** (relaxed 2026-10-15):
  - A task may also switch and notify, for example radio, light or a push message with `notify.*`. That is the user's decision. They must know that a model decides and does not judge deterministically.
  - The proposal names these actions explicitly, generated by the code and not left to the model ("Dabei schaltet sie außerdem: …"), including scene steps, device actions and every target the check reads. The yes applies exactly to that.
  - Still blocked is what Mira only switches after an explicit yes in the conversation too (`security.confirm_domains` and `confirm_entities`, e.g. locks, alarm, garage door). For such domains this also applies to targets via areas or devices.
  - What an action reaches is learnt from HA, not parsed by Mira (hardened 2026-10-26):
    - The target is gathered where HA takes it: `target`, `data`, `data_template` and an `entity_id` on the step. Any of them as a template is refused.
    - HA resolves areas, devices, floors, labels and groups to entities. In an area or on a device only the entities of the action's own domain count, as HA switches them.
    - Scenes and groups are followed into their members (the state attribute `entity_id`), so a scene that sets a lock is blocked like the lock.
    - A device action must name its entity; an integration's own device action without one is refused.
  - An action whose HA description names no entities as its target (`mqtt.publish`, `rest_command.*`, `scene.apply`, `zwave_js.set_value`, `conversation.process`) could switch anything and is refused. Exceptions: `mira.*`, `notify.*`, `persistent_notification.*`.
  - Such actions go into a task only inside a script exposed to Assist. A script is allowed if it is exposed to Assist and not protected (label "Mira: nachfragen"); what it does inside is the admin's decision.
  - Also blocked is everything that runs arbitrary code or changes HA itself: shell commands, Python scripts, restarts, updates.
  - Whoever wants that anyway enters it in HA's editor themselves. The rule does not apply there.
  - Reason: anyone can speak at the satellite, and a task acts permanently and unsupervised. Besides, automations would otherwise run with admin rights, which Mira deliberately does not have.
- Prerequisite: `configuration.yaml` includes `automations.yaml` (default).
- Risk: there is no stable interface for integrations to save automations. The editor path may need rework with HA updates.

## Mira UI

"Tasks" tab:
- Mira automations with on/off and "last triggered", link into the HA editor, dry run, delete.
- Open proposals.
- Per task the **log** of judgments: time, data in short form, importance, said, message. This complements HA's traces, which only show *that* `assess` ran.

## Dropped

- Watchers (package, config, UI tab, sub-entries of the integration for watchers and feeds).
- The rule engine of assignments (usage/level/state/threshold, five-minute loop).

The statistics evaluation lives on as the view `same_hour`.

## First build

- `assess` with `data` (`state`, `same_hour`, `day`), `memory`, `cooldown` and `notify`;
- `decide`;
- `send_message`;
- context and memory of the task;
- log;
- creating in the conversation;
- task list with dry run;
- remove watchers and assignments.

Testing happens on the test HA.

## Later

- **Collect:** `mira.collect(bucket, observation)` stores without a model. `assess` with `view: collected` evaluates what was collected in a bundle, e.g. daily, and empties the bucket. For feeds (`feedreader`), calendars and noisy sensors. Comes with the skills.
- **Skills:** use HA as a data source where HA keeps the data natively (calendar, feeds), and condense cleverly. Where HA does not model the data, or models it poorly, they work on their own as before. They bring their proactive part as an HA blueprint.
- **Acting with confirmation:** A judgment proposes an action ("Soll ich das Ventil schließen?"). On yes, Mira carries it out via her normal path, with protection and confirmation.

## Open

- **User concept:** settled in [benutzer.md](benutzer.md). Tasks have an owner (house or person). `assess` and `decide` get `owner`. Personal memory only goes along with personal tasks.
- **Discussion list:**
  - confirming via a fixed yes in the code (language-dependent phrases);
  - editing tasks in the conversation ("nimm den Sensor raus"). With HA as the editor this means: Mira changes the automation via `save`.
