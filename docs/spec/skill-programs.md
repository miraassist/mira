# Programs for skills – spec

Status 2026-11-19: specified; steps 1 to 5 built. Scenarios: [skill-programs.feature](skill-programs.feature).

## Goal

A skill whose tools come from a program next to Mira ([MCP servers in skills](skill-mcp.md), over `stdio`) should be
usable without a terminal: switching the skill on in "Fähigkeiten" is enough for Mira to get the program, and its
setup (a login, a choice of market) happens on Mira's page. Today the first such skill, `rewe`
([REWE shopping](rewe.md)), needs an admin who builds or downloads korb, copies it into Mira's config folder, logs it
in on a computer and copies its config folder after it.

## The concept it hangs on

- **The skill names its program** like a dependency: where it is published and its SHA-256 sum per architecture.
  Mira downloads it once, checks the sum, keeps it and starts it as the skill's MCP server. What the program needs
  for itself (korb: the REWE app's certificate) it does by itself on its first start; Mira knows nothing of it.
- **The skill gets a folder of its own** for its program's state (korb: the login, the delivery service), so the
  skill no longer needs a setting for it.
- **The setup is tools of the skill's server that only the page calls**, never a model. The skill lists them under
  `setup`; the page builds a form from each tool's input schema and shows the answer. Mira knows nothing of REWE: the
  same as for the OAuth connections ([OAuth](oauth.md)), where a skill declares its login and the page "Verbindungen"
  offers it.

## Decided (2026-11-19, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Where the program and the skill's state live | Under Mira's config folder (the folder of `mira.yaml`: `/config` in the HA app, the mapped folder of a container): `programs/` and `skill-data/<skill>/`, the same in both, no difference between app and container. No new volume and no new mapping; in the HA app it is in the backups. Not chosen: Mira's data folder (`/data`), which a container of one's own does not map today. Programs may run from there (checked 2026-11-19, below) |
| 2 | Which architecture | Mira's own (`amd64`, `arm64`), from the running service; the skill does not ask the user |
| 3 | Who does a program's first-run work | The program. korb fetches the REWE certificate itself on first use and checks it (rewe-cli 0.8.0) |
| 4 | The setup without a terminal | On Mira's page, admins only: tools of the skill's server listed under `setup`, never offered to a model; forms from their input schemas. Not chosen: program commands in the skill (a convention of its own for output and errors, where MCP has one); the login in Mira's OAuth (korb rotates its tokens itself, under a file lock); the login in a chat with an admin (the redirect address would pass through the model, against [REWE shopping](rewe.md), decision 2) |
| 5 | Where rewe-cli comes from | Its releases at [miraassist/rewe-cli](https://github.com/miraassist/rewe-cli) (a tag builds Linux and macOS, amd64 and arm64, with `SHA256SUMS`); the binaries hold no certificate |
| 6 | `rewe` a shipped skill or one of the subscription | Shipped, public, for now. A skill of the subscription ([Skill source](skill-source.md)) would bring its program from the skill server with it (Not part of it); the CLI stays public on GitHub either way: the open program and the skill together are the value |

## In the skill

```yaml
# skills/rewe/SKILL.md
metadata:
  mira:
    settings:
      limit:    { description: "Höchstbetrag einer Bestellung in Euro, z. B. 150", required: true }
      liste:    { description: "Die To-do-Liste mit dem Einkaufszettel; ohne: die Liste Einkaufsliste" }
      programm: { description: "Optional: ein eigenes korb statt des geladenen" }
    program:
      name: korb
      version: 0.9.0
      url: https://github.com/miraassist/rewe-cli/releases/download/v0.9.0/korb_linux_{{ arch }}
      sha256:
        amd64: "…"
        arm64: "…"
    mcp:
      transport: stdio
      command: "{{ programm }}"          # empty: the program above
      args: [mcp]
      env: ["KORB_CONFIG_DIR={{ data }}", "KORB_MAX_TOTAL_EURO={{ limit }}"]
      tools: [status, basket_fill, …, order_cancel]
      confirm: [order, modification_submit, order_cancel]
      setup:
        - { tool: status,         label: "Stand prüfen" }
        - { tool: login_start,    label: "Bei REWE anmelden" }
        - { tool: login_complete, label: "Anmeldung abschließen" }
        - { tool: store_search,   label: "Lieferdienst suchen" }
        - { tool: store_set,      label: "Lieferdienst wählen" }
```

- **`program`**: `name` (a word: letters, digits, `-`), `version` (shown on the page and in the log), `url` with the
  placeholder `{{ arch }}`, and `sha256` per architecture. A skill without a sum for Mira's architecture is a problem
  of the skill ("not for this architecture"); so is a `program` without `url` or `sha256`.
- **`command`** of a stdio server: filled (a setting with a path) it wins, as today, for one's own build; empty, the
  server starts the skill's program. A stdio server with neither is a problem of the skill, as today.
- **`{{ data }}`**: the skill's folder (below), in every text of the `mcp` section and in the prose. A secret is not
  in it; it is a path.
- **`setup`**: a list of `tool` (a tool of the skill's server) and `label` (the button, in the skill's language;
  a translation may translate it, [Skill languages](skill-languages.md)), optional `description` (the help above the
  form; without it the tool's own description). A setup tool reaches no model unless `tools` names it as well, on
  purpose (`status`: the page checks the setup with it, the model the readiness); a setup tool under `confirm` is a
  problem of the skill (nobody asks for a yes on the page). A `setup` tool the server does not offer is said on the
  page, not a problem (an older program).
- The settings `programm` and `konto` of `rewe` go: `konto` is `{{ data }}`, `programm` stays optional for one's own
  korb. A `mira.yaml` that still has `konto` starts; the log says once that it is no longer read.

## The program

**When Mira gets it:** at start and when a skill is switched on or its file changes (the skills are loaded anew),
before its MCP server is started. Until the program is there the skill's tools are missing, as for a server that
cannot be reached; the skill shows "Programm wird geladen" on its entry.

**Where:** `<config>/programs/<name>-<version>-<first 12 hex of the sum>` (`korb-0.9.0-3fa4…`), mode 0755, written
to a temporary name in the same folder and renamed only after the sum matched. A file that is already there with the
right sum is not downloaded again; one with a wrong sum (changed on disk) is replaced.

**The download:** HTTPS only, redirects followed (GitHub's releases redirect to their storage), at most 100 MB, a
timeout of 2 minutes, through Mira's proxy settings if any. Failing (no network, 404, a wrong sum) the skill has a
problem naming the address and, for a wrong sum, both sums; Mira tries again after 10 minutes and at the next start,
not in a loop.

**Updates:** only with the skill: a new `version`/`sha256` in its file (Mira's release, or a skill source) leads to a
new file. A program nobody names any more is deleted at the next start. Mira never takes a newer version from the
publisher by herself: the sum in the skill is the trust.

**Running:** as today's stdio server ([MCP servers in skills](skill-mcp.md)): Mira's minimal environment
(`StdioEnv`) plus the skill's `env`, the working folder `{{ data }}`.

## The skill's folder

`<config>/skill-data/<skill>/`, created with mode 0700 when the skill is loaded, for every skill with a stdio server.
It survives restarts and updates and is in the HA app's backups. Switching a skill off keeps it (switching on again
finds the login); deleting it is the admin's, by hand. The page shows the path on the skill's entry.

## The setup on the page

On the tab "Fähigkeiten", the entry of a switched-on skill with `setup` gets a section "Einrichten" (admins only):

- One button per entry, in the skill's order, with its label. Above it the description.
- **Fields** come from the tool's `inputSchema`: a string is a text field, an integer a number, a boolean a switch,
  an `enum` a choice; `required` fields must be filled. A tool without properties is just a button.
- **The answer** is shown below the button: JSON as a short list (keys and values), addresses (`http…`) as links that
  open in a new tab, an error (`isError`) in red with its text. Nothing of it goes to a model or to the memory.
- **Calling**: the page asks the service (`POST /v1/ui/skills/<skill>/setup/<tool>`, admins only, as the other
  writing views of the page), which calls the tool on the skill's running server with a timeout of 60 s. The log
  says who called which setup tool of which skill, and the outcome; not the arguments (a redirect address carries a
  code).
- The hooks do not run for it: no model asks, an admin clicks. A setup tool that `tools` does not name never
  reaches a model: it is in no tool list, not after `skill_open` either, and a model's call of one is refused as an
  unknown tool.

## korb's part (rewe-cli 0.9.0)

Four tools more in `korb mcp`, beside `status`:

| Tool | What it does |
|---|---|
| `login_start` | starts a login: `{"authUrl": …, "hint": …}`; the hint says to sign in with Chrome and to copy the address `de.rewe.app://redirect?…` from the console. A login attempt stays valid for 30 minutes |
| `login_complete` | `url`: the copied address; stores the tokens; answers logged in or REWE's error |
| `store_search` | `zip`: the delivery service and the pickup markets for a postcode |
| `store_set` | `id`, `zip`: selects the delivery service or a market |

They are not listed in the skill's `tools`, so no model gets them (decision 2 of [REWE shopping](rewe.md) stays: the
login is the owner's, not the model's).

## Safety

- **The program is code Mira runs**, in the HA app beside `/config` with all of Home Assistant's secrets. The sum in
  the skill is what is trusted: a program whose sum does not match is never written under its name or started. Who
  can change the skill's file decides which program runs: Mira's release for a shipped skill; for a skill of a skill
  source the source over HTTPS with its key ([Skill source](skill-source.md); its skills are not signed). That is the
  same trust as for the skill's prose and tools today.
- **No newer program by itself** (above): a compromised release changes nothing until a skill names its sum.
- **The skill's folder** holds the program's state, for korb the REWE session, readable for whoever reads the config
  folder (as `mira.yaml` and its secrets).
- **Setup tools** only for admins and never for a model.

## Problems

| What fails | What happens | Who learns of it |
|---|---|---|
| No sum for Mira's architecture | the skill is not loaded | Repairs, the skill's entry: "korb gibt es nicht für arm64" |
| The download fails | the skill's tools are missing; again after 10 min and at the next start | the entry and the log, with the address and the error |
| The sum does not match | nothing is written or started | Repairs: both sums and the address |
| The program does not start (not executable, `noexec`) | as a server that cannot be started today | Repairs, with the error |
| A setup tool fails | its answer in red | the admin on the page |

## Costs

Once per program version: one download (korb: about 10 MB), one SHA-256 over it. Per start: one `stat` and one
SHA-256 of each program (a few ms). Per setup click: one tool call. No model call anywhere.

## Checks on the test HA before building

1. **`noexec`** (checked in the sources 2026-11-19): Supervisor mounts an app's `addon_config` and `/data` as plain
   bind mounts without options (`supervisor/docker/app.py`, `DockerMount` has no `noexec`; Docker cannot set it on a
   bind mount, the source's options hold), and HAOS mounts its data partition, where both live beside the images of
   all apps, with `Options=commit=30` only (`mnt-data.mount` in `home-assistant/operating-system`): programs may run
   from `/config`. To be seen once on HAOS when step 3 is built.
2. **The image**: a static Go binary (korb, `CGO_ENABLED=0`) runs in Mira's distroless image as root, and as
   `nonroot` in a container of one's own.
3. **The download** from GitHub's releases from inside the HA app (redirect to `objects.githubusercontent.com`).

## Steps

1. **korb's setup tools** (rewe-cli 0.9.0, released): `login_start`, `login_complete`, `store_search`, `store_set`,
   tested against `korb-fake`. PG-12, PG-13 in rewe-cli.
   Built 2026-11-19: released as v0.9.0 (Linux and macOS, amd64 and arm64, `SHA256SUMS`). `status` now names a
   missing login and a missing service apart (before, without a service it said nothing of the login).
2. **The checks** above on the test HA (and HAOS if it differs), written into this spec.
   Done 2026-11-19 as far as possible without HAOS: `noexec` in the sources (above); the released korb runs in
   Mira's image `ghcr.io/miraassist/mira:0.27.0` as root and as `nonroot` from a mounted folder, and fetches its
   certificate from GitHub inside it (the image's CA certificates suffice). On HAOS itself: when step 5 runs there.
3. **The program and the folder** in Mira: `program`, `{{ arch }}`, `{{ data }}`, download, check, keep, clean up,
   problems. PG-01 to PG-08, against a local HTTP server in the Go tests.
   Built 2026-11-19 (`internal/programs`, `skills.LoadWith`): the programs are got at Mira's start, as the skills
   are loaded only then; a program no switched-on skill names is deleted at that start (not one start later). The
   server's working folder is the skill's folder. A skill switched off loses its program at the next start (got
   again when switched on), not its folder.
4. **The setup** in Mira: `setup` in the skill, the service's endpoint, the section on the page. PG-09 to PG-11.
   Built 2026-11-19: `setup` is a field of the server's entry (`config.MCPServer`), so it also works under
   `mcp_servers`; the host keeps a setup tool's definition apart and never in a model's list; the section is on a
   switched-on skill's entry with its program, its version (or "wird geladen") and its folder. The labels are not
   translated yet ([Skill languages](skill-languages.md) does not know them). The form is checked by the page's
   script check, not in a browser test.
5. **`rewe` switches over**: `program`, `{{ data }}`, `setup`; `konto` goes, `programm` optional. RW-07 and RW-08
   adjusted; the REWE evals run again (the eval runner keeps its wrapper through `programm`).
   Built 2026-11-19: `rewe` names korb 0.9.0 with the sums of its release; `konto` and `zertifikate` are gone (the
   skill was not released with them, so no `mira.yaml` has them; korb fetches its certificate itself). `status` is
   both a setup tool and the model's (decided while building: `tools` decides whether a setup tool reaches a model,
   only one under `confirm` is a problem). The REWE evals with Gemini Flash Lite for voice: 27 of 27; one answer said
   "Ich erreiche REWE gerade nicht." for a missing login, so the skill now names korb's `not logged in` (that
   scenario measured again, 3 of 3). The evals start korb through the wrapper: the download of the real release is
   tested in Go against a local server, and on Mira's image by hand (step 2), not in an eval.
6. **The guide** (de and en): switching on, "Einrichten", the login with Chrome's console.

## Not part of it

- Programs for skills of the HA side (skill scripts run in Home Assistant).
- Updating a program without a new skill file, and a program's own updater.
- Other kinds than a single executable file (archives, interpreters, containers).
- A program that comes from the skill server with a skill of the subscription (decision 6): when a skill of the
  source needs one; then `url` may name the source and its key goes with the download.
- A setup wizard with conditions ("first the login, then the service"): the order of the buttons and the program's
  `status` say it.
