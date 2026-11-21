# Access tokens – Spec

Status 2026-11-21: steps 1 to 4 built (released); step 5 (a partner token reads only who it is and the state,
decision 23) built 2026-11-21. Scenarios: [access-tokens.feature](access-tokens.feature).

## Goal

The Mira service's HTTP interface knows one token: the integration's (`server.token`, or the one handed out at
pairing, [Setup](setup.md)). Whoever holds it may do everything, and Mira cannot tell who called. A partner that looks
after Mira installations for their owners needs its own access, made and withdrawn by the owner, and the owner wants
to see what the partner did. The first use is the **Partner service** (a separate program of the partner, not part of
Mira's open source; repository `mira-partner`). Mira itself gets only what is general: more than one named access
token, and the partner activity on Mira's page.

## Decided (2026-11-16, by the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | How many tokens | More than one. Each has a **name** that says whose it is ("whoami") |
| 2 | Who makes them | Mira makes them on her page; the user copies one into the Partner service. Deleting it on the page withdraws it |
| 3 | What a token is | A random value; its name lives in Mira's database (deleting it is the withdrawal); a call `GET /v1/whoami` tells the holder its name |
| 4 | The first kind | The **partner token**. It authenticates the Partner service; the partner user travels in the header `X-Partner-User`. Mira trusts that header: the Partner service checked the partner user before (authorization in the house) |
| 5 | `X-Partner-User` with other tokens | Mira trusts the header only together with a partner token; with the integration's token it is ignored |
| 6 | What a partner token may do | ~~Everything the interface offers (one role, for now)~~ Replaced by decision 23 (2026-11-21) |
| 7 | The owner sees it | Mira logs every call of a partner token with the token's name and the partner user, and her page shows the owner what the partner did, when and which partner user |

## Decided (2026-11-16, by the maintainer, on the spec's proposals)

| # | Question | Decision |
|---|---|---|
| 8 | Who may make and delete a token on the page | Home Assistant admins only, as every other setting of the setup |
| 9 | The setup's routes | `/v1/pair`, `/v1/unpair`, `/v1/setup` and `/v1/prove` stay the integration's: a partner token is refused there (403). A partner who unpairs would cut the house off from Mira. Since 2026-11-21 part of decision 23 |
| 10 | As whom a partner token acts on the page | ~~As an admin without a Home Assistant user~~ Replaced by decision 23 (2026-11-21): a partner token reaches no route of the page. `X-Mira-HA-User` and `X-Mira-HA-Admin` are still dropped with a partner token |
| 11 | What is stored of a token | Only a hash (SHA-256) and its first characters for recognising it; the token itself is shown once, when it is made. A backup of the database holds no usable token |
| 12 | Which calls the partner activity keeps | Every call, reading too; kept 90 days, then deleted at the nightly cleanup |
| 13 | Where on the page | Setup → Connections, a section "Zugangs-Tokens" / "Access tokens" (the words: decision 19) |
| 14 | Expiry | None; a token holds until it is deleted |
| 15 | The integration's token in the list | Shown as "Home Assistant (Integration)", not deletable there (removing the integration releases it, [Setup](setup.md)) |
| 16 | Backup and restore (`/v1/backup`, `/v1/restore`) | Not with a partner token (403, like the setup's routes); they carry the whole database. Since 2026-11-21 part of decision 23 |
| 17 | The persons' memories (2026-11-16, 1B; the first text of decision 10 said wrongly that they are out of reach: a call without a Home Assistant user sees every memory) | A partner token sees and changes only the house's memory, like a person who is no admin; a person's own memory stays out of its reach. Since 2026-11-21 moot: a partner token reaches no route of the page (decision 23); the page's check stays |
| 18 | Managing access with a partner token (2026-11-16, 2B) | Refused (403): `/v1/ui/tokens` and below are the owner's; a partner cannot make, delete or list tokens, nor read the partner activity. Since 2026-11-21 part of decision 23 |
| 19 | The page's words (2026-11-16, by the maintainer) | The page and the guide do not speak of a partner: it is simply a new token ("Zugangs-Tokens", "Neues Token anlegen"; the partner user is "Benutzer"). Partner stays the word in the specs and the code (kind `partner`, `X-Partner-User`) |
| 20 | Showing a new token (2026-11-16, by the maintainer) | In the page's dialog, the token in a read-only field, selected, with "Kopieren und schließen" and "Schließen"; never inline on the page, where it was overlooked |
| 21 | A taken name; copying over http (2026-11-16, by the maintainer, after trying it) | A name is taken regardless of upper and lower case ("Test" and "test" are one); the page says so under the field, not only at its top. The browser's clipboard interface exists only on https and localhost; over http the page copies with the browser's older copy command, for the connections' copy button too |
| 22 | The state in the partner activity (2026-11-16, by the maintainer) | `GET /v1/status` with a partner token is kept neither as a row nor as a log line: the Partner service asks it at a fixed interval, it only reads, and it would bury the rest. Every other call stays as decision 12 says |

## Decided (2026-11-21, by the maintainer, after a review)

| # | Question | Decision |
|---|---|---|
| 23 | What a partner token may do (replaces 6 and 10) | Only `GET /v1/whoami` and `GET /v1/status`. Every other call is refused with 403 `a partner token may only ask who it is and the state`, before any handler; the refusal is kept in the partner activity like any call (decision 12), so the owner sees an attempt |

Why: since 2026-11-07 Mira works with the owner's Home Assistant token ([HACS default](hacs-default.md)). A partner
token that could use the whole interface was thereby an administrator's access to Home Assistant without a sign-in:
any action through `/v1/ha` without the checks for locks and alarms, a conversation as any person, the connections'
secrets, a skill's MCP command started on the page (review of 2026-11-21). The partner looks after a house in Home
Assistant itself, through the remote access of the Partner service (`mira-partner`, remote.md): signed in as a Home
Assistant user whose rights the owner gives and takes, each change in Home Assistant's logbook under that user. The
Partner service's tabs, which used Mira's interface, are gone with this decision (`mira-partner`, partner.md); it
keeps the state it asks at an interval and `whoami`.

## Building blocks

- **The token check** of the three handlers that take the integration's token today: Mira's page (`internal/ui`,
  `auth` and `authAnyone`), the integration's interface (`internal/channels/haconv`: conversation, status, messages,
  tasks, agents, OAuth, backup) and the setup (`internal/setup`). They stay as they are. One check before all of them
  (`internal/access`) knows the partner tokens: it answers `/v1/whoami`, refuses a partner token everywhere but
  `GET /v1/status` (decision 23), and passes that call on with the integration's token, the caller (name, kind, partner
  user) in the request's context. Not touched: the inbox's own token for webhooks ([Inbox](inbox.md)) and the push
  tokens of remote agents ([Remote agents](remote-agents.md)); they never start with a partner token's prefix.
- **Mira's page**: the tab Connections ([Connections](oauth.md)) gets the section; the dialog to ask is the page's own
  `ask()`, never a browser alert.
- **The nightly cleanup** ([Cleaning up the key-value table](kv-cleanup.md)) deletes old partner activity.

## What it does

### Making a token

1. On Mira's page, Setup → Connections → Access tokens: a name ("Elektro Meier"), **"Neues Token anlegen"**.
   `POST /v1/ui/tokens` `{name}` (the kind is `partner`).
2. The service makes 32 random bytes (`crypto/rand`), base64url, with the prefix `mira_p_` (so a token found in a log
   or a file says what it is), stores the hash, the first 10 characters, name, kind and time, and answers the token
   **once**. The page shows it in its dialog (decision 20): "Das Token „<name>“. Kopiere es jetzt: Es wird nur dieses
   eine Mal angezeigt.", the token selected in a read-only field, "Kopieren und schließen" copies it and closes,
   "Schließen" closes.
3. The name must be new among the tokens, regardless of case: a second "Elektro Meier" or an "elektro meier" is
   refused (409); the page says so under the field (decision 21).

**Costs:** one write; the tokens are held in memory (a map from hash to name and kind), renewed on every write, so a
call costs no read of the database.

**Fails:** no random bytes (practically never) or the write fails → 500, log line `could not make an access token`
with the name and the error; nothing is stored, the page says it did not work.

### A call with a token

1. The check takes `Authorization: Bearer <token>`. The integration's token as today; otherwise the hash among the
   held tokens. None fits → 401 as today (`missing or wrong bearer token`), no log line per call (as today; a log
   line per wrong call would let anyone fill the log).
2. A partner token: the caller is `{name, kind: partner, partner_user}`, the partner user from `X-Partner-User`
   (empty if missing). Only `GET /v1/whoami` and `GET /v1/status` pass; any other method or path is refused with 403
   `a partner token may only ask who it is and the state` (decision 23), and no handler is called. On `GET
   /v1/status` the headers `X-Mira-HA-User` / `X-Mira-HA-Admin` are dropped.
3. With the integration's token, `X-Partner-User` is dropped before the handler sees it.
4. Every call of a partner token writes a row of partner activity (time, token name, partner user, method, path,
   status), for every call except `GET /v1/status` (decisions 12 and 22). A log line `partner call` with the same fields.

**Costs:** one hash per call (SHA-256 of the token) and one lookup in memory; for a partner token one write per call (none for `GET /v1/status`).

**Fails:** the activity row cannot be written → the call still runs (the owner's interface does not stop for its
record), log line `could not record a partner call` with token name, partner user, path and the error.

**At the start** the service reads the stored tokens once. If that fails, Mira does not start, and the error says
that the access tokens could not be read: a service whose tokens are unknown would refuse its partners without a
word.

### `GET /v1/whoami`

Answers `{name, kind}` of the calling token; for the integration's token `{name: "integration", kind:
"integration"}`. A wrong token → 401.

### Deleting a token

On the page "Löschen", asked in the page's dialog ("Token „Elektro Meier“ löschen? Wer es nutzt, kommt danach nicht
mehr an Mira."). `DELETE /v1/ui/tokens/{id}` deletes the row and renews the tokens in memory; the next call with it is
refused. The partner activity stays (with the name) until it ages out.

**Fails:** unknown id → 404 (`no such token`), the page says it is already gone; the write fails → 500, log line
`could not delete an access token` with the name and the error, the page says it did not work and the token still
holds.

### The partner activity on the page

Below the tokens, per token, when its card is opened: the last calls, newest first (time, partner user, what as
method and path, the result's status), 50 at a time with "Mehr" for the next. `GET /v1/ui/tokens/{id}/activity`.
The integration's token is the first card, without "Löschen".

## Between the integration and the service

Nothing changes for the integration: its token works as before, and the new routes are new. An older integration
shows the page through its proxy as before; an older service has no section "Access" (the page asks the service's
version, as for Connections).

## Steps

1. **Tokens and the check** (built 2026-11-16): the table `access_tokens`, the check before the three handlers,
   `GET /v1/whoami`, `X-Partner-User` only with a partner token, the refused routes. Scenarios AT-01 to AT-06,
   AT-13, AT-15.
2. **The partner activity** (built 2026-11-16): the table `access_log`, the rows, the log line, the cleanup. Scenarios AT-07 to AT-09.
3. **The page** (built 2026-11-16): the section Access tokens (make, show once in the dialog, delete, activity), German and English; a partner sees the house's memory only. Scenarios AT-10 to
   AT-12.
4. **The guide** (built 2026-11-16): a paragraph on giving a partner access and taking it back.
5. **A partner token reads only who it is and the state** (decision 23, built 2026-11-21): the check refuses every
   other call before its handler; the guide says what such a token can do. Scenario AT-16.
