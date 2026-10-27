# Integration and agent in different versions

Status 2026-10-26: built (the rule for the interface, the repair, the first places that follow it). See also
[HA connection](ha-anbindung.md).

## Why

The agent and the integration are released together under one version, but they are not updated together. HACS
updates the integration; nobody updates the agent's container but the user. For days or weeks an integration may
talk to an older agent, or, the other way round, a new agent to an older integration. It happened on 2026-10-26:
the integration 0.9.0 from HACS sent persons with the new field `voice_threshold`; the agent 0.7.0 writes persons
into `mira.yaml`, which it reads strictly, and an unknown field there would have stopped the whole sync of persons,
satellites and exposure.

## The rule for the interface

Between integration and agent, **a new field is always optional**, in both directions:

- **The receiver ignores what it does not know.** A field a newer partner sends is left out, not refused; what the
  receiver does not know waits for its update.
- **The receiver copes with what is missing.** A field an older partner does not send has a default; a path an
  older agent does not have (HTTP 404) means "not there yet", not an error.
- **A field never changes its meaning.** What changes in meaning gets a new name; the old one goes only after a
  release in which neither side needs it.

Every change to what the two exchange is checked against this rule (requests and answers of `/v1/*`, the managed
config, the panel proxy).

`mira.yaml` itself stays strict: a typo there is the user's and is shown. Only what the integration sends is taken
leniently, before it becomes part of the file.

## Built

- **Persons from Home Assistant** (`PUT /v1/ui/ha`): of each person the agent keeps the fields `mira.yaml` has a key
  for (from the struct `config.Person`, no second list) and logs the others once ("fields from Home Assistant this
  Mira does not know, left out"). Rooms and the rest of the body were read leniently already (JSON).
- **The provider list** (`GET /v1/setup/providers`, since 0.8.0): an agent without it answers 404; the integration
  takes that as "no providers" and offers a model of one's own, instead of stopping the setup with an error.
- **The answers** the integration reads (`/v1/health`, `/v1/status`, …) are read with defaults (`.get`), except
  fields every agent has sent from the start (`id`, a problem's `name`).

## The repair: which one to update

The integration compares its own version with the agent's, by major and minor (0.7 against 0.9); a patch apart is
fine.

- **Where the agent's version comes from:** `/v1/status`, which the integration reads every minute, says it
  (`version`, new); an older agent does not, then the version from `/v1/health` at setup applies.
- **Apart:** a repair in Home Assistant (`version_mismatch`), in simple words, with the next step:
  - the agent older: "Update the Mira service to 0.9.0: for a container, set its image to 0.9.0 and start it again";
  - the agent newer: "Update the integration in HACS to 0.10.0 and restart Home Assistant".
- **The same again** (after an update): the repair goes away by itself on the next poll.
- No version at all (an agent that does not say one): no repair.

The repair does not prevent anything; the rule above keeps the two working while they are apart, the repair says
how to end it.

## Scenarios

- **VE-01** A newer integration sends a person field the agent does not know: the sync goes through, the field is
  left out (test `TestHomeAssistantFieldsFromANewerIntegration`).
- **VE-02** An agent a minor version behind: repair "update the agent"; a patch apart or the same version: none
  (`test_older_agent_is_a_repair_until_updated`); the agent newer: "update the integration".
- **VE-03** An agent without `version` in `/v1/status`: the version from `/v1/health` applies.
- **VE-04** An agent without the provider list: no providers, no error.
- **VE-05** The start page's footer shows the agent's version and, inside Home Assistant, the integration's: its
  proxy sends `X-Mira-Integration` with everything it passes on, the page too; an older integration sends none,
  then only the agent's shows (`TestPageShowsTheIntegrationsVersion`, `test_proxy_says_the_integrations_version`).

## Later

- **One release channel:** a Home Assistant app updates the agent with the integration
  ([App](app.md), built, not yet tried on Home Assistant OS).
- **An update entity** ("Mira service 0.10.0 available") instead of a repair, where Home Assistant cannot update
  the agent itself: open.
