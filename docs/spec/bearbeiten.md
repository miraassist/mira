# Editing models, agents and skills on Mira's page

Status 2026-10-30: step 1 built, released in 0.7.0; provider list and model check in 0.8.0. Step 2 (the settings
sections in the file, nothing in the database) built 2026-10-30. Scenarios: [bearbeiten.feature](bearbeiten.feature).

## Goal

The pages Models, Agents and Skills (uebersicht.md) show what Mira loaded; until now they only read. An admin can
now change what is there and add new ones, without opening a file on the server.

## One truth: the file

- **mira.yaml stays the only truth.** The page writes straight into it: Mira reads the file afresh, changes only the
  entry in question in its YAML tree (comments and order stay) and writes it back safely (a temporary file, then a
  rename). What is in the file is always what holds; a change by hand counts the same as one from the page. There is
  no second store that overlays the file.
- `${VARIABLE}` in the file stays as it is written; the page never puts a value in its place.
- **Where the file is:** the config path (`-config`, `MIRA_CONFIG`). If there is no file there and its folder cannot
  be written (the container: `/etc/mira`), Mira uses `mira.yaml` in the data folder (`MIRA_DATA`), reads it from
  there at start and creates it at the first save. A file that exists but cannot be written: the page says so, and
  editing goes by hand only.
- **Skills:** a skill's settings and whether it is on are in mira.yaml (its entry under `skills`, off with `enabled: false`); a skill's instructions,
  agents and thread (target) are in its `SKILL.md`, and the page writes that file, for the user's own skills only.
- **The settings sections** (`notifier`, `persons`, `satellites`, `persona`, `web_search`) go into the file too; see
  [Settings sections](#settings-sections).

## Secrets

- **Not in mira.yaml.** A token or a secret setting entered on the page goes into Mira's secrets file in the data
  folder (`secrets.json`, readable only by Mira, mode 0600); mira.yaml gets a reference, `${NAME}`, so it can be
  shared or versioned. The name comes from the entry: `MIRA_MODEL_<MODEL>_TOKEN`, `MIRA_SKILL_<SKILL>_<SETTING>`
  (upper case, anything but letters and digits an underscore).
- **Where the names come from:** the secrets file first, then the environment. A name the page wrote holds even if
  the environment has an older value.
- **Encrypted, if a key is given:** with `MIRA_SECRET_KEY` set (an environment variable or Docker secret, not in the
  data folder), the secrets file is encrypted (AES-256-GCM, the key derived from it with SHA-256). A backup of the
  data folder alone then gives nothing away. Without it the file is plain text, mode 0600, and the page says so.
  A file encrypted with a key that is missing or wrong at start: Mira says so and starts without its secrets.
- **Never back:** the page shows only whether a secret is set; it never gets its value.

## Models

- Shown as before; **Edit** opens a form: provider, token (write only: empty keeps it, "remove" clears it), model,
  privacy, parallel, fallback, context size, reasoning effort, temperature, tool rounds, timeout.
- **Provider instead of address:** a list of the common cloud providers (OpenAI, Google Gemini, OpenRouter, Mistral,
  Anthropic) and, last, "Own server". A provider sets the address; only "Own server" shows the address field, also for
  local models. Privacy follows the choice (provider → cloud, own server → local) and can still be changed. Editing a
  model finds its provider by the address. The list is the agent's one list (docs/spec/modelle.md, Providers).
- **Model:** once the server is known (and the token, where one is needed), the page asks its `/models` and offers
  what it names as a list. When the server names nothing, the model stays a text field with a hint.
- **Check:** when a model is chosen, Mira tries it with a real request, as the setup in Home Assistant does. A refused
  key, an unknown model or one without tools is said below the field. When it answers, the context field gets the
  server's size (docs/spec/modelle.md, Context size). Saving tries again when server, model or token changed; what
  does not answer is not saved. Some servers list their models without a key (OpenRouter), so only this request
  shows a wrong key.
- **New model:** a name and the same form.
- **Rename:** another name in the form; the agents and fallbacks that name the model are changed with it.
- **main** can neither be renamed nor deleted. When the integration in Home Assistant sets main up, main is read only
  here, with a hint: change it in Home Assistant under Configure (one place for a setting).
- **Delete:** only a model no agent takes and none falls back to; otherwise the page names who uses it.

## Agents

- **Mira's own** (conversation.voice, delegate, …): model and privacy can be changed (an entry under `agents` with
  only these); their instructions are Mira's code.
- **The ones in mira.yaml:** description, model, max rounds, tools (none, read_only or a list), context (isolated or
  background), rights, in the conversation, privacy, instructions; input and output as YAML text, checked on save.
- **New agent:** a name and the same form.
- **Delete:** forbidden while it is used: another agent names it in its tools, a skill's thread names it, or it is
  one of Mira's own. The page names who uses it.
- **A skill's agents** are edited with the skill.

## Skills

- **Switch on and off** (`enabled: false` in its entry; switching off keeps the settings, and Mira's own skills can be switched off too, for a replacement), and its **settings**: one field per setting, with its description; a
  secret one goes into the secrets file.
- **The user's own skills** (in a skill folder other than the bundled one): the instructions (the text of
  `SKILL.md`), the description, the agents, the thread (`target`) and the MCP server (`mcp`), the last three as
  YAML text. Mira checks the new `SKILL.md` as it would load it before writing it.
- **Bundled skills** (Mira's `skills` folder): only on/off and settings; the rest is read only, since an update of
  Mira replaces those files.
- **MCP connection test:** for a skill with an MCP server, **Test connection** connects with the address and token as
  entered (or as set) and says whether it worked, how many tools the server has and which of the skill's tools are
  missing.

## Settings sections

The pages Notifications, Satellites and Persons (without the integration) and the integration's options "General"
(persona, web search, notifications) change whole sections of the configuration. Until 2026-10-30 Mira kept such a
change in her database and laid it over the file ("changed in the UI", "Back to mira.yaml"). Now:

- **Saving writes the section into mira.yaml.** Mira replaces the section in the file's YAML tree; the rest of the
  file, its comments and order stay, comments inside the section do not. A section that Mira would refuse is not
  written, and the page shows why (as in step 1).
- **Only what differs from Mira's defaults goes into the file** (2026-10-30), compared as Mira reads it (`30m` is
  `30m0s`), down into the parts of a section (the quiet hours, the escalation). A section left at its defaults has no
  entry. So the file stays short and a later default still reaches it.
- **It holds at once**, as before: Mira applies it to the running configuration and restarts the notifier; no restart
  of Mira.
- **The page shows no source any more:** no "changed in the UI", no "Back to mira.yaml". What is in the file is what
  holds. "Managed in Home Assistant" stays for what the integration sends.
- **Without a writable mira.yaml** saving is refused with the reason, as for models and skills.
- **Costs:** one save reads mira.yaml once and writes it once (a temporary file, then a rename); nothing grows with
  the number of entries beyond the file's size.

### What Home Assistant sends

The integration sends Mira's settings on HA's persons and satellites (`PUT /v1/ui/ha`): the sections `persons` and
`satellites` and, of `notifier`, `morning_trigger`, `room_mode` and `indicators` ([HA connection](ha-anbindung.md)).
These settings live in Home Assistant, on the persons and satellites; Mira writes them nowhere:

- **Mira holds them in memory only.** They are laid over the file while she runs.
- **After a restart of Mira** she has none of them until the integration sends again. While she has none since her
  start, she reports an **empty fingerprint** in `/v1/status` (`exposed_hash: ""`). The integration compares the
  fingerprint on every poll (every minute) and sends everything again when it differs (ha-anbindung.md). So the
  settings are back within a minute; until then the persons and rooms from HA are missing.
  - Why a poll: it is the integration's existing status poll, the only way Mira reaches the integration; nothing
    new is polled, and the integration needs no change, so an older integration does the same.
- **While the integration manages them,** the page refuses `persons` and `satellites`; a notifier saved on the page or
  in the options keeps the file's own `morning_trigger`, `room_mode` and `indicators` in the file and runs with the
  ones from Home Assistant.
- **Released** (the integration removed, `DELETE /v1/ui/ha`): the values from Home Assistant hold until Mira restarts;
  then the file's.
- The list of what is exposed to Assist and the login names stay stored as before (ha-anbindung.md): they are not
  settings, and without the list Mira does not act.

**What was in the database is not carried over** (no users yet, 2026-10-30): Mira no longer reads it.

## When a change holds

- Changes to models, agents and skills hold after a **restart**. After a save the page says "Restart needed" and has
  a button **Restart Mira**: Mira starts anew in her own process (as after setup).
- Before writing, Mira checks the whole new file as she would load it; what she would refuse is not written, and the
  page shows why.

## Rights

Only admins (in Home Assistant) or the operator with the token edit; the page of someone who is no admin shows
nothing of this.

## Decided

1. mira.yaml is the only truth; the page edits the entry in the file, no overlay in the database (2026-10-24).
2. Secrets go into a secrets file in the data folder, referenced as `${…}` with names derived from the entry;
   encrypted with AES-GCM when `MIRA_SECRET_KEY` is set (2026-10-24). Base64 was rejected: it hides nothing.
3. Changes hold after a restart, with a button for it.
4. Bundled skills: only on/off and settings.
5. Nested parts (thread, MCP, input/output schemas) as YAML text with a check, not as forms.
6. main is changed in Home Assistant when the integration set it up.
7. **Skills as entries** (2026-10-24): under `skills`, beside `dirs`, each key is a skill with its settings; without
   `enabled` it is on, `enabled: false` switches it off and keeps its settings. Mira's own skills are on without an
   entry and can be switched off, for a replacement of one's own. The old `skills.enabled` is refused with a hint,
   as `llm` and `routing` were (no users to migrate yet).
8. **A further model beside the integration's main** (2026-10-24): mira.yaml may name models without `main` when
   the integration sets `main` up (MO-02 relaxed); without either, the page refuses and Mira waits for the setup.
9. **Settings sections in the file, nothing in the database** (2026-10-30): the page and the options write the
   sections into mira.yaml; what Home Assistant sends Mira holds in memory and gets again after her restart through
   the empty fingerprint. The overlay in the database and its "Back to mira.yaml" are gone; nothing is carried over.

## Steps

1. Models, agents and skills edited in the file (built, 0.7.0).
2. The settings sections in the file; what Home Assistant sends in memory, fetched again after a restart (built
   2026-10-30).

## Open

- Nothing.
