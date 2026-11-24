# API tools in Starlark – spec

Status 2026-11-21: decisions 1 to 15 made (the maintainer); steps 1 to 4 built, step 5 built with [YouTube research](youtube.md). The bound on JSON's depth (SL-20) added 2026-11-21. Decision 8 changed and decision 16 added 2026-11-23, built with [Share prices](boerse.md), step 1. Decisions 17 to 19 added 2026-11-24; 17 and 19 built (step 7), 18 not yet.
Scenarios: [api-tools.feature](api-tools.feature).

## Goal

A skill whose service has a web API should give the model **fixed tools** for it, with parameters and a description,
as an MCP server does, without a program of its own and without code in Mira per service. Today such a skill has
only the web API tool (`<skill>__api_get`, [Connections](oauth.md)): the model builds the whole address from the
skill's prose. Trying YouTube showed what that costs (2026-11-20, [YouTube research](youtube.md), check Y3): a
dropped bracket in `fields` (`HTTP 400`), a search without `publishedAfter` (old videos), a weak model that did not
call at all. Nothing in the address is checked but its prefix.

The skill brings its tools as **Starlark functions** in `tools.star`. Mira makes a tool of each, with the
description and the parameters from the function's docstring and signature; the function calls the service through
a small module `api` that only reaches the skill's addresses, with the connection's token.

**Skills and code.** Some skills need no code. Others bring scripts, blueprints or automations for Home Assistant
([Skill files](skill-files.md)), a program ([Programs for skills](skill-programs.md)), or now Starlark. The guide
says so in these words (2026-11-20, the maintainer).

## The concept it hangs on

- **Starlark in Mira**: the hooks ([Hooks](hooks.md), `internal/hooks`) run Starlark files in a sandbox: no file
  system, no network, a limit of 100,000 steps; `print()` goes to the log with the hook's name; an error carries its
  backtrace with file and line; a file that does not load is a problem with that message. The same interpreter
  (`go.starlark.net`) and the same habits serve here.
- **Skill scripts** ([Skill files](skill-files.md), decision 27): one general piece of Mira's code makes what a skill
  declares into the tools `<skill>__<id>`, offered as a skill's MCP tools are (after `skill_open`, to the skill's
  schedules and agents). No code per skill.
- **The web API tool** ([Connections](oauth.md), `internal/oauth/api.go`): the prefixes and methods a skill allows,
  the connection's token renewed by Mira, one renewal and one repeat on a 401, the answer as third-party text, at
  most 2 MB, 20 s. Its rules become the `api` module's; the tool `api_get` itself goes (decision 7).

## Example

```yaml
# skills/youtube/SKILL.md
metadata:
  mira:
    oauth:
      # … the login as today …
      api:
        - url: https://www.googleapis.com/youtube/v3/
          methods: [GET]
```

```python
# skills/youtube/tools.star
def suche(thema, tage = 7, anzahl = 5):
    """Sucht Videos zu einem Thema, die meistgesehenen zuerst. Kostet 100 Einheiten des Tageskontingents.

    Args:
      thema (str): Das Thema in wenigen Wörtern.
      tage (int): Wie viele Tage zurück, 1 bis 30.
      anzahl (int): Wie viele Videos, 1 bis 10.
    """
    if tage < 1 or tage > 30:
        fail("tage ist 1 bis 30, nicht %d" % tage)
    r = api.get("https://www.googleapis.com/youtube/v3/search", params = {
        "part": "snippet", "type": "video", "q": thema, "order": "viewCount",
        "publishedAfter": time.now() - tage * 24 * time.hour,
        "maxResults": anzahl, "relevanceLanguage": "de",
        "fields": "items(id/videoId,snippet(title,channelTitle,publishedAt))",
    })
    if not r.ok:
        fail("YouTube antwortet %d: %s" % (r.status, r.text[:300]))
    return [_video(i) for i in r.json["items"]]

def _video(i):
    s = i["snippet"]
    return {"titel": s["title"], "kanal": s["channelTitle"], "datum": s["publishedAt"][:10],
            "adresse": "https://www.youtube.com/watch?v=" + i["id"]["videoId"]}
```

The model sees `youtube__suche(thema, tage, anzahl)` with the description and the three parameters; `_video` is a
helper and no tool.

## Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Declarations or code | **Starlark** (A), the language the hooks already use. Not chosen: a declaration of one request per tool in the skill's YAML (B), which grows into a half programming language of its own; both (C) |
| 2 | Where the tools are | `tools.star` in the skill's folder (4A), one file per skill |
| 3 | Which functions are tools | Every top-level function whose name does not begin with `_` (2A); `_name` is a helper |
| 4 | How a tool is described | The function's docstring in Google's style (1A): the text before `Args:` is the tool's description; under `Args:` one line per parameter, `name (type): text`. Type and description stay with the code |
| 5 | Parameters | From the signature: without a default a parameter is required, with one it is optional and the default goes into the schema. Types `str`, `int`, `float`, `bool`, `list` (3A); allowed values and limits are code (`fail()` with a message the model reads), and the docstring's text names them |
| 6 | Checking the arguments | Against the schema built from the docstring, before the function runs; a wrong argument is the tool's error naming the parameter and the value |
| 7 | `api_get` | Gone for every skill: a skill with an `api` section brings `tools.star`. The shipped skill `strava` is rewritten in the same step (nobody uses it yet) |
| 8 | Without a connection | Only for a skill with a connection for now (13A of the first round): `api` exists only with an `oauth` section. **Changed 2026-11-23 (the maintainer, 2A, for the skill `boerse`):** a skill without a login writes its `api` section under `metadata.mira` itself; its tools send no token, and the same rules hold (only below its prefixes, the methods allowed there, the limits). A skill with an `oauth` section keeps its `api` there; one in both places is a problem of the skill |
| 16 | How Mira names herself (2026-11-23, the maintainer, 2A) | Every request of a Starlark tool carries `User-Agent: Mira/<version>`. Seen 2026-11-23: Yahoo Finance answers Go's default name (`Go-http-client/1.1`) with 429 and Mira's with the quote |
| 17 | A tool that only computes (2026-11-24, the maintainer, 1A) | A `tools.star` needs no `api` section: without one, `api` refuses every address, and its tools' answers are no third-party text (seen 2026-11-24: marked as such, they locked the plan's writing tool after a computation). A computation is code, never the model's |
| 18 | Reading Home Assistant (2026-11-24, the maintainer, B) | The module `ha`: `ha.action(name, data)` calls an action of Home Assistant with its response (`return_response`; Home Assistant refuses an action without one) and gives the response; `ha.entries(domain)` gives the ids of a domain's config entries (an action like `nordpool.get_prices_for_date` needs one). Only actions the skill names under `tools.actions` in SKILL.md |
| 19 | Reading Mira's tools (2026-11-24, the maintainer, A) | The module `mira`: `mira.tool(name, args)` calls one of Mira's tools by its full name and gives its text: a tool of an MCP server (also another skill's) or of the service. Only tools that count as read-only and return no third-party text, and only those the skill names under `tools.reads` in SKILL.md; any other fails. So the model carries no data from one tool to another |
| 9 | Errors must be seen | A tool that is wrong must be recognisable, in the log and as a repair in Home Assistant; for a tool that is right but gives an unexpected result, what went over the wire must be visible (below, Seeing what went over the wire). An approach whose errors cannot be seen is not usable |
| 10 | Costs of a call | One HTTPS request per `api` call, at most 20 s each; the turn waits for the tool; a renewal of the token about once an hour |
| 11 | Seeing more than the log | **"Ausprobieren"** on the skill's entry in "Fähigkeiten", for admins (11A): a form per tool from its schema, as the page builds for a program's setup tools; it runs the tool and shows its answer, every request (method, address, status, time, the start of the answer) and every `print` |
| 12 | The term | "Starlark tool" / „Starlark-Werkzeug“ (12A), in the glossary; "skill script" stays Home Assistant's script of a skill |
| 13 | Writing | `api.post`, `api.put`, `api.patch`, `api.delete` with a JSON body, for the methods the `api` section allows (13B); a tool that writes asks first after third-party text, as `api_write` does today ([Connections](oauth.md), decision 10). How Mira knows that a tool writes: decision 15 |
| 14 | A time limit for the whole tool | At most 60 s per tool call (14B), beside the step limit and 20 s per request; beyond it the tool's error "took too long" and one log line |
| 15 | How Mira knows before a call that a tool writes | The skill lists its writing tools in `SKILL.md`, `writes: [name, …]` beside `confirm` (as for an MCP server) (15A). Only they may call a method other than `GET`; such a call from another tool fails before anything is sent, with the tool's error and one log line. A writing tool asks first after third-party text (`AskAfterWeb`), and always when it is under `confirm`. A name under `writes` or `confirm` that is no tool is a problem of the skill. Not chosen: every tool of a skill whose `api` section allows writing (B), which would make each search ask for a yes |

## What the file may do

- **Predeclared**: `api` (below), `json` (`encode`, `decode`), `time` (`now()`, durations, `parse_time`, as in
  `go.starlark.net/lib/time`), `math` (`round`, `floor`, … as in `go.starlark.net/lib/math`; Starlark has no
  `round` of its own), `fail`, `print`, and `settings`, the skill's settings that are not secret, as a
  dictionary of strings. Nothing else: no file system, no other network, no `load()`.
- **`api.get(url, params = {})`**: a `GET` to `url` with the query from `params`. Mira encodes every value (a time
  as RFC 3339 in UTC, a list as repeated parameters) and leaves out a parameter whose value is `None`. The address
  must be below one of the skill's `api` prefixes that allows `GET`; otherwise the call fails before anything is
  sent. Mira adds the connection's token, renews it on a 401 once and repeats once, follows no redirect to another
  host with it. The answer: `status`, `ok` (2xx), `text` (at most 2 MB), `json` (decoded, or `None`). A status of
  400 or more is no error by itself: the function decides, as with Python's `requests`.
- **Writing**: `api.post(url, body = …)`, `api.put`, `api.patch`, `api.delete`, for the methods the `api` section
  allows at that prefix; the body is encoded as JSON (decisions 13 and 15).
- **Limits**: 100,000 steps per call as for a hook, every `api` call at most 20 s, the whole tool at most 60 s
  (decision 14).
- **Memory and answer** (2026-11-20, the maintainer, after a look at the sandbox): Starlark counts steps, not memory,
  and one step may allocate much (`"x" * 1000000000`). During a call Mira looks every 20 ms at what it allocated since the
  call began (Go's `/gc/heap/allocs:bytes`, freed memory not subtracted) and stops the call beyond 256 MB: a rough
  brake, not a hard limit (what the rest of Mira allocates meanwhile counts too, and one allocation is not stopped
  before it happens). A tool's answer of more than 1 MB is refused. Both
  are the tool's error and a log line (`memory limit reached`, `answer too large`).
- **JSON that nests too deep** (2026-11-21, the maintainer, after a review): Starlark's decoder recurses once per
  level without a bound; two megabytes of `[` overflow the stack, which ends the whole Mira service, not only the call
  (one step for the step counter, no memory for the brake). So a text that nests arrays and objects deeper than
  1,000 levels (brackets in strings not counted) is not decoded: an answer's `json` is `None` and its `text` stays,
  with the log line `the answer nests too deep to decode`; `json.decode` fails with `the JSON nests deeper than 1000
  levels`, the tool's error, or a problem of the skill when it runs while `tools.star` loads. Costs: one pass over
  the text before it is decoded.
- **Addresses that climb** (`..`, `.`, encoded `%2e`, a hidden slash `%2f`, `%5c`) are refused before anything is
  sent, for every use of the connection's token ([Connections](oauth.md), Renewing and using the token).
- **The tool's answer** is the function's return value, encoded as JSON (a string as it is). It counts as
  third-party text, as the web API tool's answer does today.

## Loading

When the skill loads (Mira's start, the skill switched on, a skill source loaded), Mira runs `tools.star` once (its
top level, no `api` calls allowed there) and reads its functions. A **problem of the skill**, which then does not
start, in the log and as a repair in Home Assistant, naming the file, the line and what is wrong:

- the file does not parse or its top level fails (the backtrace);
- a tool without a docstring, or without an `Args:` line for one of its parameters, or with one for a parameter it
  does not have;
- a type that is not one of the five;
- `*args` or `**kwargs` in a tool;
- a tool's name that another tool of the skill has (its MCP server, its scripts);
- an `api` section without `tools.star`, or `tools.star` without an `api` section.

## Calling

The model calls `<skill>__<function>` with its arguments. Mira checks them against the schema (decision 6), runs
the function in a new thread with the limits, and returns its answer. What fails, and who learns of it:

| What fails | The model gets | The log gets |
|---|---|---|
| An argument does not fit the schema | the tool's error with the parameter and the value | one line: skill, tool, the parameter |
| `fail("…")` in the function | the tool's error with the message | one line with the message and the line of `fail` |
| An error in the function (a missing key, a wrong type) | the tool's error: "the tool failed", the error's first line | the whole backtrace |
| The step limit | the tool's error: the tool took too long | one line with the tool and the limit |
| An address outside the prefixes | the tool's error with the address | one line: skill, tool, the address |
| A timeout or a lost connection in `api.get` | the tool's error with the method and the address (Starlark has no `try`: the tool ends there) | one line per failed request (below) |
| The connection is missing or expired | the tool is not offered, as today; the skill's prose says to connect | as today ([Connections](oauth.md)) |

## Which tools write

```yaml
# SKILL.md
metadata:
  mira:
    tools:
      writes: [posten]        # only these may call api.post, api.put, api.patch, api.delete
      confirm: [posten]       # these always ask for a yes (default/skill_confirm.star)
```

A name there that is no tool of `tools.star`, or a `tools` section without `tools.star`, is a problem of the skill.

## Seeing what went over the wire

Every `api` request leaves one log line: skill, tool, method, the address with its query (without the token), the
status, the time in ms and the size of the answer. `print()` in a tool goes to the log with the skill and the tool.
On Mira's page, **"Ausprobieren"** (decision 11) runs a tool with the arguments entered and shows its answer, every
request with method, address, status, time and the start of the answer, and every `print`, in the order they
happened; an error shows with its backtrace. It runs as the admin's own call: a writing tool asks first there too.

## Costs

- Per tool call: as many HTTPS requests as the function makes; the tool's description names its cost when the
  service counts it (YouTube's quota). The turn waits for the whole function.
- Loading: running each `tools.star` once at the start; nothing grows with the number of items.

## Steps

1. **Loading `tools.star`** (`internal/startools`, `internal/skills/starlark.go`): the tools from the docstrings and
   signatures, the `tools` section, the problems. Scenarios SL-01 to SL-06. Built.
2. **Calling**: the schema check, the `api` module with the rules of the connections (`oauth.Manager.Request`), the
   limits, the log lines; the tools offered with their skill (`skills.StarlarkTools`). `api_get` and `api_write` are
   gone; the scenarios CO-15 to CO-19 of [Connections](oauth.md) are tested through the `api` module. Scenarios SL-07
   to SL-15; the bound on JSON's depth, SL-20 (2026-11-21). Built.
3. **"Ausprobieren"** on the page: the skill's entry lists its Starlark tools with a form each
   (`POST /v1/ui/skills/{skill}/tools/{tool}/try`); a writing tool is asked for in the page's dialog first. Scenarios
   SL-16 and SL-17 (the dialog and the form itself by hand). Built; the endpoint run on the test HA with the four
   tools of `youtube` (the requests with address, status, time and size, and an argument out of range as `fail`). The
   form on the page is left for the maintainer.
4. **`strava`** rewritten with `tools.star` ([Strava](strava.md), step 4). Scenario SL-18 and SV-06 to SV-08. Built;
   run on the test HA (2026-11-20) with the connected account: the five questions of the skill each through its tool
   (last tour, the month, the series, the bike without a guess about the chain, the best efforts).
5. **`youtube`** ([YouTube research](youtube.md)) built on it.
6. **A web API without a login and Mira's name** (decisions 8 and 16): built with [Share prices](boerse.md), step 1
   (BO-01, BO-02); `skills.Skill.API`, `oauth.Manager.Request` for both ways.
7. **Tools that only compute, and reading Mira's tools** (decisions 17 and 19): SL-21, SL-22. The module `ha`
   (decision 18) is not built yet.
