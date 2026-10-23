# Editing models, agents and skills on Mira's page

Status 2026-10-24: built (not released). Scenarios: [bearbeiten.feature](bearbeiten.feature).

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
- **Skills:** a skill's settings and whether it is on are in mira.yaml (`skills.enabled`); a skill's instructions,
  agents and thread (target) are in its `SKILL.md`, and the page writes that file, for the user's own skills only.
- The old overlay of the sections satellites, persons and notifier in Mira's database stays for now (open point).

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

- Shown as before; **Edit** opens a form: address, model, privacy, parallel, fallback, context size, reasoning
  effort, temperature, tool rounds, timeout, token (write only: empty keeps it, "remove" clears it).
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

- **Switch on and off** (`skills.enabled`), and its **settings**: one field per setting, with its description; a
  secret one goes into the secrets file.
- **The user's own skills** (in a skill folder other than the bundled one): the instructions (the text of
  `SKILL.md`), the description, the agents, the thread (`target`) and the MCP server (`mcp`), the last three as
  YAML text. Mira checks the new `SKILL.md` as it would load it before writing it.
- **Bundled skills** (Mira's `skills` folder): only on/off and settings; the rest is read only, since an update of
  Mira replaces those files.
- **MCP connection test:** for a skill with an MCP server, **Test connection** connects with the address and token as
  entered (or as set) and says whether it worked, how many tools the server has and which of the skill's tools are
  missing.

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

## Open

- Move the overlay of satellites, persons and notifier onto the same principle.
- **A further model beside the integration's main:** mira.yaml with a `models` section must name main (MO-02,
  modelle.md). When main comes from the integration and the file has no `models` yet, the page cannot add a model
  without writing main into the file, which would make two places for main. Options: (A) MO-02 allows models without
  main when the integration sets main up; (B) the page writes main into the file once, read only there afterwards;
  (C) leave it: a further model needs main in the file, by hand. Until decided the page says so and writes nothing.
