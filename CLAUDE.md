# Working on Mira

Mira is a voice assistant for Home Assistant: the Mira service (Go, `cmd/mira`, `internal/`), the Home Assistant
integration (`custom_components/mira`), the HA app that runs the service (`app/`), and the skills (`skills/`). Mira
FRITZ!Box ([miraassist/mira-fritz](https://github.com/miraassist/mira-fritz)) follows the same rules.

## How work goes

- **Plan, propose, go, build.** What you notice is a proposal with options; wait for a go. Build exactly what was
  asked. A question is answered, not acted on.
- **Every question with lettered options** (A, B, C), also yes/no; several decisions numbered (1, 2, 3).
- **No fallbacks or workarounds of your own**: an edge case or a problem is discussed first.
- **Replies to the maintainer in German**; everything in the repository in English (specs, code, comments,
  commits), except the user-facing texts and the guide, which come in German and English, and the skills, which stay
  German.
- **Terms**: the Mira service, the HA app, the integration; a *turn* (one conversation as Home Assistant conducts it)
  and a *thread* ("Vorgang", a concern across turns). "The agent" means one of Mira's helpers, never the service.

## Specs first

- A feature starts as `docs/spec/<topic>.md` with its goal, the decisions (with date), what it does, and numbered
  **Steps**; its testable rules in `docs/spec/<topic>.feature`.
- **Every tool and every step a voice turn waits for says what it does step by step and what it costs**: calls to
  Home Assistant or devices, downloads, subscriptions, waits, and what the turn waits for versus what runs after it.
  Anything that grows with the number of items is named.
- Every scenario has a fixed id (`AB-07`) that is never reused, and a tag `@stepN` for the step that builds it.
- A built step goes into the map in `docs/scenarios_test.go`; every scenario of it then needs a test named
  `TestAB07_…`.
- The spec's status line and its steps say what is built.

## Design rules

- **Do not rebuild what Home Assistant offers.** Its tools over MCP, its entities, events, actions and registries;
  media through Music Assistant.
- **Convention over configuration; few settings.** A setting for an edge case is not added. A setting lives in one
  place only; `mira.yaml` is the one truth, the pages write it.
- **The interface between the integration and the service**: new fields are optional both ways, so an older service
  and an older integration keep working. Check every change of it on both sides.
- **Push, not polling; filter at the source.** Watch what reports a change (state subscriptions filtered to the
  entities or domains needed, events, triggers); never fetch everything to filter it here, and never join two full
  lists here. Polling only where nothing reports a change, and the spec says why.
- **Signals, not timing.** A sequence (play, then speak, then listen) is coordinated by clear signals of state
  transitions: a state changes, an event comes, a turn arrives. Never by computed durations, estimated delays or
  sleeps. A time is only a safety net, or the very last resort where no signal exists, and then the spec says why.
- **Handlers of a subscription never block**: they run on the connection's read loop and only pass the change on.
- **Whose a thread is follows clear rules** ([Threads](docs/spec/vorgaenge.md), Assignment): a thread is bound to a
  person or a room by its type; who speaks is decided by the voice alone.
- **User-facing texts** (pages, setup, options, repairs) are for Home Assistant users who are not technical: the next
  click, no terms they do not know.
- **Keep it understandable**: the simple variant; no elaborate safeguards where the worst case is a nuisance.
- **Public repository**: no facts of a real setup (addresses, host names, users, paths, secrets), only abstract
  recipes.

## Releasing

- **Semantic versioning**: a patch for fixes, a minor version for features and changed configuration. Every live
  change raises the version of the service and of the integration.
- **The guide is updated before a release**: new pages, new skills, screenshots.
- **`scripts/check.sh` before every push** (Go tests with the scenario check, pytest, hassfest).
- Push and tags only to `origin` (miraassist).
