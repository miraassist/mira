# The agent as a Home Assistant app

Status 2026-10-26: built, not yet tried on Home Assistant OS (see Testing). Home Assistant calls apps "apps" now (the official repository is
`home-assistant/apps`); this document uses its words: app, app store. See also [Versions](versionen.md) (why the agent and the
integration drift apart) and [Setup](einrichtung.md).

## Goal

On Home Assistant OS and Supervised installs, the agent comes as an app: installed from Home Assistant's app
store, updated there, with the same release as the integration. Whoever updates the integration in HACS sees the
app's update next to it; the two stay together without anyone editing an image tag.

Not for Home Assistant as a container (no Supervisor, no apps): there the agent stays a container of its own,
with the repair from [Versions](versionen.md) when the two drift apart.

## What an app is made of

- **A repository** Home Assistant is given once (Settings → Apps → App store → Repositories): a
  `repository.yaml` (name, maintainer, address) and a folder per app.
- **The app's `config.yaml`:** name, `version`, `slug` (`mira`), description, the architectures (`amd64`,
  `aarch64`, the ones the image is built for), the image, the port (8787), what it may reach, and which folders it
  gets.
- **An image:** the one there is, `ghcr.io/miraassist/mira`, built for both architectures by the release workflow.
  Supervisor pulls the image of the app's version.
- **Documentation and pictures:** `DOCS.md` (shown in the app's page), `icon.png`, `logo.png`, a `CHANGELOG.md`
  for the update dialog.

## How Mira runs in it

- **Folders:** the database in the app's own `/data` (kept across updates, in Home Assistant's backups);
  `mira.yaml` in the app's config folder (`addon_config`), so it can be read and edited like any other config
  file. The agent reads both paths from its environment already (`MIRA_CONFIG`, `MIRA_DATA`, set by the
  Dockerfile); the app sets them to its folders in `config.yaml`. The image stays the same for container and
  app.
- **One image, run as root:** Supervisor gives an app folders owned by root and starts the container as the
  image's user; an image running as `nonroot` could not write to `/data`. So the image runs as root (distroless
  `static-debian12`, not `:nonroot`). Whoever runs Mira as a container of their own starts it with
  `--user nonroot` (Compose: `user: nonroot`); the image's data folder `/var/lib/mira` belongs to that user, so a
  named volume works as before. README and guide say so. No second image.
- **Options in the app's page:** as few as possible: the setup goes through the integration as today (model,
  pairing). [Fewer settings, one place.] One option, **Reset data** (`reset_data`, since 0.11.0): the app has no
  terminal for `mira reset`, so the option does it at the next start, once until turned off and on again
  ([Setup](einrichtung.md), Reset). The agent reads it from Supervisor's `/data/options.json`. The pairing code for
  another Home Assistant is in the app's log.
- **Home Assistant from inside:** the agent keeps its own non-admin user and token from the integration's setup,
  as today. Not the app's Supervisor token: that one has admin rights, and Mira works without them on purpose.
- **Finding it:** for now the address is typed once: **Add integration** → **Mira**, `http://localhost:8787`
  (the app's port is published on the host). Later the app could announce itself through Supervisor's discovery
  (service `mira`), so the integration offers "Mira found" (see Not for now).
- **Not in it:** local models (LM Studio), the speaker-id service, Music Assistant: services of their own, as
  today.

## One release channel

- **In this repository** (decided 2026-10-26): a `repository.yaml` at its root, next to HACS's `hacs.json`, and the
  app's folder `app/` (`config.yaml`, `DOCS.md`, `CHANGELOG.md`, icon, logo). A few files are not worth a
  repository of their own.
- **The version is set by the release workflow, after the image:** Home Assistant offers an app update as soon as
  the `version` in `config.yaml` changes on the default branch, at the push, not at the release. Raised in the
  release commit, it would be offered for the minutes until the image is built, and an install in that time would
  fail. So the release commit does not touch the app; once the images of the tag are on ghcr.io, the workflow
  sets `version` in `app/config.yaml` to the tag and commits that to `main` (the workflow needs write access to the
  repository's contents for it). Only then does Home Assistant see the update.
- The other three places of the one version (`cmd/mira/main.go`, `const.py`, `manifest.json`) stay as they are: the
  release commit raises them, the workflow refuses a tag that does not match.
- The integration from HACS and the app from the store are still two updates, but both are offered at the same
  time, in Home Assistant, with one click each. The repair from [Versions](versionen.md) reminds of the second.

## Testing

The test house is Home Assistant as a container: no Supervisor, no apps. An app needs a Home Assistant OS
install to try (a VM). Tried on 2026-10-26 by hand, as a container: the image as root with fresh root-owned
volumes on `/data` and `/config` and the app's environment starts, answers `/v1/health` and creates `mira.db` in
`/data`; with `--user nonroot` and a named volume on `/var/lib/mira` the same.

## Built

- `repository.yaml` at the repository's root.
- `app/`: `config.yaml` (version set by the release workflow, not by hand), `DOCS.md`, `CHANGELOG.md`,
  `icon.png`, `logo.png`.
- `Dockerfile`: root instead of `:nonroot`, a comment on `--user nonroot`.
- Release workflow: the job "App version" after the images, which commits the tag's version to `main`.
- README and guide: the app as the way on Home Assistant OS, `--user nonroot` in the container examples.

## Not for now (2026-10-26)

- **Discovery:** the address is typed once, as for a container.
- **Options in the app's page:** none.
- **A Home Assistant OS VM for testing:** not set up yet.
