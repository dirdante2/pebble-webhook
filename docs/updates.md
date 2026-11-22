# Updates

## Channels

| Channel | What you get | Versions |
|---|---|---|
| `stable` (default) | long-term releases | `X.Y.Z` |
| `beta` | new features that are not mature yet | `X.Y.Z-beta.N` |
| `nightly` | every change as soon as it is pushed, untested | `X.Y.Z-nightly.YYYYMMDDHHMM` |

Only releases of the active channel are offered. Switch the channel with the dropdown on *Status → Updates* (applies immediately) or under *Status → Konfiguration*.

## In the web app

The service checks the GitHub releases of the update source in the background every 4 hours. A new version of the active channel shows a dot on the menu button, an arrow next to *Updates* in the menu and a card on the status page; it opens *Updates*, which also shows the installed version with its change notes and

- lists every newer version of your channel with its change notes and installs it with one click,
- offers *Ältere Version installieren* to roll back to any older release (down to 1.14.0, the first version that keeps its settings in the database).

How it works: the service writes `update-request` to its data directory; the systemd path unit `pebble-webhook-update.path` starts `install.sh --apply-request` as root. The installer stops the service, backs up the running state to `BACKUP_DIR` (see Backups below), downloads the release (or takes it from the local cache), verifies `SHA256SUMS`, replaces the binary and starts the service. If the service does not answer `/healthz` within 30 s, program and database go back to the state before. Progress and errors are shown on the page; details in `journalctl -u pebble-webhook-update`.

## On the command line

```bash
sudo bash /usr/local/lib/pebble-webhook/install.sh                         # latest stable
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.15.0        # any release, also to roll back
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.16.0-beta.1
```

## Backups

*Updates → Backups* lists all backups, newest first. Each is one archive `pebble-<version>-<channel>-<time>-<kind>[-audio].tar.gz` in `BACKUP_DIR` (start value in `/etc/pebble-webhook/env`, default `/srv/pebble-backup`; after changing it run `install.sh` again) with

- `meta.json` – version, build, channel, time, kind, recordings yes/no,
- `pebble.db` – the database with all settings,
- `env` – the start values,
- `audio/` – the recordings, if chosen.

Kinds: *before update* (by the installer before every update or version switch, kept 30 days), *scheduled* (daily, weekly on Mondays or monthly on the 1st at the chosen time) and *manual* (*Back up now*); scheduled and manual ones are kept `BACKUP_RETENTION_DAYS` (default 30). The program itself is not backed up.

*Restore* backs up the current state, brings back database and recordings and installs the version named in the backup from its channel (cache or GitHub). Backups of own builds (`lokal`) restore the data only and keep the installed program. Recordings in the audio folder that the restored database does not know show up on the status page as *recordings without entry* and can be imported again (new entry, received = file time, evaluated by the AI again). Recordings are kept in `diktat/` and `planung/` under the audio folder, so an import knows the kind: notes first, then planning rounds – one round each without questions, assigned to a planning by the AI. By hand: `tar -xzf <backup> -C <dir>`, stop the service, copy `pebble.db` to `/var/lib/pebble-webhook/`, start the service.

## Releases (maintainers)

Releases are built by GitHub Actions (`.github/workflows/release.yml`) for linux amd64 and arm64, with `install.sh` and `SHA256SUMS`:

- **nightly**: every push to `main` → pre-release `vX.Y.Z-nightly.<time>` (X.Y.Z from the `VERSION` file, notes = the CHANGELOG.md section of that version, else the commit messages since the last nightly). The newest 10 are kept.
- **beta**: push a tag `vX.Y.Z-beta.N` → pre-release, notes from `## [X.Y.Z-beta.N]` in RELEASES.md (required).
- **stable**: push a tag `vX.Y.Z` → release, notes from `## [X.Y.Z]` in RELEASES.md (required).

Version numbers: **X** = breaking for database or app (new database needed or hard to roll back, e.g. migrations that change columns), **Y** = new feature, **Z** = other UI changes and small fixes that can be installed or rolled back freely. Roll back only within the same X.

Every version needs a section `## [X.Y.Z] - YYYY-MM-DD` in [CHANGELOG.md](../CHANGELOG.md) with short bullet points (New / Changed / Fixed); `deploy/deploy.sh` refuses to deploy without it.

Beta and stable releases get a summarised section in [RELEASES.md](../RELEASES.md): only what lasts since the previous beta/stable – intermediate steps and fixes of things new in the same span are left out. The update page shows these summaries on the beta and stable channels and every single version on nightly ("Frühere Änderungen"). Bump `VERSION` with every change so nightlies sort above the last release.
