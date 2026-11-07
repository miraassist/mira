# HACS default catalog – spec

Status 2026-11-07: decisions 1 to 3 taken, checks C1 to C4 done; steps 1 to 3 built and run on the test HA, not released.

## Goal

The integration goes into HACS's default catalog. The review
([hacs/default#13170](https://github.com/hacs/default/pull/13170), 2026-11-04) asks for three changes before it can:

1. The integration reads API keys out of other integrations' config entries.
2. The automation commands let the service's non-admin user read, write and delete automations in `automations.yaml`.
3. The integration creates calendars and exposes them to Assist in the background, without an admin asking for it.

Each one changes behaviour the specs describe; this spec says how, and which spec changes with it once built.

## Decisions

1. **No model "from Home Assistant"** (2026-11-06): the integration no longer reads keys or addresses from other
   integrations' entries. The model's source is a provider (its key entered for Mira) or an own server. Changes
   [Setup](setup.md), section 3, and [Models](models.md), "What the integration sets".
2. **Mira works with an admin's rights, openly; no user of her own** (2026-11-07): the review objects to how the
   integration does admin things, not that it does them: a non-admin user let in through the integration's own door
   (`_agent_only`), and the integration writing `automations.yaml` itself. So:
   - The service's Home Assistant access is a long-lived token of Home Assistant's **owner** (the admin who set Home
     Assistant up; there is exactly one, and Home Assistant does not let it be deleted), made with an admin's explicit
     consent in Mira's setup, which names the owner and says why Mira needs admin rights. The owner, not the admin
     who submits: a flow does not know who submits (check C3), and the owner stays (2026-11-07). Like any client
     with an admin's token
     (Home Assistant's MCP server used from a desktop app, Node-RED), Mira acts with the rights the admin gave her.
   - Mira's own Home Assistant user "Mira" goes. What it stood for does not need it: what a satellite says for Mira
     is Mira's; who answers, Mira knows from the voice. A turn Mira started at a satellite (`start_conversation`)
     carries no Home Assistant user (checked on the test HA 2026-11-07: the logbook of 2026-11-04 22:50, the
     satellite's states by "Mira", the turn `conversation.mira` without a user), so the admin's token does not make
     the admin the speaker. A task's run shows no user in any case (an automation runs apart from who made it).
     The cost: in the logbook, what Mira does directly (switching by voice, announcements, music) shows "by" the
     admin instead of "by Mira".
   - Tasks and one-time timers stay Home Assistant automations, saved and deleted through Home Assistant's own
     automation config API, the one the automation editor uses; the integration writes no automation YAML.
   - The commands that read automation data need `require_admin`; `_agent_only` goes.
   - The old user "Mira" of existing installs stays where it is, without a function; it does no harm. The guide says
     it may be deleted.
   Reverses the decision of 2026-10-14 ("Mira stays a non-admin user"). Changes [Setup](setup.md), section 1
   (Mira's HA user), [Tasks](tasks.md) (websocket commands), [Scheduled actions](scheduled-actions.md) (saving and
   cleanup), [Watching states](ha-integration.md) (the exposure list "only an admin can read") and
   [System](../system.md) (agent → integration).
3. **A missing mail calendar is a repair the admin confirms** (2026-11-06): the integration no longer creates it on
   its own. Changes [Mail calendar](mail-calendar.md), section 1, and [Parcels](parcels.md), Setup.

## 1. The model's source

What goes:

- `ha_sources` (`models.py`) and the choice "From Home Assistant: …" in the form "Where does the AI model come from?"
  of the setup and of Configure → AI model.
- The words `from_ha` of the form.

What stays as it is:

- **Provider:** the provider from the service's list (`GET /v1/setup/providers`), then its key, entered for Mira.
- **Own server:** address and an optional key.
- The model list, the trial run and its errors, as [Setup](setup.md) section 3 says.

**Installs set up from Home Assistant before:** at setup the integration sent the key to the service once, which keeps
it as `main`. Nothing reads the other integration's entry afterwards, so such an install goes on unchanged. Changing
the model later goes through a provider or an own server like any other; the key is entered once more. No migration,
no repair: nothing fails.

**Failures:** none new. A wrong key is the trial run's error in the form, as today.

**Cost:** less than today: the form no longer walks the config entries of five integrations.

## 2. A missing mail calendar

Today `ensure_calendars` (`mail.py`), called from the status poll, creates a `local_calendar` entry for each mail
entry without its calendar and exposes it to Assist. Instead:

1. **The poll finds a missing calendar** as today: an IMAP entry whose device name is a mail application the service
   reports (`mail_apps` in `/v1/status`), and no calendar of that name. For each it raises a **repair** (fixable,
   warning), issue `mail_calendar_<name>` with the name lower-cased and its spaces as `_`:
   - title: "Mira needs the calendar „Pakete“" / „Mira braucht den Kalender „Pakete““;
   - text: "Mira puts what the mails of „Pakete“ announce into a calendar of the same name. Click Submit to create
     it; Mira then also uses it for answers." / „Mira trägt ein, was die Mails von „Pakete“ ankündigen, in einen
     Kalender mit demselben Namen. Klick auf Absenden, um ihn anzulegen; Mira nutzt ihn dann auch für Antworten.“
2. **The admin confirms** in Settings → Repairs. The repair's flow creates the local calendar of that name (the flow
   of `local_calendar`, as today) and exposes its entity to Assist. The repair goes.
3. **The repair goes by itself** when the poll finds the calendar (the admin made it by hand, or renamed one), or no
   longer finds the IMAP entry or its mail application.

What stays: the name convention, the service's handling of mails, the calendars of the test HA.

**Failures:**

| Step | Fails | Who learns of it |
|---|---|---|
| 1 | The poll cannot read the status | No repair raised or removed; the poll's error as today |
| 2 | `local_calendar` refuses or aborts | The flow shows its reason in the dialog ("Could not create the calendar: …" / „Der Kalender ließ sich nicht anlegen: …“); a log warning with the name and the reason; the repair stays for another try |
| 2 | The calendar was made, its entity is not in the registry, so it cannot be exposed | A log warning with the name; the flow says the calendar exists but must be exposed by hand (Settings → Voice assistants → Expose); the repair goes once the poll sees the calendar |
| — | A mail arrives while the calendar is missing | As today: the service ignores it with the log line `mail … action=ignored reason="no calendar …"`; the repair is already there |

The `_failed` set of `mail.py` (a calendar that failed once is not tried again until restart) goes: nothing is tried
without the admin.

**Cost:** per poll as today, the calendars' states and the IMAP entries in memory, no call; one repair per missing
calendar, created or deleted only when the set changes. Confirming: one config flow of `local_calendar`, one
exposure.

## 3. An admin's access, and automations through Home Assistant's API

### The access

1. **Setup** (the step "Start using Mira", after the overview): a short paragraph says what Mira does with admin
   rights and why, naming the owner: "Mira legt eure Aufgaben und Timer als Automationen an, so wie man es im
   Automationseditor tut. Das darf in Home Assistant nur ein Administrator. Mira bekommt dafür einen Zugang im Namen
   von {owner}, der Home Assistant eingerichtet hat; er steht in dessen Profil unter Sicherheit und kann dort
   jederzeit widerrufen werden." / "Mira creates your tasks and timers as automations, the way one does in the
   automation editor. In Home Assistant only an administrator may do that. Mira gets an access in the name of
   {owner}, who set Home Assistant up; it is in their profile under Security and can be revoked there at any time."
   Submitting is the consent. The integration makes a long-lived token "Mira (Integration)" for the owner (`is_owner` among Home
   Assistant's users), and sends it to the service as today (`POST /v1/pair` or `/v1/setup`). No user is created.
2. **Existing installs** (their service holds the token of the old user "Mira"): a fixable repair "Mira braucht
   einen neuen Zugang" / "Mira needs a new access" with the same paragraph. Submitting makes the owner's token,
   sends it with `POST /v1/setup`, and revokes the old user's token "Mira agent". Until then Mira runs as before;
   only saving and deleting a task or timer fails (below).
3. **Removing** the integration revokes the token, as today.
4. **An agent with Home Assistant's token in its own file** (`ha.token` in `mira.yaml`, no pairing): the integration
   made no token and does nothing. For tasks and timers that token must be an admin's; the guide says so. Without
   it, saving says what the tool's error says below.

| Step | Fails | Who learns of it |
|---|---|---|
| 1, 2 | Sending the token to the service fails | As today: the form says why, the token is revoked again, nothing half set up |
| 2 | The repair is not confirmed | A task's or timer's save says it in the turn: "Dafür braucht Mira erst einen neuen Zugang: Einstellungen → Reparaturen." / "Mira first needs a new access for that: Settings → Repairs." The tool's error; a log line with the command and Home Assistant's answer |
| — | The token is revoked in the owner's profile | The integration finds its token gone (it keeps the token's id; checked at each status poll, a lookup in Home Assistant's memory, no call). It makes a new token for the owner, as at the setup, and sends it with `POST /v1/setup`; a log line "made a new token for Mira, the old one was revoked". Until then the service's calls are refused: the problem of the source `ha` in `/v1/status` ([Context](context.md)) |

### Saving, reading, deleting

What the service does instead of the integration's writing commands. Each call is one the automation editor makes;
the service's token is an admin's.

- **Save a task** (new or changed): (1) `mira/automation/validate` at the integration, as today (Mira's checks:
  the confirm list, actions without a target only in exposed scripts, unchanged steps of a household task kept);
  (2) `POST /api/config/automation/config/<id>` with the config: Home Assistant writes `automations.yaml` and
  answers at once; (2b) `automation.reload` with the id, blocking: it returns once the automation is loaded, the
  signal that its entity exists (check C1); (3) the label "Mira" (a new task),
  done by the integration's `mira/automation/label` (admins only), which finds the entity by the automation's id in
  the registry and creates the label if missing: no list goes over the wire. A household task keeps its labels.
  Cost: four calls; the turn waits for one automation's reload (milliseconds on the test HA).
- **Save a timer**: (1) `mira/once/build` at the integration: the timer's automation from title, due time, owner,
  area and actions, checked like a task (before 0.18 part of `mira/once/save`); (2) and (3) as a task, with the label
  "Mira: einmalig".
- **Read**: `mira/automation/list`, `mira/automation/get` and `mira/once/list` stay with the integration, with
  `require_admin`; they only read.
- **Delete**: `DELETE /api/config/automation/config/<id>` (check C2), after one read of Mira's tasks
  (`mira/automation/list`). The service deletes only an automation with
  one of Mira's labels ("Mira", "Mira: einmalig"); any other is the tool's error "Das ist keine Aufgabe von Mira."
  / "That is not one of Mira's tasks." (the review: "removes any automation").
- **Cleaning up timers** moves from the integration's status poll to the service: a timer's automation goes once
  it has run. Signal: its `last_triggered` set and its state no longer running, from a state subscription filtered
  to the timers' entities (they carry "Mira: einmalig"); at the service's start once through `mira/once/list` for
  what ran while it was down. Cost: one subscription to the timers' entities, at most ten per person and room.

The integration's `automations.py` keeps the checks, the reading and the labels' names; its `save`, `delete`, the
YAML reading and writing and `_agent_only` go. `mira/agent/done` (an agent's result as an event, no automation
data) gets `require_admin` too: its only caller is the service.

| Step | Fails | Who learns of it |
|---|---|---|
| validate / build | Mira's check refuses | As today: the reason in the tool's result, the model says it |
| POST | Home Assistant refuses the config, or `automations.yaml` is not included | Home Assistant's answer in the tool's error; a log line with the id and the answer. Nothing was saved |
| label | The label cannot be set after a successful save | The task exists without its label: a log warning with the entity and the error, and the tool says the task is saved but not marked, so it does not show among Mira's tasks |
| DELETE | Home Assistant refuses | Its answer in the tool's error; a log line |
| cleanup | A finished timer cannot be deleted | A log warning with the timer and the answer, as today; tried again at its next state change or the next start |
| any | The token is not an admin's (repair of step 2 not confirmed) | See the table above |

### Older and newer versions (decided 2026-11-07)

A departure from the interface rule (new fields optional both ways): the integration's writing commands
(`mira/automation/save`, `mira/automation/delete`, `mira/once/save`, `mira/once/delete`) are gone, and the service's
new calls (`mira/once/build`, `mira/automation/label`) are unknown to an older integration. While only one side is
updated, saving and deleting tasks and timers fail with Home Assistant's "unknown command" in the tool's error and the
log; everything else works. The reason: keeping the old commands would keep the integration writing
`automations.yaml`, which the review rules out. Both come in the same release, and the version repair
([Versions](versions.md)) says which side to update.

### Checks on the test HA before building

| # | What | Checked |
|---|---|---|
| C1 | `POST /api/config/automation/config/<id>` writes the item into `automations.yaml`, reloads it and answers once the automation entity exists in the registry | 2026-11-07: **partly.** It writes and answers 200 at once (13 ms); the reload runs in a task after the answer, so right after it the entity is not yet in the registry (it was 7 ms later). Signal for the label: `automation.reload` with the id, called blocking after the POST, returns once that automation is loaded (HA's source: `post_write_hook` in `components/config/automation.py`) |
| C2 | `DELETE /api/config/automation/config/<id>` removes the item and the entity's registry entry | 2026-11-07: yes. Answers 200, the item is gone from the file at once, the registry entry within a second (a task after the answer), the state is gone (404) |
| C3 | A config flow (also one started by discovery) and a repair flow know the admin who submits | 2026-11-07: **no.** A config flow's context has only `source` (and `entry_id` when reconfiguring), a repair flow's only `issue_id` (HA's source: `get_context` in `components/config/config_entries.py`, `components/repairs/websocket_api.py`); the flow's code never sees the user. Hence the owner's token (decision 2) |
| C4 | The same calls with a non-admin token are refused with 401, so step 2's error is clear | 2026-11-07: yes. POST, GET and DELETE with the non-admin token: 401, nothing written |

## Documentation

- The guide (de/en): the model choice without "From Home Assistant"; for mail skills ("Pakete", own mail skills), the
  repair to confirm instead of "Mira creates the calendar"; Mira's admin access, why, and where it is revoked; the
  old user "Mira" may be deleted.
- Once built, [Setup](setup.md), [Models](models.md), [Mail calendar](mail-calendar.md), [Parcels](parcels.md),
  [Tasks](tasks.md), [Scheduled actions](scheduled-actions.md), [Watching states](ha-integration.md) and
  [System](../system.md) say the new behaviour and point here.

## Steps

0. **Spec** (this file). Done 2026-11-06.
1. **The model's source** (decision 1): the choice "From Home Assistant" goes. Scenarios HD-01, HD-02, in pytest.
   Built 2026-11-07.
2. **The mail calendar's repair** (decision 3). Scenarios HD-10 to HD-13, in pytest. Built 2026-11-07; run on the
   test HA (the IMAP device renamed "Pakete anna": the repair after one poll, confirmed, the calendar made and
   exposed, the repair gone).
3. **An admin's access, automations through Home Assistant's API** (decision 2): first the checks C1 to C4, then
   the setup's paragraph and token, the repair, the service's saving, deleting and cleanup, `require_admin`.
   Scenarios HD-20 to HD-27: the integration's in pytest, the service's in Go. Built 2026-11-07; run on the test HA
   with the owner's token at the fake Show: a sleep timer of one minute saved (labelled "Mira: einmalig"), run
   and removed in the same second by its state change; a task proposed, confirmed, saved (labelled "Mira") and
   deleted by voice; with the old non-admin token a timer is refused and Mira says where to give the new access.
   Not run on the test HA: the setup's consent and the repair for the new access (its entry was set up before
   tokens were kept; pytest only).
4. **Release and review:** guide, versions of the service and the integration, release with notes, then the review
   is requested again on the pull request.
