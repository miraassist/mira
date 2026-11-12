# HACS default catalog – spec

Status 2026-11-12: decisions 1 to 4 taken, checks C1 to C4 done; steps 1 to 3 built and released (0.18.0), decision 4
(the zip) built and released. Second review (2026-11-12): decisions 5 to 7 taken, checks C5, C7 and C8 done; steps 5 and 6 built.

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
4. **The integration comes as a zip at every release, so its downloads are counted** (2026-11-08). Without it HACS
   takes the repository's archive of the tag, which GitHub counts nowhere. So `hacs.json` says `zip_release: true`,
   `filename: mira.zip`, and the release workflow attaches `mira.zip`: the tracked files of
   `custom_components/mira` at the zip's root (`git archive`), since HACS unpacks the zip into
   `custom_components/mira` (HACS's `async_download_zip_file`, checked in its source 2026-11-08). HACS reads
   `hacs.json` from the tag it installs (`async_get_hacs_json` with the version's ref), so releases before this one
   still install from the archive, and the change takes effect with the next release.
   - **What it counts:** downloads of the zip, summed over all releases: every install and every update through
     HACS, not users (whoever updates ten times counts ten times). GitHub's `download_count` per asset, read by
     `GET /repos/<owner>/<repo>/releases` (one call, up to 100 releases).
   - **Cost:** one asset more per release, made from the files already checked out; nothing at Home Assistant's
     side.
   - **When it fails:** `git archive` fails → the release job fails before anything is published, the workflow's
     log says why (no release without the zip). The asset missing at a release that says `zip_release` → HACS
     refuses that version ("Download of mira.zip was not completed") and the user stays on the installed one; the
     HACS check of hacs/default flags it as well.

The second review (2026-11-12) accepts decisions 1 and 3 and objects to how decision 2 gets its token: the
integration mints a ten-year token of the owner, whichever admin sets up, and makes it anew when it is revoked; and
Mira's rule "delete only what carries her label" rests on a label anyone can set. The precedent is
`homeassistant-ai/ha-mcp`, approved by the same reviewer for the catalog (hacs/default#13222, 2026-11-04): an MCP
server for language models with an admin's token, whose tools write and delete any automation; its first submission
(#8968) was refused for minting an admin token, and accepted once the admin supplies it (its
`server_credentials.py`). Decisions 5 to 7 follow that precedent and keep decision 2's tasks as real automations.

5. **The admin gives Mira the token, and that is all** (2026-11-12): at setup the admin pastes a long-lived token of
   an admin; the integration mints and issues no token. Giving Mira admin rights is a deliberate act of the admin.
   ha-mcp offers a second way (a login for which it issues a token); Mira does not: one way is simpler. Replaces
   decision 2's "a long-lived token of the owner, made with an admin's consent"; the rest of decision 2 stays.
6. **No access, a repair** (2026-11-12): when the token does not work or no longer does (unknown, revoked, its user
   deactivated or no longer an admin), a repair asks for a new token, unless it is already there. Nothing is made
   anew in the background. Mira has no mode of her own for it: what Home Assistant refuses fails on the error paths
   that exist (section 3), and one notices it when using her. Replaces the row "The token is revoked in the owner's profile" of section 3.
7. **Mira deletes only what she created, by its id in her own storage** (2026-11-12): the service keeps the ids of
   the automations it created, tasks and timers, in its database. Deleting by the tool and cleaning up finished
   timers touch only those. The labels stay as markers for lists and the editor, no longer as a permission.
   Changing a household task stays as decision 2 says (its labels kept, Mira's checks on what is new or changed).

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

## 4. The admin's access and Mira's own automations (second review)

### The access the admin gives

1. **Setup:** "Start using Mira" (the overview's last choice) opens a form "Access for Mira" / „Zugang für Mira“
   with one field, the token (hidden). Its text (the wording of Home Assistant's menus as check C8 finds it):
   - de: „Mira legt eure Aufgaben und Timer als Automationen an, so wie man es im Automationseditor tut. Das darf in
     Home Assistant nur ein Administrator. Klicke dafür in deinem Profil unter Sicherheit → Langlebige
     Zugriffstoken auf „Token erstellen“, nenne es zum Beispiel „Mira“ und füge es hier ein. Du kannst es dort jederzeit widerrufen; Mira
     fragt dann unter Reparaturen nach einem neuen.“
   - en: "Mira creates your tasks and timers as automations, the way one does in the automation editor. In Home
     Assistant only an administrator may do that. In your profile under Security → Long-lived access tokens, click
     "Create token", name it for example "Mira", and paste it here. You can revoke it there at any time; Mira then asks
     for a new one under Repairs."
   Submitting checks the token (2), then pairs as today (`POST /v1/pair` or `/v1/setup` with the token).
2. **The check**, in Home Assistant's memory (no call): Home Assistant knows the token, it is long-lived, its user is
   active and an admin. The entry keeps only the token's id (`agent_token_id`), never the token.
3. **At each status poll** the integration checks the token by its id the same way. Does it fail:
   - the repair "Mira has no access any more" / „Mira hat keinen Zugang mehr“ (fixable, error) with the same text and
     field as the setup; submitting checks the token as in 2, sends it with `POST /v1/setup`, and the repair goes;
   - nothing else: what Home Assistant refuses fails on the paths that exist. A task or timer says „Mira braucht erst
     wieder einen Zugang: Einstellungen → Reparaturen.“ (section 3, `ErrNotAdmin`); a revoked token is the problem of
     the source `ha` in `/v1/status`, as today.
   Home Assistant keeps one repair per id, so raising it at every poll while it is open adds none.
   The repair goes by itself when the token works again (its user is an admin again).
4. **Removing** the integration revokes nothing: the token is the admin's, made in their profile, and removed there
   (decided 2026-11-12). The guide says so.
5. **An agent with Home Assistant's token in its own file** (`ha.token`, no pairing): unchanged, section 3, item 4.

Error texts of the form and the repair (de / en):

| Key | Text |
|---|---|
| `invalid_token` | „Dieses Token kennt Home Assistant nicht.“ / "Home Assistant does not know this token." |
| `token_not_long_lived` | „Das ist kein langlebiges Zugriffstoken.“ / "This is not a long-lived access token." |
| `token_not_admin` | „Dieses Token gehört keinem Administrator.“ / "This token does not belong to an administrator." |

| Step | Fails | Who learns of it |
|---|---|---|
| 1, 2 | The check refuses | The form shows the error above; nothing is sent. A log line at debug with the check that failed (never the token) |
| 1, 3 | Sending the token to the service fails | As today: the form says why; nothing half set up |
| 3 | The token's user is deleted | Home Assistant removes the user's tokens: as revoked |

**Existing installs:** after this step there are no tokens the integration made. The owner's token "Mira
(Integration)" it made (0.18.0 to 0.22.x) is revoked at the integration's first start after the update, with a log
line; the check of 3 then fails and the repair asks for a token (decided 2026-11-12). The old user "Mira"'s token is no admin's: the check of 3 fails, the repair comes; the old repair "Mira
needs a new access" and its code go.

**Cost:** setup, repair and each poll: one lookup in memory.

### Deleting only what Mira created

1. The service's database gets a table `own_automations`: the automation's id, its kind (`task`, `once`) and when it
   was created. A row is written when `POST /api/config/automation/config/<id>` succeeded for a **new** task or a
   timer (before the reload and the label: a failed label leaves an automation that is still Mira's). Changing a
   household task writes none.
2. **Deleting** by the tool: the id must have a row, else the tool's error "That is not one of Mira's tasks."
   as today; the label is not consulted. After `DELETE` succeeded the row goes.
3. **Cleaning up finished timers**: the state subscription and `mira/once/list` stay as they are (they find the
   timers by their label); a finished timer is deleted only when it has a row. A labelled timer without a row stays,
   with one log line per timer at the service's start ("a timer labelled 'Mira: einmalig' was not created by Mira,
   left alone", with its id).
4. Rows of automations deleted elsewhere (the editor) stay; they cost nothing and point nowhere. A later delete of
   such an id gets Home Assistant's "not found", which the tool says as today.
5. `mira/automation/label` stays (it marks, it permits nothing); listing Mira's tasks stays by label.

**Existing tasks and timers** (created before this, labelled, without rows): at the service's first look at the
timers with the table (its start, before anything is tidied), every automation labelled "Mira" or "Mira: einmalig"
(`mira/automation/list`, `mira/once/list`) gets a row once, and a mark in the table `kv` that it is done, in one
transaction; a log line says how many (decided 2026-11-12). A failure records nothing, tidies nothing, warns, and is
tried again at the next look (a change of a timer or a reconnect).

| Step | Fails | Who learns of it |
|---|---|---|
| 1 | The row cannot be written after a successful save | The automation exists, Mira cannot delete it later: a log warning with the id and the database error; the tool says the task is saved (it is) |
| 2 | The row cannot be read | The tool's error with the database error; a log line; nothing deleted |
| 2 | The row cannot be removed after a successful delete | A log warning with the id; harmless (item 4) |

**Cost:** one insert per created automation, one lookup and one delete per deletion; at the start one read of the
timers' rows (at most ten per person and room).

### Older and newer versions

The interface between the integration and the service does not change: the token goes as today with `pair` or
`setup`. The table is the service's own (a migration of its database, no data carried over).

### Answered questions

Decided 2026-11-12: the owner's token the integration made is revoked at the update; removing revokes nothing;
existing tasks and timers are taken over once; without access Mira has no mode of her own, a repair is raised
(decision 6).

### Checks on the test HA before building

| # | What | Checked |
|---|---|---|
| C5 | A long-lived token made in an admin's profile and pasted works for everything the service does (websocket, the automation config API, Home Assistant's MCP server) | 2026-11-12: yes. The test Mira runs with exactly such a token since 2026-11-07 (`/v1/status` without problems, tasks and timers saved and deleted) |
| C7 | Deactivating or deleting a user removes its tokens; taking away admin keeps the token | 2026-11-12: from Home Assistant's source (`AuthManager.async_deactivate_user`, `async_remove_user` remove the user's refresh tokens). Home Assistant's own permission checks are not tried again here; the integration's check reads `is_admin` at each poll, tested in pytest |
| C8 | The wording of Home Assistant's menus for long-lived tokens, de and en (the frontend's translations), for the texts above | 2026-11-12: from the frontend's translations of the test HA: de „Sicherheit“ → „Langlebige Zugriffstoken“ → „Token erstellen“; en \"Security\" → \"Long-lived access tokens\" → \"Create token\"; the texts above use them |

## Documentation

- The guide (de/en): the model choice without "From Home Assistant"; for mail skills ("Pakete", own mail skills), the
  repair to confirm instead of "Mira creates the calendar"; Mira's admin access, why, and where it is revoked; the
  old user "Mira" may be deleted. Since decision 5: making the token in the profile and pasting it, with a picture;
  what happens when it is revoked.
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
   is requested again on the pull request. Done with 0.18.0 (2026-11-07).
5. **The access the admin gives** (decisions 5 and 6): the setup's form, the
   token's check, the poll's check, its repair and Mira stopping; removing, existing installs. Scenarios HD-30, HD-31 and HD-33 to HD-38, in pytest (HD-32, a login, dropped). Retires HD-20, HD-21 and HD-27 (their tests go).
   Built 2026-11-12. Run on the test HA: the entry given a token id that does not exist, the repair came after the
   start (fixable, error) with one log warning; its form refused "not a token" with `invalid_token`. Not run there:
   submitting a valid token in the repair and the setup's form (the test Mira's `mira.yaml` is in the repository and
   `/v1/setup` would write the token into it; pytest only).
6. **Deleting only what Mira created** (decision 7): the service's table, deleting, cleaning up timers; existing
   automations taken over once. Scenarios HD-40 to HD-44, in Go. Retires HD-24.
   Built 2026-11-13: the table `own_automations` (migration 0028), `automation.Save`, `Delete` and `TakeOver`.
   Run on the test HA: at the start the one task labelled "Mira" was taken over (`count=1`); a sleep timer of one
   minute set at the fake Show was noted as `once`, ran, and was removed by its state change a minute later, its
   row gone. Not run there: deleting a labelled automation Mira did not create (Go only).
7. **Release and review:** guide, versions, release with notes; then the comment on the pull request with the
   release's link, and the review requested again.
