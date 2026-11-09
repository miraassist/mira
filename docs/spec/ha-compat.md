# Compatibility with Home Assistant – Spec

Status 2026-11-05: step 1 built, not released. Scenarios: [ha-compat.feature](ha-compat.feature).

## Goal

Mira relies on things Home Assistant does: the MCP server of the Assist API, its prompt, websocket commands, actions,
the OAuth helper. A new Home Assistant version can change any of them, and Mira then works worse without anybody
noticing: Home Assistant 2026.11 renamed the prompt, and Mira ran without it for hours. The suite checks what Mira
relies on against the running test Home Assistant, after every update of it, as fixed checks instead of improvised
ones.

## Decisions (2026-11-05)

1. **A command of `testha`**: `go run ./cmd/testha compat`. It runs against the test Home Assistant in `testenv/`,
   not in `scripts/check.sh` (that needs no running Home Assistant).
2. **Mira's own code makes the calls**: the checks use Mira's MCP host, its prompt cache and its websocket client, as
   the service does, as the user `mira` (no admin). What is checked is Mira's way, not a copy of it.
3. **It runs after every update of the test Home Assistant** (step in [system.md](../system.md), Testing).
4. **Small first**: step 1 has the checks without a model. The turns (a follow-up with the same conversation id,
   `start_conversation` with an answer by voice) are step 2.

## Building blocks

The concept it hangs on is `testha contract`: a command that compares the test Home Assistant with what Mira
expects and says per item whether it matches. Departure: `contract` compares the fakes with the real devices,
`compat` compares Home Assistant with Mira.

## What it does

Each check prints `ok` or `FAIL` with its evidence (what came, what was expected); the command ends with an error when
one failed. The checks, step 1:

| Id | What Mira relies on | Checked |
|---|---|---|
| HC-01 | Home Assistant's MCP server at `/api/mcp/assist` answers Mira's token (the owner's) | Mira's MCP host connects with the token `TESTHA_ADMIN_TOKEN` |
| HC-02 | The tools Mira relies on by name are there: `GetLiveContext`, `GetDateTime`, `HassTurnOn`, `HassTurnOff` | in the host's list, with any prefix; `GetLiveContext` and `GetDateTime` are read-only by Home Assistant's own annotation |
| HC-03 | Home Assistant's prompt is there, the device lines at its end | Mira's prompt cache with the satellites: a text, no problem, and each satellite's own part is less than a tenth of it |
| HC-04 | The websocket commands the service sends answer Mira's token | the registries (areas, devices, entities, none empty), the states, the actions, the history of one entity and the statistics of one sensor |
| HC-05 | The actions Mira calls exist | `assist_satellite.announce`, `assist_satellite.start_conversation`, `tts.speak`, `media_player.play_media`, `media_player.media_stop`, `notify.send_message`, `light.turn_on`, `light.turn_off` in Home Assistant's actions |
| HC-06 | The integration loads cleanly | Home Assistant's log since the container's start has no deprecation of the integration's use of Home Assistant ("deprecated", "breaks in", "will stop working") and no failed setup ("Error setting up", "Setup failed", "Error loading"); that the Mira service was not reachable for a while is no matter of compatibility |
| HC-07 | The OAuth helper can send the secret as HTTP Basic | `LocalOAuth2Implementation` has `token_auth_method` |

**Costs:** HC-01 to HC-05 are calls to Home Assistant: one MCP connection with its tool list, the prompt once and once
per satellite, eight websocket calls. HC-06 reads the container's log (`docker logs`), HC-07 runs one line of Python
in the container. No model, no device, no download; a few seconds.

**Where a check's logic is tested:** against the fakes (`fakemcp`, `fakeha`), in Go: a check that passes with what
Home Assistant serves and fails with what is missing. The log check and the OAuth check read text; they are tested
with recorded lines.

## Steps

1. **The checks without a model** HC-01 to HC-07, with `testha compat`. Scenarios HC-01 to HC-04. Built.
2. **The turns** (follow-up with the same conversation id; `start_conversation` with Mira's token and an answer by voice
   at a fake satellite), with the model, switchable off. Not built.
