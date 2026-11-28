# Updates

## Channels

| Channel | What you get | Versions |
|---|---|---|
| `stable` (default) | long-term releases | `X.Y.Z-stable` (before 1.33.9: `X.Y.Z`) |
| `beta` | new features that are not mature yet | `X.Y.Z-beta` |
| `nightly` | every pushed change that gets a nightly build, untested | `X.Y.Z-nightly` |

- Every number exists once per channel.
- Publication time decides which release is newer, not the number.

What each channel offers:

| Active channel | Shows |
|---|---|
| stable | stable |
| beta | beta and stable |
| nightly | all three |

- So a stable newer than the last beta is offered on beta too.
- A release from another channel than the installed one counts as newer only with a higher number.
- Switch the channel with the dropdown on *Status → Updates* (applies immediately).
- Or switch it under *Status → Configuration*.

## In the web app

The service checks the GitHub releases of the update source every 4 hours, in the background.

A new version of the active channel shows:

- a dot on the menu button,
- an arrow next to *Updates* in the menu,
- a card on the status page.

It opens *Updates*. That page:

- shows the installed version with its change notes,
- lists every newer version of your channel with its change notes and installs it with one click,
- offers *Install an older version* to roll back to any older release (down to 1.14.0, the first version that keeps its settings in the database).

### How it works

1. The service writes `update-request` to its data directory.
2. The systemd path unit `pebble-webhook-update.path` starts `install.sh --apply-request` as root.
3. The installer stops the service.
4. It backs up the running state to `BACKUP_DIR` (see [Backups](#backups)).
5. It downloads the release (or takes it from the local cache).
6. It verifies `SHA256SUMS`.
7. It replaces the binary and starts the service.
8. If the service does not answer `/healthz` within 30 s, program and database go back to the state before.

Progress and errors show on the page. Details: `journalctl -u pebble-webhook-update`.

## On the command line

```bash
sudo bash /usr/local/lib/pebble-webhook/install.sh                         # latest stable
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.15.0        # any release, also to roll back
sudo bash /usr/local/lib/pebble-webhook/install.sh --version 1.34.0-beta
sudo bash /usr/local/lib/pebble-webhook/install.sh --channel nightly       # newest of a channel (stable, beta, nightly)
```

- `--channel` takes the moving release of that name on GitHub (`stable`, `beta`, `nightly`).
- That release always holds the newest build of the channel.
- The usual way to change the channel is the web app.
- A new installation always starts with stable.

## Docker

The container has no updater.

- The update page shows new versions with the `docker pull` command.
- Install, roll back and automatic update are off.
- The chosen channel only selects which versions are shown.
- Restoring a backup works in the app. The container restarts and brings back the data. The version stays that of the image.
- A new version backs up the data on its first start, before migrating them.

| Tag | Points to |
|---|---|
| `X.Y.Z` | exactly that version (fixed; the same code in every channel) |
| `stable`, `latest` | newest stable |
| `beta` | newest beta or stable, whichever is higher |
| `nightly` | newest of all channels |

- Each release builds one image.
- The moving tags are only re-pointed (same version: stable > beta > nightly).
- The channel of a build is in the label `org.opencontainers.image.version`.

## Backups

*Backups* (menu, below Updates) lists all backups, newest first.

Each backup is one archive in `BACKUP_DIR`:

```
pebble-<version>-<channel>-<time>-<kind>[-audio].tar.gz
```

- `BACKUP_DIR` start value is in `/etc/pebble-webhook/env`, default `/srv/pebble-backup`.
- After changing it, run `install.sh` again.

Archive content:

| File | Content |
|---|---|
| `meta.json` | version, build, channel, time, kind, recordings yes/no, users with key id |
| `system.db` | users, sign-in (only hashes and public keys) and system settings |
| `users/<id>/daten.db` | each user's data, encrypted with their key |
| `env` | the start values |
| `audio/<id>/` | the encrypted recordings, if chosen |

- The keys are never in a backup.
- Restored on another server, each user opens their data with their recovery code after signing in.
- Backups up to 2.x (`pebble.db`, unencrypted) can still be restored. The data moves to the new layout on start.
- The program itself is not backed up.

### Kinds

| Kind | Made | Kept |
|---|---|---|
| *before update* | by the installer before every update or version switch | 30 days |
| *scheduled* | daily, weekly on Mondays or monthly on the 1st, at the chosen time | `BACKUP_RETENTION_DAYS` (default 30) |
| *manual* | *Back up now* | `BACKUP_RETENTION_DAYS` (default 30) |

### Restore

*Restore*:

- backs up the current state,
- brings back database and recordings,
- installs the version named in the backup from its channel (cache or GitHub).

Backups of own builds (`lokal`) restore the data only. They keep the installed program.

Recordings without entry:

- Recordings in the audio folder that the restored database does not know show on the status page as *recordings without entry*.
- They can be imported again: new entry, received = file time, evaluated by the AI again.
- Recordings are kept in `<user>/diktat/` and `<user>/planung/` under the audio folder, so an import knows the kind.
- Notes come first, then planning rounds: one round each without questions, assigned to a planning by the AI.

Restore by hand:

```bash
tar -xzf <backup> -C <dir>
```

1. Stop the service.
2. Remove the old `system.db` and `users/` in `/var/lib/pebble-webhook/`.
3. Copy `system.db` and `users/` from the archive there.
4. Start the service.

## Releases (maintainers)

GitHub Actions builds releases (`.github/workflows/release.yml`) for linux amd64 and arm64, with `install.sh` and `SHA256SUMS`.

- A push to `main` builds nothing.
- After the push, `deploy/release.sh [nightly|beta|stable]` (default nightly) pushes one tag for the pushed `VERSION`.
- GitHub builds that channel only.
- A beta or stable build needs no nightly of the same version. The update page offers stable on beta and all channels on nightly.
- The tag must match `VERSION`.
- An existing number per channel fails.

| Channel | Tag | Result | Notes |
|---|---|---|---|
| **nightly** | `vX.Y.Z-nightly` | pre-release | CHANGELOG.md section of that version, else commit messages since the last nightly. Newest 10 are kept. |
| **beta** | `vX.Y.Z-beta` (push it) | pre-release | `## [X.Y.Z-beta]` in RELEASES.md (required) |
| **stable** | `vX.Y.Z-stable` (push it) | release | `## [X.Y.Z]` in RELEASES.md (required) |

### Docker image

- The same run builds the Docker image from these binaries.
- It pushes it to Docker Hub with the tags [above](#docker).
- Needs the repository variable `DOCKERHUB_IMAGE` (e.g. `owner/pebble-webhook`).
- Needs the secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
- Without the variable, the Docker steps are skipped.
- The Docker Hub page text comes from `docker/README.md`.
- Workflow `dockerhub-readme.yml` runs on every change on `main` or by hand.
- The token needs Read, Write, Delete.

### Version numbers

| Part | Meaning |
|---|---|
| **X** | breaking for database or app (new database needed or hard to roll back, e.g. migrations that change columns) |
| **Y** | new feature |
| **Z** | other UI changes and small fixes that can be installed or rolled back freely |

Roll back only within the same X.

- X.Y.Z changes once per pushed block of work.
- Set it by hand in `VERSION` before the push.
- Every `deploy/deploy.sh` only counts the local build number A (`X.Y.Z.A`).
- A is kept in `lokal/BUILD`, never in Git. It starts at 1 for every new X.Y.Z.
- Bump `VERSION` with every change so nightlies sort above the last release.

### Changelog and release notes

- Every version needs a section `## [X.Y.Z] - YYYY-MM-DD` in [CHANGELOG.md](../CHANGELOG.md).
- Use short bullet points (New / Changed / Fixed).
- `deploy/deploy.sh` refuses to deploy without it.

Beta and stable releases get a summarised section in [RELEASES.md](../RELEASES.md):

- Only what lasts since the previous beta/stable.
- Intermediate steps and fixes of things new in the same span are left out.
- The update page shows these summaries on the beta and stable channels.
- On nightly it shows every single version ("Earlier changes").
