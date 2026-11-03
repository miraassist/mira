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

Mira holds a Home Assistant token for its own user, without admin rights, and can switch whatever is exposed to
Assist. Findings in these areas are especially welcome:

- the token: how it is stored, sent and withdrawn;
- the HTTP API between the integration and the service, and Mira's pages;
- anything that lets a voice, a chat message, a web page or a skill make Mira act beyond what the user allowed.
