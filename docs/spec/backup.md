# Backup and restore – spec

Status 2026-11-01: steps 1–3 built, not released. Scenarios: [backup.feature](backup.feature). Builds on Home Assistant's
backups (Settings → System → Backups) and on [The interface between the integration and the service](ha-anbindung.md).

## Goal

Mira keeps more and more of the household: what she remembers, the profiles, messages, threads, the settings in
`mira.yaml` and its secrets, one's own skills. Losing it hurts. Home Assistant backs itself up on a schedule, with
encryption and to places of the user's choice; Mira's data goes with it, so a restore of Home Assistant brings Mira
back too, on every kind of installation.

## Decided (2026-11-01)

| # | Question | Decision |
|---|---|---|
| 1 | How Mira gets into the backup | Through the integration, on every installation: before Home Assistant's backup it fetches a consistent snapshot of Mira and lays it into Home Assistant's config folder; the backup takes it along |
| 2 | The HA app | Its database is left out of the app's own backup (`backup_exclude`), so it is not there twice and never as a torn copy |
| 3 | What the snapshot holds | The database, `mira.yaml`, the secrets file, and one's own skill folders (not Mira's bundled skills) |
| 4 | Restore | After Home Assistant was restored, a repair "Restore Mira's data" with the snapshot's date; a click sends it to Mira |
| 5 | The secrets | Decrypted in the snapshot: Home Assistant's backup encryption protects them, as it does Home Assistant's own secrets. A restore encrypts them again with the restoring Mira's `MIRA_SECRET_KEY`, if set; so a lost key or a new Mira loses nothing. The key itself is never in the snapshot |
| 6 | `mira.yaml` read only at a restore | Everything else is restored; `mira.yaml` is left as it is, the backup's copy is written to the data folder as `mira.yaml.restored`, and the answer says so |

Not built: backups of Mira on her own (a schedule, a place, keeping several). Home Assistant does that.

## What it does

### The snapshot (the service)

`GET /v1/backup` (the integration's token) answers with one `tar.gz`:

| File in the archive | From |
|---|---|
| `mira.db` | the database, written as one consistent copy with SQLite's `VACUUM INTO` into the data folder, then streamed and removed |
| `mira.yaml` | the configuration file as it is |
| `secrets.json` | the secrets, decrypted (decision 5) |
| `skills/<dir>/<skill>/…` | every skill folder in a folder of `skills.dirs` other than Mira's bundled one |
| `manifest.json` | Mira's version, the time, the highest migration, the file list |

Writing goes on while the copy is made: `VACUUM INTO` reads one snapshot of the database, nothing is locked for
longer than SQLite needs.

### Into Home Assistant's backup (the integration)

The integration has Home Assistant's backup platform (`backup.py`):

- **Before the backup** (`async_pre_backup`): it fetches `GET /v1/backup` and writes it to
  `.storage/mira_backup.tar.gz` in Home Assistant's config folder, which the backup takes along (on HA OS too: the
  Supervisor tells Home Assistant before it backs up the config folder).
- **After the backup** (`async_post_backup`): it deletes the file. So the file is only ever there inside a backup,
  and after a restore.
- **It never fails Home Assistant's backup.** An error there stops the whole backup, so every failure (Mira not
  reachable, an older Mira without `/v1/backup`, a full disk) is caught: the backup goes on without Mira, the log
  says why, and a repair "Mira was not in the last backup" names the reason and the next click. It goes away with
  the next backup that has Mira.

### Restore

- **Found at start:** when the integration starts and finds `.storage/mira_backup.tar.gz` (left only by a restore),
  it raises the repair "Restore Mira's data" with the snapshot's date and Mira's version in it, once it is connected
  to Mira.
- **The repair's flow** says what is replaced (what Mira remembers, messages, threads, settings, one's own skills)
  and asks for a yes. Yes sends the archive with `POST /v1/restore`; "ignore" deletes the file. Either way the file
  is gone afterwards.
- **The service** checks the archive before it changes anything: a `manifest.json`, a database that opens, a
  migration it knows (a snapshot of a newer Mira is refused with the reason: update Mira first). Then it:
  1. keeps its own pairing with the integration (the keys of the setup in `kv`): the archive's would cut the
     integration off;
  2. writes the database beside the live one (`mira.db.restore`): the store puts it in place when it opens the
     database next, at the restart (removing `-wal` and `-shm`), so an open database is never replaced;
  3. writes the secrets (encrypted with its own key, if set), `mira.yaml` and the skill folders (a skill folder of
     the snapshot replaces the one of that name; into the folder it came from if that is still one of
     `skills.dirs`, else into the first of one's own);
  4. answers, then starts anew in its own process (as "Restart Mira" on the page), so everything is read again.
- **`mira.yaml` cannot be written** (mounted read only, as in a container setup that keeps it under version
  control): it is left as it is, the backup's copy goes to the data folder as `mira.yaml.restored`, and the answer
  says so (decision 6).

### The HA app

`app/config.yaml` gets `backup_exclude: ["mira.db", "mira.db-wal", "mira.db-shm"]`: the database comes through the
integration's snapshot only. The rest of the app's folder stays in the app's backup as before.

## The interface

New fields and calls are optional both ways:

- An older service has no `/v1/backup`: the integration's hook gets a 404, the backup goes on, the repair says to
  update Mira.
- An older integration has no hook: backups are as before (with the app: its folder, now without the database; the
  app and the integration are released together).

## Costs

- **Each backup:** `VACUUM INTO` of the database (today about 1 MB; under a second), one download of the archive
  from Mira to Home Assistant (the database, a few KB of files, one's own skills), one file written and later
  deleted in Home Assistant's config folder. Grows with the database and the number of one's own skills. Home
  Assistant's backup waits for it (the pre-backup step).
- **Restore:** one upload of the archive, the check (opening the database once), writing the files, one restart of
  Mira (a few seconds without Mira).
- Nothing runs between backups.

## Steps

1. **Service: the snapshot and the restore.** `GET /v1/backup`, `POST /v1/restore` with the checks and the restart.
   Scenarios BK-01 to BK-07.
2. **Integration: into Home Assistant's backup.** `backup.py`, the repair when Mira was not in it. Scenarios BK-10
   to BK-13, tested in pytest.
3. **Integration: restore.** The file found at start, the repair's flow, the upload. Scenarios BK-20 to BK-23,
   tested in pytest.
4. **The app** leaves its database out; the guide says how backup and restore go and what the secrets key means.
