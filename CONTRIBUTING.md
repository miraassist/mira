# Contributing to Mira

Thank you for helping. Issues and discussions are welcome in German or English.

Everyone here follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Something does not work

1. Update first: only the latest release gets fixes.
2. Look in the guide ([Deutsch](docs/guide/de.md) · [English](docs/guide/en.md)) and under Settings → System →
   **Repairs** in Home Assistant; many problems explain themselves there.
3. Open an issue with the form **Bug report**. What helps most: the exact sentence you said or wrote, what Mira
   answered or did, and the lines of Mira's log from that moment (for the Home Assistant app: Settings → Apps → Mira →
   **Log**).

Before you post a log, remove what belongs to your home: tokens, addresses, names of people and places.

A security problem does not go into a public issue; see [SECURITY.md](SECURITY.md).

## An idea

Open an issue with the form **Idea**, or start a thread under **Discussions**. Say what you want to do in your home,
not only how; often Home Assistant or an existing skill already has a piece of it.

## A skill

Skills are the easiest way to add something: a folder under `skills/` with a `SKILL.md` that tells Mira what to do,
with the tools she already has. No program code. The guide explains the format (section "Skills"). Mira's bundled
skills are written in German, with an English `SKILL.en.md` next to them.

## A pull request

Talk about a larger change in an issue first, so no work is lost. Then:

- **Spec first.** A feature starts as `docs/spec/<topic>.md`: its goal, the decisions, what it does, numbered steps,
  and what happens when a step fails. Its testable rules go into `docs/spec/<topic>.feature`, each scenario with a
  fixed id (`AB-07`) and a tag for the step that builds it.
- **Tests carry the scenario's name**: `TestAB07_…` in Go, `test_AB07_…` in pytest. A built step is entered in
  `docs/scenarios_test.go`; the check fails when a scenario of it has no test.
- **Run `scripts/check.sh` before you push.** It runs what CI runs: gofmt, go vet, staticcheck, `go test -race`, ruff,
  pytest, a syntax check of the page's JavaScript, and hassfest. It needs Go (the version in `go.mod`), Python with
  the packages of `requirements_test.txt`, [uv](https://docs.astral.sh/uv/) for ruff, Node.js, and Docker for
  hassfest (`--no-docker` skips it).
- **Language.** Code, comments, specs and commit messages in English. Texts users see (pages, setup, repairs) and the
  guide in German and English.
- **Commit messages** say what changes for the user, as a sentence: "A plan can be ended on the page".

### Design rules

- **Do not rebuild what Home Assistant offers**: its entities, events, actions and tools over MCP; media through
  Music Assistant.
- **Few settings.** A setting for an edge case is not added. `mira.yaml` is the one place; the pages write it.
- **Push, not polling**: subscribe to what reports a change, filtered at the source.
- **Signals, not timing**: a sequence waits for a state change or an event, not for a computed delay.
- **Every error leaves a trace**: a log line with what failed and what happens next; no silent fallback.
- **The integration and the service stay compatible both ways**: new fields between them are optional, so an older
  service and an older integration keep working.
- **No facts of a real setup** in the repository: no addresses, host names, users or secrets.

[docs/system.md](docs/system.md) describes the parts and the main flows.

## License

Contributions are released under the [Apache License 2.0](LICENSE), like the rest of Mira.
