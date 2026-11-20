# YouTube research – spec

Status 2026-11-20: steps 1 to 3 built and run on the test HA (Y5, the connection after seven days, is open). Scenarios:
[youtube.feature](youtube.feature).

## Goal

"Was ist gerade angesagt auf YouTube?", "Was gibt's Neues zu Home Assistant auf YouTube?", "Was hat @heiseonline
hochgeladen?": Mira looks on YouTube and says in a few sentences what she found. And once a day she puts together
what is new on YouTube about the topics of the household, as a list with links for one person, or two spoken
sentences for the house.

Mira knows a video by what YouTube's API says about it: title, description, channel, date, views, length. What is
said in a video is not part of it (decision 3).

## The concept it hangs on

A **skill with a connection** ([Connections](oauth.md)) and **API tools in Starlark** ([API tools in
Starlark](api-tools.md)): the skill's `oauth` section signs in at Google, its `api` section allows `GET` below
`https://www.googleapis.com/youtube/v3/`, and its `tools.star` gives the model fixed tools (`youtube__charts`,
`youtube__suche`, `youtube__kanal`, `youtube__details`) that build the requests, as the skill `x` uses the fixed
tools of X's MCP server. The
daily list is a **schedule of the skill** for one person ([A schedule for one person](schedule-receiver.md)), as
the own skill `x-recherche` does it for X. Nothing in the Mira service is built for YouTube; the API tools in Starlark are built for every skill
with a connection, YouTube is their first user.

Why not something else:
- **Home Assistant's YouTube integration** gives sensors per channel (latest upload, subscribers), no search and no
  charts; it does not answer the questions above. Mira does not rebuild it either: a question about the latest
  upload of a channel goes to the API like any other.
- **An MCP server of others** (looked at 2026-11-20): `yutu` (Go, single binaries) signs in only with its own login
  and asks for write access to the YouTube account; `kirbah/mcp-youtube` and `ZubeidHendricks/youtube-mcp-server`
  need Node, which the image and the HA app do not have. A program of our own is not needed: the connection and
  the API tools in Starlark do it (decision 1).
- **An API key** instead of a connection: it also needs a Google Cloud project, and Mira has no place for a key in a
  web API call; the connection exists.

## Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | How Mira reaches YouTube | Mira's connection (OAuth) (1A); the tools in the skill's `tools.star` (changed 2026-11-20 from the web API tool `youtube__api_get`, after check Y3: [API tools in Starlark](api-tools.md)). Not chosen: `yutu` as the skill's program (B), a program of our own with an API key (C) |
| 2 | Whose Google client | Each household makes its own OAuth client in Google Cloud and enters it on the page "Verbindungen", as for X (2A). Not chosen: one client of Mira, verified by Google (B) |
| 3 | How deep Mira looks into a video | Title, description, channel, date and numbers (3A). No transcripts: YouTube's API gives them only to a video's owner; other ways are unofficial |
| 4 | What the daily list is about | Topics, a setting of the skill (4A). Not chosen: channels as a setting (B), the account's subscriptions (C) |
| 5 | A shipped skill or an own one | A shipped skill `youtube` with the questions and the daily list (2A of the first round) |

## In the skill

```yaml
# skills/youtube/SKILL.md (since 2026-11-20 a job template, docs/spec/scheduled-jobs.md)
metadata:
  mira:
    oauth:
      name: YouTube
      authorize_url: https://accounts.google.com/o/oauth2/v2/auth
      token_url: https://oauth2.googleapis.com/token
      scopes: [https://www.googleapis.com/auth/youtube.readonly]
      pkce: true
      authorize_params: { access_type: offline, prompt: consent }   # Google gives a refresh token only so
      api:
        - url: https://www.googleapis.com/youtube/v3/
          methods: [GET]
    jobs:
      liste:
        description: Neue Videos auf YouTube zu Themen
        parameters:
          themen:     { description: …, required: true }
          empfaenger: { description: … Leer heißt das ganze Haus. }
        to: "{{empfaenger}}"
        window: 24h
        task: >-
          Stell die Liste neuer YouTube-Videos seit {{since}} zusammen, … Die Themen: {{themen}}
        first:
          - { name: morgens, cron: "0 7 * * *" }      # not made without themen (scheduled-jobs.md, decision 9)
```

- **Only reading**: the scope `youtube.readonly` and only `GET`; there is no `youtube__api_write`.
- **Topics and recipient** are the parameters of each scheduled job ([Scheduled jobs](scheduled-jobs.md), decision 3): a list in the morning and one in the evening may have other topics. Without topics no job is made. The search of a job takes `seit` (`{{since}}`, the job's last run), so a second run a day brings nothing twice.
- **The user decides** who gets a list, about what and when: each list is a scheduled job made on the skill, with
  its own topics, recipient (empty is the house) and time ([Scheduled jobs](scheduled-jobs.md)).

## What the model does, step by step

Each question has a tool of the skill; the model fills in the topic, the handle or the region, the tool builds the
requests. Every request asks only for the fields it needs (`fields=`), and the tool returns only what the answer
needs, so it stays short.

| Question | Tool: requests | Quota units |
|---|---|---|
| "Was ist gerade angesagt?" | `youtube__charts`: `videos?part=snippet,statistics&chart=mostPopular&regionCode=DE&maxResults=5` | 1 |
| "Was gibt's Neues zu …?" | `youtube__suche`: `search?part=snippet&type=video&q=…&order=viewCount&publishedAfter=<7 days ago>&relevanceLanguage=de&maxResults=5` | 100 |
| "Was hat @kanal hochgeladen?" | `youtube__kanal`: `channels?part=contentDetails&forHandle=@kanal` (its uploads playlist), then `playlistItems?part=snippet&playlistId=…&maxResults=5` | 1 + 1 |
| "Wie lang ist das / wie oft gesehen?" | `youtube__details`: `videos?part=statistics,contentDetails&id=…` | 1 |
| The daily list, per topic | `youtube__suche`: `search?…&q=<topic>&order=viewCount&publishedAfter=<24 h ago>&maxResults=10`, then `videos?part=statistics&id=<all ids>` once | 100 per topic + 1 |

- **What YouTube calls "trending"** is, since July 2025, only its charts for music, films and gaming
  (`chart=mostPopular`); the skill says so when asked for trends in general, and searches the topic instead when a
  topic is named.
- **The answer**: two or three videos in one spoken sentence each (who, what, how long ago), then the offer to say
  more; no addresses read aloud. The daily list for a person: `details` one video per line, channel, a short sentence
  in German, the address `https://www.youtube.com/watch?v=<id>`; `spoken` one sentence.
- **What YouTube returns is third-party text** (a tool's answer is `web_content`): the skill tells the model not to
  follow instructions in titles or descriptions.

## Costs

- **Per call**: one HTTPS request to `www.googleapis.com` from the Mira service, with the connection's token; the
  voice turn waits for it (at most 20 s, the web API tool's limit). Renewing the token adds one request to
  `oauth2.googleapis.com` about once an hour.
- **Google's quota**: 10,000 units a day per Google Cloud project, free. A search costs 100, every other call 1.
  The daily list with three topics costs about 300 units; a question with a topic 100. It grows with the number of
  topics, which the skill limits to the first five.
- **No money**: the YouTube Data API costs nothing within the quota.

## Errors

| What fails | Who learns of it | What happens |
|---|---|---|
| Not connected, or the connection expired | the user, in a sentence; the page and a repair (as for every connection, [Connections](oauth.md), Problems) | The web API tool is not offered; the skill says that YouTube is connected on Mira's page "Verbindungen" |
| Google refuses the token (401) | the log of the connection (`oauth: …`) | Mira renews once and repeats once (existing); refused again: the connection is expired, as above |
| Quota used up (403 `quotaExceeded`) | the user: "YouTube lässt für heute keine Suchen mehr zu" | No retry; the answer of the tool carries the status and Google's reason, which the skill tells the model to say |
| Another 4xx (a wrong address, a handle that does not exist) | the user, in a sentence | The model says that it found nothing for it; no retry |
| 5xx, timeout, lost connection | the user, in a sentence; the turn's log has the tool's error | No retry in the voice turn (the user can ask again). The schedule's run fails with the error and shows as failed on the Schedule page |
| Nothing found | the user, in a sentence; the schedule: "nothing to report" | `importance` 0, no message |
| The person in `empfaenger` is unknown or has no channel | the Schedule page and the log (`no person "<to>" with a channel of their own`) | As in [A schedule for one person](schedule-receiver.md); nothing goes to the house instead |

## Checks on the test HA before building

The skill waits for the signals of the existing connection; what is new is Google as the service:

| # | Transition | Signal | How it arrives | Checked on the test HA |
|---|---|---|---|---|
| Y1 | Page → Google's login | the login address with `my.home-assistant.io`, PKCE, the scope, `access_type=offline`, `prompt=consent` | the flow's first step | 2026-11-20: all of it in Google's login address. A client in "Testing" refuses an account that is not a test user (`access_denied`, "The developer hasn't given you access to this app") |
| Y2 | Sign-in at Google → tokens | access and refresh token | the helper's exchange (secret in the body, `token_auth: post`) | 2026-11-20: the log shows `oauth: connected` for youtube, with the maintainer's account as a test user |
| Y3 | A call of the web API tool | `HTTP 200` with the fields asked for | the tool's answer in a turn with the fake Show's device id | 2026-11-20: `search`, `videos?chart=mostPopular`, `channels?forHandle=` and `playlistItems` answer `HTTP 200` with public data (any channel, not only the account's) in 60 to 340 ms. Found: the model dropped a closing bracket of `fields` once (`HTTP 400`, "Invalid FieldMask"), so the skill gives each address whole; a handle that does not exist answers `HTTP 200` with `totalResults` 0; `order=viewCount` without `publishedAfter` finds old videos |
| Y4 | Renewing at Google | a new access token; Google sends no new refresh token, the stored one stays | the log (`oauth: token renewed`), then the calls after it | 2026-11-20: renewed at 14:43, an hour after the sign-in; the calls of the tools after it answered `HTTP 200` |
| Y5 | The connection after seven days | still connected | the Connections page | open: Google ends the sign-in of a client in "Testing" after seven days; whether "In production" without Google's review keeps it is to be seen |
| Y6 | Quota used up | `HTTP 403` with `quotaExceeded` | not provoked (it would block the project for a day); the shape from Google's documentation | the tool's sentence tested in Go (YT-05) |

Y1, Y2 and Y5 need a Google OAuth client of the maintainer and the sign-in in a browser.

## Steps

1. **Checks** Y1 to Y6 on the test HA, with the maintainer's Google client. Done but Y5.
2. **The skill `youtube`** (`skills/youtube/SKILL.md` and `tools.star`) on [API tools in Starlark](api-tools.md): the
   settings `themen` and `empfaenger`, the connection, the tools `charts`, `suche`, `kanal`, `details`, the
   instructions and the schedule `liste` (7:00). Scenarios YT-01 to YT-06. Built. On the way: a schedule's `to` with an
   empty setting was "-" instead of empty (the house); fixed in the skills' loading (PS-07).
3. **End to end on the test HA** (2026-11-20): through Mira's conversation API with the fake Show's device id
   "Was ist gerade angesagt auf YouTube?" (`youtube__charts`, the charts named as music, films and gaming), "Was gibt's
   Neues zu Home Assistant Voice auf YouTube?" (one `youtube__suche`), the follow-up "erzähl mehr zum zweiten"
   (`youtube__details`), "Nein danke" (ends), "Was hat NASA auf YouTube zuletzt hochgeladen?" (`youtube__kanal`, no
   search). The schedule run by hand with three topics and the person Anna: three searches with English keywords, one
   video kept, a private message to Anna. Scenarios YT-07 to YT-09 (as evals, not yet written). Left for the
   maintainer: the answers heard at a satellite, and the list in a real channel (Telegram) of the live house.

## What is not part of it

- Transcripts and what is said in a video (decision 3).
- Subscriptions, likes, playlists of the account (decision 4; reading them would be the same connection later).
- Playing a video: Mira has no player for YouTube; Music Assistant plays YouTube Music, not videos.
