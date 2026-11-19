# Installing Mira in detail

The short way is [Get started](../README.md#get-started) in the README; the guide walks through it with every click
([Deutsch](guide/de.md#einrichtung) · [English](guide/en.md#setup)). This page has the details: running the Mira
service from source or a container of your own, what it needs, and how the integration pairs with it.

## The Mira service

### From source

Running from source, or with your own `mira.yaml` for what goes beyond the setup form (more models, more MCP
servers, skills with settings):

```sh
go build -o mira ./cmd/mira
./mira run                            # no config: waits to be set up by Home Assistant
cp mira.example.yaml mira.yaml        # optional; secrets come from ${ENV}
./mira -config mira.yaml check        # probe the LLM, Home Assistant and MCP servers
./mira -config mira.yaml chat "Wie warm ist es im Wohnzimmer?"
./mira -config mira.yaml profile      # compact the remembered facts into the profile now
./mira -config mira.yaml run
```

### In a container of your own

The image `ghcr.io/miraassist/mira` (or one built with the `Dockerfile`: a static binary on distroless, about
45 MB) needs two folders:

| In the container | What it holds |
| --- | --- |
| `/etc/mira` | `mira.yaml`, skills of your own, and what skills bring along: their programs (`programs/`) and each skill's own folder (`skill-data/<skill>`, a login, say) |
| `/var/lib/mira` | Mira's data: the database (`mira.db`, `MIRA_DATA`), the secrets file |

The [compose.yaml in Get started](../README.md#get-started) maps both to folders beside it. Mount the folder
`/etc/mira`, not the single file `mira.yaml`: Mira's page saves `mira.yaml` through a temporary file in the same
folder, which does not work on a file mounted on its own. Skills of your own go into a folder inside it (say
`config/skills`), listed first in `skills.dirs` as `/etc/mira/skills`, before the built-in `skills`. Programs a skill
brings are downloaded into `programs/` and run from there, so the folder must not be on a filesystem mounted
`noexec`.

The image runs as root, as a Home Assistant app needs; the compose example keeps that, so the folders need no
`chown`. To run as `nonroot` (user 65532) instead, add `user: nonroot` and give both folders to that user
(`sudo chown -R 65532 config data`).

Building the image yourself:

```sh
docker build -t mira .
```

`network_mode: host` (`--network host`) lets the integration find the Mira service by mDNS; without it, publish the
port (`8787:8787`) and enter the URL in the integration. The database goes to `/var/lib/mira/mira.db` in the image
(`MIRA_DATA`), to `mira.db` in the working directory outside it; `store.path` changes it. The
built-in skills are in the image. MCP servers started over stdio need their own runtime (e.g. node), which the image
does not have; use MCP servers over HTTP, or a skill that brings its program (a single executable file).

### Requirements

- A language model with an OpenAI-compatible API: a cloud provider (OpenAI, Google Gemini, OpenRouter, Mistral,
  Anthropic) or a server of your own. With llama.cpp, `models.<name>.pin_slots` keeps one slot per channel warm
  (`llama-server --parallel 3`), or `warm_slots: true` keeps the prompt warm in one of two slots while a changed one
  is read into the other (`--parallel 2`, docs/spec/warm-slot.md). LM Studio ignores slot ids; it picks slots by
  common prefix, which works well because all channels share the start of the prompt.
- Home Assistant. At the setup an administrator pastes a long-lived token of theirs; pairing switches on the "Model
  Context Protocol Server" integration with the Assist API (`/api/mcp/assist`) if it is missing. With `ha.url` and
  `ha.token` in `mira.yaml` instead, set both up by hand.
- Optional: more MCP servers under `mcp_servers`, over HTTP or started by Mira (stdio). A web search is built in
  (DuckDuckGo, no key; switched in the integration's options, docs/spec/web-search.md). Radio is built in too: "play
  SWR3" finds the station in Home Assistant's Radio Browser and starts it on the room's speaker (`radio_play`,
  docs/spec/radio.md). See "More tools over MCP" in the guide ([de](guide/de.md), [en](guide/en.md)).

## The integration

1. Click **Open in the Marketplace** in the README, or Settings → Marketplace → **Add from link** →
   `https://github.com/miraassist/mira`. The first time, the Marketplace asks to connect GitHub. Before Home
   Assistant 2026.11: HACS → Custom repositories, the same address, type Integration.
2. Install **Mira**, restart Home Assistant.
3. Settings → Devices & services: the Mira service announces itself by mDNS (`_mira._tcp`, `server.announce`) and
   shows up under **Discovered**; otherwise Add integration → Mira with its URL. A new Mira service is added with one
   click (one with `server.token` in `mira.yaml` asks for that token), then the form asks for the AI model, the
   assistants and Mira's access to Home Assistant.

Pairing (docs/spec/setup.md): the form chooses and tries the model first (the Mira service answers
`/v1/setup/models` and `/v1/setup/probe` without a token only while it was never paired); nothing is changed before
**Start using Mira**. There an administrator pastes a long-lived token of theirs (the integration checks that it is
long-lived and an admin's; it creates none itself). Then the integration switches on HA's MCP server if it is missing
and pairs and sets up the Mira service in one call (`POST /v1/pair` with HA's address, the token and the model),
which keeps everything in its database, hands out its access token and starts anew. A Mira service that was never
paired needs no code. The integration makes Mira the conversation agent of the assistants ticked in the form. If the
token is revoked later, the repair "Mira has no access any more" asks for a new one.

Removing the integration gives the assistants their previous conversation agent back, hands persons and satellites
back to the web UI and lets the Mira service go (`POST /v1/unpair`: it makes a new token, forgets the HA access and
keeps the model). The administrator's token stays: it is theirs, revoked in their profile. Home Assistant keeps
Mira's old access token in its storage, so the same HA pairs again without a code (also when the Mira service was
away and did not let go); another HA needs the pairing code the Mira service writes into its log. `mira reset` (or
the app's option "Reset data") deletes the Mira service's data, keeps `mira.yaml`, and it pairs like a new one.

Mira's web UI is in Home Assistant's sidebar: the integration passes the page through Home Assistant (open, it holds
no data) and its API calls through an authenticated proxy that adds the Mira service's token and the signed-in HA
user. So it works over HA's HTTPS, with HA's login, and takes on HA's theme. There everyone, admins too, sees the
house's memory and their own (person linked to the HA user); messages, tasks and settings are for admins, enforced by
the proxy and the Mira service. The integration's options switch it off.

Mira keeps no lists of her own for what Home Assistant has (docs/spec/ha-integration.md): every HA person is a person
for Mira, every Assist satellite with an area speaks for that area. What Mira needs on top is stored on the person or
satellite itself, as entity registry options under `mira`, and set in the integration's options ("Configure":
settings for a person, for a satellite). Entities labelled "Mira: nachfragen" are only switched after a yes. Each
change is checked by the Mira service before it is saved; what it refuses shows in the form. While the integration
manages them, Mira's web UI shows those sections read-only. Removing the integration hands them back. The device also
has a quiet-mode switch, a pending-messages sensor and a button to compact the memory profile now.
