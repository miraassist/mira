# Companion services – Spec

Status 2026-11-23: decisions 1 to 4 by the maintainer (in the companion's own repository, 2026-11-23: its settings on
Mira's page, no second panel in Home Assistant, a path convention, the companion registers itself); 5 to 12 taken while
writing, to be confirmed. Not built. Scenarios: [companions.feature](companions.feature) (`BS-…`).

## Goal

A **companion service** is a program the owner installs beside Mira and connects to her with an access token
([Access tokens](access-tokens.md)): it brings skills of its own (a skill directory both share, an MCP server) and has
settings and a state of its own. The owner should see and change them on Mira's page, inside Home Assistant's one
panel of Mira, not in a second panel or a page of its own somewhere else. And when the companion changes the skills it
brings, Mira should take them without the owner restarting her.

Mira gets only what is general: a companion registers with a name, an address and a token; Mira shows a tab for it
that holds the companion's own page, and passes that page's calls on. Mira knows nothing of the companion's settings.

## The concept it hangs on

- **Mira's page** (`internal/ui`): its files under `/ui/…` need no token, its data comes from `/v1/ui/…`; tabs fixed in
  `index.html` (`ALL_TABS`).
- **The integration's panel** (`custom_components/mira/panel.py`): page files at `/api/mira/panel/…` without sign-in
  (only what `PAGE_FILES` lets through), `/api/mira/proxy/v1/ui/…` to Mira with the user and whether they are an
  admin; for non-admins an allow-list.
- **Access tokens** ([Access tokens](access-tokens.md)): a partner token may call only `whoami` and `status`
  (decision 23). A companion uses such a token.
- **Skills** are loaded at Mira's start ([Skill source](skill-source.md), decision 6; [Skill files](skill-files.md)).

Departures, each with its reason:

- **A partner token may do two more things** (decision 5): register its companion and ask for the skills to be read
  again. Neither reaches Home Assistant or anything of the owner's: the reason for decision 23 of the access tokens
  holds.
- **Mira passes on what she does not understand** (decision 7): only for admins, only to the registered address.

## Decided (2026-11-23, by the maintainer, in the companion's repository)

| # | Question | Decision |
|---|---|---|
| 1 | Where a companion's settings are kept | With the companion. Not in `mira.yaml`, not in Mira's database |
| 2 | Where they are shown | On Mira's page, inside Home Assistant's one panel of Mira; never a second panel |
| 3 | How the two are joined | A convention in the path: a companion's page and data are under `companion/<name>/`, on Mira's routes and through the integration's two routes |
| 4 | Who joins them | The companion registers itself with Mira (name, address, a token it makes); only while it is registered its tab and its paths are there |

## Decided while writing (2026-11-23, to be confirmed)

| # | Question | Decision |
|---|---|---|
| 5 | What a partner token may do besides (widens [Access tokens](access-tokens.md), decision 23) | `PUT` and `DELETE /v1/companions/{name}` (its own registration) and `POST /v1/skills/reload`. Every other call stays refused with 403 |
| 6 | Registering | `PUT /v1/companions/{name}`, body `{"title", "url", "token"}`. A name is `[a-z][a-z0-9-]{0,31}`; the title at most 32 characters; the URL `http` or `https` with a host; the token not empty. Kept in Mira's database (table `companions`), the newest registration of a name wins, with the token that registered it; another token may not overwrite a name (409). `DELETE` removes it. 204 on success, 400 per field otherwise |
| 7 | Passing on | `GET /ui/companion/{name}/{path...}` from `<url>/ui/{path}` (the companion's page files; no token, like Mira's own); `/v1/ui/companion/{name}/{path...}` from `<url>/api/{path}` for admins only (as Mira's page's other routes), with `Authorization: Bearer <the companion's token>` instead of the caller's. Methods GET, PUT, POST, DELETE; bodies up to 1 MiB; 10 s per call. An unknown name: 404. The companion unreachable: 502 `companion <name> cannot be reached` |
| 8 | The tab | `GET /v1/ui/companions` lists name and title. Each registered companion is a tab after Mira's own in the group "Einrichten" / "Setup", for admins only, holding an iframe of `companion/<name>/` on the page's own origin (the CSP's `default-src 'self'` allows it) |
| 9 | The integration | `PAGE_FILES` lets `companion/<name>/…` through; the proxy passes `v1/ui/companion/…` for admins only (not on the non-admins' allow-list) |
| 10 | Reading the skills again | `POST /v1/skills/reload`: 202, then Mira restarts as after saving settings on her page (the simple variant: skills are wired at the start, [Skill source](skill-source.md), decision 6). A companion calls it only when its skill folder really changed |
| 11 | Trust | A companion's page runs in the same origin as Home Assistant and Mira's page: it can use the Home Assistant token of the admin who opens it, as Mira's page can. The owner installed the companion and gave it its token |
| 12 | Its activity | A partner token's calls stay in the partner activity (access-tokens.md, decision 12), registering and reloading too |

## What it does

1. **Registering** (decision 6): a companion calls `PUT /v1/companions/{name}` with its partner token; Mira checks the
   fields, keeps the registration and logs `companion registered` with name and URL (never the token).
2. **The tab** (decision 8): Mira's page asks `GET /v1/ui/companions` at its start; each companion becomes a tab for
   admins. The tab holds an iframe of the companion's page through Mira.
3. **Passing on** (decision 7): Mira serves the companion's page files and passes its data calls on with the
   companion's token, the caller's dropped.
4. **Reloading** (decision 10): `POST /v1/skills/reload` answers 202 and restarts Mira. The answer goes out with its
   length and flushed before the restart: a chunked answer lost its end to the restart (seen on the test HA).

## When something fails

| Fails | Who learns of it |
|---|---|
| A field of a registration is wrong | 400 with the field and why; a log line with the name |
| A name taken by another token | 409; a log line with the name and both tokens' names |
| The companion cannot be reached when its page is used | 502 `companion <name> cannot be reached`; a log line with the name, the URL and the error; the tab shows it |
| The companion answers 401 (Mira's stored token is no longer its) | 502 `companion <name> refuses Mira's token`; a log line; the companion registers anew at its next start |
| A non-admin calls a companion's path | 403 (Mira's page's rule; the integration does not pass it for non-admins) |
| Reloading cannot restart | A log line `cannot restart` with the error, as for the page's restart |

## Cost

- One registration per start of a companion; one row per companion.
- Loading a companion's tab: its files and its calls, each through Home Assistant and Mira to the companion; nothing
  while it is closed.
- A reload is a restart of Mira (seconds without her).

## Steps

1. **Registering**: the table, `PUT`/`DELETE /v1/companions/{name}`, the partner token's two new calls.
2. **Passing on and the tab**: `/ui/companion/…`, `/v1/ui/companion/…`, `GET /v1/ui/companions`, the tab.
3. **The integration**: `PAGE_FILES` and the proxy.
4. **Reloading**: `POST /v1/skills/reload`.
5. **The run on the test HA** with a companion: registered, its tab in the panel, its page loading and saving, a reload.

Built: steps 1 to 4. BS-07 (a non-admin through the integration) has no coded test yet.

**The run on the test HA (2026-11-24)** with mira-pro as the companion `pro`: it registered with its partner token
(again 5 s later when Mira restarted under it); the tab Pro is in Setup in the panel; its page loads through
`/api/mira/panel/ui/companion/pro/` and reads and saves through `/api/mira/proxy/v1/ui/companion/pro/…`; a reload
restarted Mira, who then read mira-pro's skill folder and connected its MCP server; in a conversation she called its
tool. What it showed: the restart's answer lost its end (fixed, above), and calls arriving during a restart fail with a
lost connection (the companion tries again).
