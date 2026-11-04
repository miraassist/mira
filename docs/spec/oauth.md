# Connections (OAuth) – spec

Status 2026-11-04: steps 1–5 built, not released; run on the test HA, the sign-in at X included (see Steps). Scenarios: [oauth.feature](oauth.feature).

## Goal

Many services Mira could use (X, calendars, music services, task lists) give access only through an OAuth login:
the user signs in at the service and allows an app; the app gets tokens that run out and are renewed. Mira should
not need code of its own per service. A **skill** describes the service's login in its metadata; Mira then offers
**"Connect to …"** for it, and anyone can write such a skill. Whoever uses the skill registers an app at the service
and enters its client id and secret.

The concept it hangs on: a **skill brings its MCP server** ([skill-mcp.md](skill-mcp.md)). The connection gives that
server a token that renews itself instead of a fixed one. New are the `oauth` section, the login through Home
Assistant, the stored tokens, the Connections page and a tool for a service's own web API.

## Decided

1. **Home Assistant does the login, Mira keeps the tokens** (H2, 2026-11-04). The redirect a service needs is
   Home Assistant's fixed one, `https://my.home-assistant.io/redirect/oauth`; only a flow of the integration can
   receive it (below, The login). After the first exchange the tokens go to the Mira service once; from then on Mira
   renews them itself. The alternative, the tokens staying in Home Assistant with the service asking the integration
   for one before every call, was rejected: a round trip per call and a config entry per connection.
2. **The tokens are kept in Mira's database**, encrypted like the secrets file (2026-11-04, 1A). They change with
   every renewal (X also replaces the refresh token each time), so they are state, not a setting: mira.yaml and the
   secrets file stay as they are.
3. **Client id and secret come with the `oauth` section** (2026-11-04, 2A): the skill does not declare them under
   `settings`. They are stored like every skill setting, the secret in the secrets file.
4. **One connection per skill**, for the whole house (2026-11-04, 3A). Per person is left out.
5. **The token is used for the skill's MCP server and for its web API** (2026-11-04, 4B), and **only for the
   addresses the skill names**: the code checks every request before the token is added.
6. **A page of its own, "Connections"**, with the state of each connection (connected, not connected, expired),
   connect, disconnect and the redirect address to enter at the service (2026-11-04, 5A). It sits in the group
   "Setup" beside rooms, persons and the notifier (2026-11-04, 9A).
7. **The service's addresses and parameters are written by the skill's author**; where they differ per
   installation, they take placeholders from the skill's settings (2026-11-04, 6A).
8. **Client id and secret are entered on the Connections page** only; the skill's page points there (2026-11-04,
   7A).
9. **The web API tool is used as the skill's instructions say** (2026-11-04): the methods the `api` section names
   are the limit; Mira adds no question of her own for writing. Whether a post needs a yes is for the skill to say.
10. **Writing after third-party text counts as passing on** (2026-11-04, 10A): in a turn that has read third-party
    text (`web_content`), a method other than `GET` through the API tool asks first (`AskAfterWeb`); otherwise it
    runs as the skill says.
11. **Connections need Home Assistant 2026.11 or newer** (2026-11-04, A): its OAuth helper sends the secret as HTTP
    Basic (`token_auth_method`) only from 2026.11 on, and X wants it so (C7). On an older Home Assistant the
    Connections page says so; the rest of Mira keeps working from 2026.10. The token request is not built by Mira.

## In the skill

```yaml
# skills/x/SKILL.md
metadata:
  mira:
    oauth:
      name: X                                     # "Connect to X"
      authorize_url: https://x.com/i/oauth2/authorize
      token_url: https://api.x.com/2/oauth2/token
      scopes: [tweet.read, users.read, offline.access]
      pkce: true                                  # default false
      token_auth: basic                           # basic or post (default): how the secret goes to token_url
      authorize_params: {}                        # extra parameters of the login, e.g. Google: access_type: offline
      api:                                        # where the token may go besides the MCP server (4B)
        - url: https://api.x.com/2/
          methods: [GET]
    mcp:
      url: https://api.x.com/mcp
      token: oauth                                # the connection's token, renewed by Mira
      web_content: true
      tools: [search_posts_all, get_users_timeline, get_users_mentions]
```

- **The service's addresses and parameters** are written by the skill's author; they are the same for everyone who
  uses the skill. Where they differ per installation (a tenant, a self-hosted server), they take placeholders from
  the skill's settings, as in the `mcp` section (`token_url: "{{ server }}/oauth/token"`) (decision 7).
- **`client_id` and `client_secret`** are the skill's settings without being declared: in mira.yaml
  `skills: { x: { client_id: …, client_secret: ${X_CLIENT_SECRET} } }`, the secret in the secrets file when entered
  on the page. They are entered on the Connections page, not among the skill's settings (decision 8).
- **`token: oauth`** in the `mcp` section: the connection's current access token goes into each request to the
  server. Without a connection the server is not connected, and the skill says what is missing (below, Problems).
- **`api`**: the addresses the skill's web API tool may call with the token, each a prefix of an `https` address,
  and the methods allowed there. Without `api` there is no such tool.
- **A mistake in the `oauth` section** (no `authorize_url` or `token_url`, an address that is not `https`, an
  unknown `token_auth`) is a problem of the skill: it does not start, as for a wrong `mcp` section.

## The login

What the user does: on the Connections page, **Connect** at "X". A window of the service opens; the user signs in
and allows the app; the window closes, and the page shows "connected".

What happens, step by step:

1. **The page** asks the integration to start a connection for the skill (a view of the integration,
   `/api/mira/oauth/<skill>/start`, as for the page's settings today, through HA's login; only for an HA admin). It
   opens the new window right on the click and gives it the address once the integration answers, so that the
   browser does not block it.
2. **The integration** fetches the skill's login from the Mira service: addresses, scopes, PKCE, extra parameters,
   client id and secret (`GET /v1/oauth/<skill>`, with the bearer token). The secret is held in memory for the
   flow only.
3. **The integration** starts a config flow of its own for the connection (Mira's config flow with the source
   `connect`; Home Assistant's frontend lists flows only of its discovery sources and `reauth`, C2, so the flow shows
   up nowhere). It builds the address of the login with
   Home Assistant's OAuth helper (`LocalOAuth2ImplementationWithPkce` or `LocalOAuth2Implementation`): the redirect
   address, the state signed by Home Assistant that names the flow, and with PKCE the challenge. It returns this
   address to the page, which opens it in a new window.
4. **The service** sends the browser to `my.home-assistant.io/redirect/oauth`, which forwards it to the user's
   Home Assistant (`/auth/external/callback`). Home Assistant checks the state and continues the flow with the
   code; the window closes itself. A flow at that point waits for the next step to be asked for, which otherwise
   a dialog of the frontend does (checked in pytest, 2026-11-04); the flow asks for it itself.
5. **The flow** exchanges the code for the tokens at `token_url` with the helper (the secret in the body or as HTTP
   Basic, `token_auth`), gives them to the Mira service
   (`PUT /v1/oauth/<skill>/token`) and ends without creating a config entry.
6. **The Mira service** stores the tokens (decision 2), connects the skill's MCP server with them and marks the
   connection as connected.
7. **The page** shows the new state when its window has the focus again.

**Disconnect** deletes the tokens in Mira and disconnects the skill's server. Revoking the app at the service is
left to the user (a link to the service's page is not part of step 1).

**Outside Home Assistant** (Mira's page opened on its own address) Connect is not offered: the redirect needs Home
Assistant. The page says to open it in Home Assistant. Inside it the page sends its own address
(`HA-Frontend-Base`), from which Home Assistant without "My Home Assistant" builds the redirect.

**Client id and secret** saved on the page go into the skill's entry of mira.yaml (`client_id`, `client_secret` as
`${MIRA_SKILL_<SKILL>_CLIENT_SECRET}`) and the secrets file, and hold at once, without a restart.

## Renewing and using the token

- **Renewing:** Mira renews the access token when it is used and has run out (with a margin of a minute), with the
  refresh token at `token_url`. A new refresh token from the service replaces the stored one at once. Only Mira
  renews: Home Assistant keeps nothing after the login.
- **A 401 from the service** with a token that should still be valid: Mira renews once and repeats the request
  once.
- **The token goes only where the skill says** (decision 5): the skill's MCP server (`mcp.url`), the `api`
  prefixes and `token_url`. A request to another address, also after a redirect, gets no token; redirects to
  another host are not followed with the token.
- **The web API tools:** `<skill>__api_get` (an address) for the `GET` prefixes, read only, and `<skill>__api_write`
  (method, address, body) for the other methods the `api` section names; answer: the service's status and body
  (at most 2 MB, shortened like every tool's answer). They are there only with an `api` section and only while the
  skill is connected, offered like the skill's MCP tools (from opening the skill, to its agents and schedules).
  What they return is third-party text. A method or address the `api` section does not name is refused; the rest
  is used as the skill's instructions say (decision 9). Two tools, so that background work (`read_only`) gets
  reading only and the rule after third-party text holds per tool.
- **After third-party text** in the same turn, `<skill>__api_write` asks first, like keeping or passing on
  (decision 10).

**Who:** connecting, disconnecting and entering client id and secret is for HA admins, as the other settings of
skills.

## Problems

- **Not connected:** the skill is there; its MCP server and API tool are missing; it is no problem under Repairs
  (the server is not tried, so it is not "cannot be reached" either). When the skill is opened, Mira
  says that the connection is missing and where it is made ("Verbinde X auf der Seite Verbindungen").
- **Expired** (the service refuses the refresh token, e.g. because the app was revoked): the state becomes
  "expired", the tokens are deleted, and Home Assistant shows it under Repairs with the way to the Connections
  page. Mira says the same when the skill is used.
- **Client id or secret missing:** Connect is not offered; the page says what to enter.
- **The service cannot be reached** while renewing: the call fails with that reason; the tokens stay.
- **Home Assistant older than 2026.11:** the Connections page says that Connect needs Home Assistant 2026.11 or
  newer (decision 11).
- **An older integration** without the connection view: the page says that the integration needs an update. **An
  older Mira service** without `/v1/oauth`: the integration does not offer the view. Both are optional fields of
  the interface, in both directions ([Versions](versions.md)).

## Safety

- **The tokens and the secret reach no model**, no prompt and no log; the page only shows whether they are set.
- **Stored encrypted** with `MIRA_SECRET_KEY`, as the secrets file; without a key, as plainly as the secrets file.
  The table survives the reset of an evaluation run, as Mira's settings in the database do.
- **The secret leaves Mira only to the integration**, over the bearer-protected interface, for the exchange in
  step 5, and is not stored there.
- **Only the addresses the skill names** get the token (decision 5). The skill comes from the operator, who knows
  what it does; Mira does not judge the addresses.
- **What the MCP server and the API tool return** is third-party text (`web_content`), with its rules.
- **Writing after third-party text** through the API tool asks first (`AskAfterWeb`, decision 10).

## Costs

- **Connecting** (once per connection): one request from the integration to the Mira service for the login, one
  token request from Home Assistant to the service's `token_url`, one request with the tokens to the Mira service,
  one write to Mira's database, one connection to the MCP server. The user's sign-in at the service is not timed;
  the flow waits for the redirect, not a turn.
- **Using:** no extra call while the token is valid. When it has run out, one request to `token_url` and one write
  to the database before the call; the turn waits for it (with X every two hours: `expires_in` 7200, C4).
- **Nothing grows with the number of items**; one database row per connection.
- **The page:** reading the state is one request to the Mira service, and inside Home Assistant one to the
  integration (whether Connect works there), when the page opens or gets the focus again.

## Checks on the test HA before building

The login waits for signals that are checked first:

| # | Transition | Signal | How it arrives | Checked on the test HA |
|---|---|---|---|---|
| C1 | Page → flow started | the address of the login | answer of the flow's first step (external step) | 2026-11-04: a flow started over HA's API returns the login address with the redirect `https://my.home-assistant.io/redirect/oauth`, the signed state, PKCE S256 and the scopes |
| C2 | Flow started without a dialog in HA's frontend, not shown as a discovered integration | — | the integrations page stays unchanged | 2026-11-04, from HA's frontend code (2026.10): it shows flows of the discovery sources (`zeroconf`, `dhcp`, …) and `reauth`; a flow with source `user` is not among them |
| C3 | Sign-in done → flow continues | the redirect with code and state | `my.home-assistant.io` → `/auth/external/callback`; the flow's step is called | 2026-11-04: the browser with the test HA set at my.home-assistant.io lands on its `/auth/external/callback`, the flow continues |
| C4 | Code → tokens | the token answer | the helper's exchange, with PKCE and `token_auth: basic` for X | 2026-11-04 on Home Assistant 2026.11.0 with the helper's own exchange (`client_secret_basic`): access and refresh token, `expires_in` 7200 s. On 2026.10 a first try was refused with 400, its reason not logged (cause unknown) |
| C5 | Tokens → Mira | `PUT /v1/oauth/<skill>/token` answered | answer of the Mira service | 2026-11-04: after the sign-in at X the log shows `oauth: connected` and X's MCP server connected with its 6 tools |
| C6 | X's hosted MCP server takes the user token | `initialize` and a `get_users_timeline` call answered | the MCP answer | 2026-11-04: `initialize`, `get_users_me` and `get_users_timeline` answered (with the app-only bearer token the last two are refused with 403) |
| C7 | Renewing at X | new access token, new refresh token | the token answer; the old refresh token refused afterwards | 2026-11-04: new access and refresh token, 7200 s; the old refresh token is refused (400); the secret in the body instead of HTTP Basic is refused (401 "Missing valid authorization header") |

**Found in the checks:** Home Assistant 2026.10's helper sends the secret only in the body (`client_secret_post`);
X refuses that with 401 (C7). Home Assistant 2026.11 lets an implementation choose HTTP Basic
(`token_auth_method = "client_secret_basic"`), hence decision 11.

Interrupting: the user closes the window or refuses at the service (the redirect then carries `error`): the flow
ends, nothing is stored, the page stays "not connected". A flow left open ends with Home Assistant's own cleanup of
unfinished flows; nothing of Mira waits for it.

For the automated tests a **fake OAuth service** (`internal/testsupport/fakeoauth`: login, token with PKCE and
HTTP Basic or the body, renewing with a new refresh token each time, letting tokens run out, revoking, a web API
that wants a current token) is used by the Go tests; the integration's tests fake the service in pytest. The test
HA cannot reach a fake over HTTPS it trusts, so there the service is X itself; the sign-in at X is done by the
maintainer ([Fake devices](fake-devices.md)).

**Run on the test HA (2026-11-04, Home Assistant 2026.11.0, the test Mira):** the skill `x` switched on, client id and
secret saved on the page's endpoint (no restart asked for); the integration's start gives X's login address with
`my.home-assistant.io`, PKCE and the scopes; tokens handed to the Mira service connect X's MCP server; X refused an
older token set (401, then the refresh token refused: the connection became expired), Home Assistant showed the
repair `skill_x`, and a voice turn with the Show's device id said that the connection with X must be made again.
After the maintainer's sign-in at X the Mira service got the tokens and connected X's MCP server (C5); a voice turn
with the Show's device id ("Was gibt es Neues von NASA auf X?") read posts through `x__search_posts_all` and answered.
Left for the maintainer: Connect and Disconnect by hand on the page (CO-12, CO-13).

## Steps

1. **Checks** C1–C7 on the test HA, the fake service with them. Built.
2. **The `oauth` section, the stored tokens, renewing, `token: oauth` for the MCP server** in the Mira service, with
   `GET /v1/oauth/<skill>` and `PUT /v1/oauth/<skill>/token`. Scenarios CO-01 to CO-08. Built.
3. **The login in the integration and the Connections page.** Scenarios CO-09 to CO-14 (pytest: CO-09, CO-10, CO-11,
   CO-14; CO-12 and CO-13 are the page's, run by hand). Built.
4. **The web API tools.** Scenarios CO-15 to CO-19. Built.
5. **The skill `x`** (reading: search, posts of an account, own timeline and mentions). Costs per call: one request
   to X's MCP server, which X bills per post read (about 0.005 USD a post, measured 2026-11-04); the skill asks for
   10 posts in a search (X's least) and 5 elsewhere, and for nothing twice in a conversation. Built.

## What is not part of it

- **One connection per person** (3B).
- **Revoking at the service** on disconnect.
- **A connection for something other than a skill** (an MCP server under `mcp_servers`).
- **OAuth 1.0a** (older X apps): only OAuth 2.0.
