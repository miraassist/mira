# OpenAPI – Spec

Status 2026-11-16: steps 1 and 2 built (released). Decisions 1, 2, 11, 12 and 13 by the maintainer, the others
taken while writing, to be confirmed; notes from building below the decisions.

## Goal

The Mira service's HTTP interface is used by the integration, Mira's page, and services outside the house that hold
a token ([Access tokens](access-tokens.md)), such as the Partner service. They need a description of every route:
method, path, parameters, request, answer, errors. It must never be out of date.

## Decisions

By the maintainer (2026-11-16):

| # | Topic | Decision |
|---|---|---|
| 1 | Made how | Generated from the code, not written by hand |
| 2 | The way | Every route is registered through one small function that knows its method, path, summary and the Go types of its request and answer; a generator writes the OpenAPI file from that, and a test fails when the file does not match the code. The handlers themselves stay as they are (until decision 12) |
| 11 | At run time | The running service serves its description at `GET /v1/openapi.yaml` (and `GET /v1/openapi.json`), generated from the routes it registered, so it is always the running version's. Without a token: it says nothing the open repository does not; it is itself in the file |
| 12 | "A JSON object" (the open question of step 1: decided A) | Every route's request and answer has a named Go type, given in its registration; the handler fills that type instead of a map or an anonymous struct. The JSON on the wire stays as it was, key for key. A body that is free-form on purpose (an action's data, a protocol of others, settings' sections as `mira.yaml` holds them, the description itself) stays an open object, and its route's description says why |
| 13 | `additionalProperties` (decided B) | No schema says `additionalProperties: false`: the generator takes it out of every schema, so a client written against an older description takes a newer service's answer with a field added (the interface rule: new fields are optional both ways) |

Taken while writing (2026-11-16), to be confirmed:

| # | Topic | Decision |
|---|---|---|
| 3 | The file | `docs/openapi.yaml`, OpenAPI 3.1, in the repository; written by `go run ./cmd/openapi` |
| 4 | What is in it | Every route of the service under `/v1/`, grouped by tags: the integration's, the page's (`/v1/ui/…`, marked as Mira's page's own, which may change between versions), setup, Home Assistant ([Home Assistant through Mira's interface](ha-api.md)), skills. Not: `/ui` (the page's files), test-only routes (`/v1/eval/reset` only in eval mode is listed with a note) |
| 5 | Schemas | From the Go types by reflection (`json` tags, `omitempty` as optional); a field's comment as its description where the generator can read it, else none. A route whose answer is not JSON (a backup file, a page) says its media type |
| 6 | Errors | Mira's one error body (`{"error": "…"}`) as a shared schema; each route lists the statuses its spec names |
| 7 | Security | One bearer scheme; the header `X-Partner-User` with a partner token ([Access tokens](access-tokens.md)) described once |
| 8 | Version | `info.version` is the service's version (`cmd/mira`'s `version`) at generation |
| 9 | A route without registration | The test also walks the service's mux: a route registered past the function fails the test, naming it |
| 10 | A dependency | A small, maintained library for the reflection of Go types into JSON Schema may be used; no framework that changes how handlers are written |

## Notes from building (2026-11-16)

- **Decision 9, walking the mux**: Go's `ServeMux` cannot list its routes. The service's only mux is `api.Mux`, which
  keeps every route it is given; a route reaches it only through `Handle` (or `HandleIf` for a route of some setups,
  `Before` for one answered by a wrapper in front of the mux, `Hidden` for a pattern left out with its reason, `Mount`
  for a part's mux). The test reads the service's Go source (`cmd/`, `internal/`, without tests, `internal/api` and the
  fakes in `internal/testsupport`) for a `Handle`/`HandleFunc` call with a pattern string on any other mux and names
  it with its file and line.
- **Decision 4, what is in it**: also `GET /v1/whoami` (answered by the access check in front of the mux), the inbox's
  webhook and the remote agents' push (each with a token of its own, so without the bearer scheme, said in their
  description), and the description's own routes. A route only some setups have (agents, skills started from Home
  Assistant, connections, backup, eval mode) is listed with "Only …". Left out: the page's files under `/ui`, and the
  setup mode's own `GET /v1/health` and page (the service waiting to be set up is a server of its own).
- **Decision 5, schemas**: a field's Go comment is not read (no source at run time); a `jsonschema` tag would be.
  Where a handler built its answer or read its body as a map or an anonymous struct, the description said "a JSON
  object" without its keys: 56 answers and 18 bodies of the 79 routes. Decision 12 names them all (step 2).
- **Decision 12, building (2026-11-16)**: a struct that replaces a map lists its fields in the order of their keys,
  so it marshals byte for byte as the map did (Go sorts a map's keys); one that replaces an anonymous struct keeps its
  order. `omitempty` only where the key was left out, and exactly then; a key that was there even when empty (an
  empty fingerprint, an empty list) is a pointer with `omitempty`, which the schema shows as nullable although the
  service never sends `null`. The equality was checked once, for the change, against the old maps (not kept as a
  test: once the maps are gone it tests nothing); the handlers' tests and the file's test guard it from then on. The
  open objects on purpose: the body of `POST /v1/ha/services/{domain}/{service}` (each action takes its own data),
  of `POST /v1/a2a/push/{call}` (an A2A StreamResponse, the A2A protocol's), of `PUT /v1/ui/config` (a section's
  value as `mira.yaml` holds it), and the answer of `GET /v1/openapi.json` (an OpenAPI document). A test fails for
  any other body or answer that is an object without keys, naming the route.
- **Decision 13, building**: the generator walks every schema the reflection made and drops
  `additionalProperties: false`; a map's schema of its values stays.
- **Decision 6, errors**: each route lists the statuses its handler writes, `401` added for every route with the
  token, `403` where a partner token is refused.
- **Decision 8, version**: `cmd/openapi` and the test read `var version = "…"` from `cmd/mira/main.go`; every raise of
  the version is a change of the file (`go run ./cmd/openapi`).
- **Decision 11, at run time**: the running service describes the routes of its setup; it equals the file when
  every optional route is on. The description is made at each call, nothing is kept.

- **Accepted by the maintainer (2026-11-16):** a field made a pointer to keep its key present when empty shows as
  nullable in the description, although the service never sends null (2A); `TaskList.error` with `omitempty` differs
  from the old map only for an error with an empty message, which does not happen (3A).

## What it does

1. `internal/api`: `Route{Method, Path, Summary, Tag, Query, Request, Response, Status, Errors, Open, Only}`, `Mux`
   with `Handle` (registers a handler and keeps the route), and the generation (`api.YAML`, `api.JSON`; the schemas by
   `github.com/google/jsonschema-go`, already a module of the service through the MCP library).
2. Every place that registers a route (`internal/channels/haconv`, `internal/ui`, `internal/setup`) does it through
   that function; `internal/routes` puts the parts together, for the running service (`cmd/mira`) and for the
   generator alike.
3. `cmd/openapi` builds the service's routes as the service does (without connecting anything) and writes
   `docs/openapi.yaml`.
4. A test generates it in memory and compares it with the file; a difference fails with "run go run ./cmd/openapi".
5. `GET /v1/openapi.yaml` and `GET /v1/openapi.json`, without a token, generate it from the running service's routes.

What it costs: nothing at the service's run time beyond keeping the list, and one generation per call of the
description; the generation of the file runs only on demand and in the test.

## Errors

| Fails | What happens | Who learns of it |
|---|---|---|
| The file does not match the code | the test fails naming the first difference and the command to run | the developer, `scripts/check.sh`, CI |
| A route registered past the function | the test fails naming the route | the developer |
| A Go type the reflection cannot describe | the generator stops naming the route and the type | the developer |
| The description cannot be made at run time | `500` with Mira's error body naming the error; a log line with the path and the error | the caller, the log |

## Steps

1. **The function and the generator**, every route through it, the file and the test, the description at run time
   (OA-01 to OA-05; built 2026-11-16).
2. **Named types and open schemas** (decisions 12 and 13): every request and answer a named Go type, the free-form ones
   listed with their reason; no `additionalProperties: false` (OA-06, OA-07; built 2026-11-16).
