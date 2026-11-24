# Updates

## Channels

| Channel | What you get | Versions |
|---|---|---|
| `stable` (default) | long-term releases | `X.Y.Z-stable` (before 1.33.9: `X.Y.Z`) |
| `beta` | new features that are not mature yet | `X.Y.Z-beta` |
| `nightly` | every pushed change that gets a nightly build, untested | `X.Y.Z-nightly` |

Every number exists once per channel; which release is newer is decided by its publication time, not by the number.

The active channel also offers the more stable ones: stable shows only stable, beta shows beta and stable, nightly shows all three – so a stable that is newer than the last beta is offered on beta too. A release from another channel than the installed one counts as newer only if it also has a higher number. Switch the channel with the dropdown on *Status → Updates* (applies immediately) or under *Status → Konfiguration*.

## In the web app

The service checks the GitHub releases of the update source in the background every 4 hours. A new version of the active channel shows a dot on the menu button, an arrow next to *Updates* in the menu and a card on the status page; it opens *Updates*, which also shows the installed version with its change notes and

- lists every newer version of your channel with its change notes and installs it with one click,
- offers *Ältere Version installieren* to roll back to any older release (down to 1.14.0, the first version that keeps its settings in the database).

How it works: the service writes `update-request` to its data directory; the systemd path unit `pebble-webhook-update.path` starts `install.sh --apply-request` as root. The installer stops the service, backs up the running state to `BACKUP_DIR` (see Backups below), downloads the release (or takes it from the local cache), verifies `SHA256SUMS`, replaces the binary and starts the service. If the service does not answer `/healthz` within 30 s, program and database go back to the state before. Progress and errors are shown on the page; details in `journalctl -u pebble-webhook-update`.

## On the command line

```bash
sudo bash /usr/local/lib/pebble-webhook/install.sh                         # latest stable
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.15.0        # any release, also to roll back
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.34.0-beta
sudo bash /usr/local/lib/pebble-webhook/install.sh --channel nightly       # newest of a channel (stable, beta, nightly)
```

`--channel` takes the moving release of that name on GitHub (`stable`, `beta`, `nightly`), which always holds the newest build of the channel. The usual way to change the channel is the web app; a new installation always starts with stable.

## Docker

In the container there is no updater: the update page shows new versions with the `docker pull` command; install, roll back and automatic update are off. The channel chosen there only selects which versions are shown. Restoring a backup works in the app (the container restarts and brings back the data, the version stays that of the image). A new version backs up the data on its first start before migrating them.

| Tag | Points to |
|---|---|
| `X.Y.Z` | exactly that version (fixed; the same code in every channel) |
| `stable`, `latest` | newest stable |
| `beta` | newest beta or stable, whichever is higher |
| `nightly` | newest of all channels |

Each release builds one image; the moving tags are only re-pointed (same version: stable > beta > nightly). The channel of a build is in the label `org.opencontainers.image.version`.

## Backups

*Backups* (menu, below Updates) lists all backups, newest first. Each is one archive `pebble-<version>-<channel>-<time>-<kind>[-audio].tar.gz` in `BACKUP_DIR` (start value in `/etc/pebble-webhook/env`, default `/srv/pebble-backup`; after changing it run `install.sh` again) with

- `meta.json` – version, build, channel, time, kind, recordings yes/no, users with key id,
- `system.db` – users, sign-in (only hashes and public keys) and system settings,
- `users/<id>/daten.db` – each user's data, encrypted with their key,
- `env` – the start values,
- `audio/<id>/` – the encrypted recordings, if chosen.

The keys are never in a backup. Restored on another server, each user opens their data with their recovery code after signing in. Backups up to 2.x (`pebble.db`, unencrypted) can still be restored; the data moves to the new layout on start.

Kinds: *before update* (by the installer before every update or version switch, kept 30 days), *scheduled* (daily, weekly on Mondays or monthly on the 1st at the chosen time) and *manual* (*Back up now*); scheduled and manual ones are kept `BACKUP_RETENTION_DAYS` (default 30). The program itself is not backed up.

*Restore* backs up the current state, brings back database and recordings and installs the version named in the backup from its channel (cache or GitHub). Backups of own builds (`lokal`) restore the data only and keep the installed program. Recordings in the audio folder that the restored database does not know show up on the status page as *recordings without entry* and can be imported again (new entry, received = file time, evaluated by the AI again). Recordings are kept in `<user>/diktat/` and `<user>/planung/` under the audio folder, so an import knows the kind: notes first, then planning rounds – one round each without questions, assigned to a planning by the AI. By hand: `tar -xzf <backup> -C <dir>`, stop the service, copy `system.db` and `users/` to `/var/lib/pebble-webhook/` (remove the old ones first), start the service.

## Releases (maintainers)

Releases are built by GitHub Actions (`.github/workflows/release.yml`) for linux amd64 and arm64, with `install.sh` and `SHA256SUMS`:

A push to `main` builds nothing. After the push, `deploy/release.sh [nightly|beta|stable]` (default nightly) pushes one tag for the pushed `VERSION` and GitHub builds that channel only – a beta or stable build needs no nightly of the same version, since the update page offers stable on beta and all channels on nightly. The tag must match `VERSION`; an existing number per channel fails.

- **nightly**: tag `vX.Y.Z-nightly` → pre-release, notes = the CHANGELOG.md section of that version, else the commit messages since the last nightly. The newest 10 are kept.
- **beta**: push a tag `vX.Y.Z-beta` → pre-release, notes from `## [X.Y.Z-beta]` in RELEASES.md (required).
- **stable**: push a tag `vX.Y.Z-stable` → release, notes from `## [X.Y.Z]` in RELEASES.md (required).

The same run builds the Docker image from these binaries and pushes it to Docker Hub with the tags [above](#docker). Needs the repository variable `DOCKERHUB_IMAGE` (e.g. `owner/pebble-webhook`) and the secrets `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`; without the variable the Docker steps are skipped. The Docker Hub page text comes from `docker/README.md` (workflow `dockerhub-readme.yml`, on every change on `main` or by hand; the token needs Read, Write, Delete).

Version numbers: **X** = breaking for database or app (new database needed or hard to roll back, e.g. migrations that change columns), **Y** = new feature, **Z** = other UI changes and small fixes that can be installed or rolled back freely. Roll back only within the same X.

X.Y.Z changes once per pushed block of work and is set by hand in `VERSION` before the push; every `deploy/deploy.sh` only counts the local build number A (`X.Y.Z.A`, kept in `lokal/BUILD`, never in Git; it starts at 1 for every new X.Y.Z). Every version needs a section `## [X.Y.Z] - YYYY-MM-DD` in [CHANGELOG.md](../CHANGELOG.md) with short bullet points (New / Changed / Fixed); `deploy/deploy.sh` refuses to deploy without it.

Beta and stable releases get a summarised section in [RELEASES.md](../RELEASES.md): only what lasts since the previous beta/stable – intermediate steps and fixes of things new in the same span are left out. The update page shows these summaries on the beta and stable channels and every single version on nightly ("Frühere Änderungen"). Bump `VERSION` with every change so nightlies sort above the last release.
