# Security

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it privately on GitHub instead: the tab
**Security** of this repository, **Report a vulnerability**. Only the maintainers see the report.

Helpful in a report:

- what is affected: the Mira service, the Home Assistant integration, the Home Assistant app or a skill;
- the version (the integration shows it on its device page);
- the steps to reproduce it, and what an attacker gains.

You get an answer as soon as we have looked at it. Once a fix is released, the report is published with it, and
you are named unless you prefer not to be.

## Supported versions

Only the latest release gets security fixes. Updating Mira through Home Assistant brings you to it.

## What matters most

Mira holds a long-lived Home Assistant token of an administrator: she saves tasks and timers as automations, which
only an admin may do. The admin creates the token in their profile and pastes it at the setup; they can revoke it
there at any time. In a conversation Mira switches only what is exposed to Assist. A program with the integration's
token can read and call anything in Home Assistant through her interface (`/v1/ha/…`); a partner token can register a
companion service, which gets Mira's Home Assistant address and token in the answer and may run prompts on her models
(`/v1/complete`); and skills save blueprints, automations and scripts there. Findings in these areas are especially welcome:

- the token: how it is stored, sent, renewed and withdrawn, and what it is used for besides Assist;
- the HTTP API between the integration and the service, and Mira's pages;
- access tokens and partner tokens (stored as a hash, `X-Partner-User`, the routes they are refused), the companion
  registration (`/v1/companions/…`, which hands out the Home Assistant token) and `/v1/complete`, the `/v1/ha` routes,
  and the skill source's subscription key;
- the Starlark tools of skills (`tools.star`): the sandbox they run in (a bound on steps, run time and memory, a
  bounded answer, no `load`, no files, no network but the skill's service) and the connection's token, which goes
  only to the addresses under the skill's `api`, never to a path that climbs up (`..`, encoded dots or slashes);
- anything that lets a voice, a chat message, a web page or a skill make Mira act beyond what the user allowed.
