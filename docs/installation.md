# Installation

## Requirements

- Debian 12/13 or Ubuntu 22.04+ with systemd, amd64 or arm64; 1 CPU, 512 MB RAM are enough
- An HTTPS reverse proxy with a domain (nginx, Caddy, Nginx Proxy Manager …) – passkeys and web push need HTTPS
- An OpenAI API key
- Optional: a Joplin server for the one-way export

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash -s -- --channel stable
```

Installation starts with the newest stable release. Switch to beta or nightly afterwards in the web app under *Updates → Kanal* (or install one directly with `--channel beta` / `--channel nightly`).

The installer

1. downloads the latest release for your architecture and checks it against `SHA256SUMS`,
2. creates the system user `pebble`, `/var/lib/pebble-webhook` (databases), `/srv/pebble-medien` (recordings, images), `/srv/pebble-backup` and `/etc/pebble-webhook/env`,
3. installs the binary to `/usr/local/bin/pebble-webhook`, the CLI wrapper `/usr/local/bin/pebble` and the systemd units,
4. starts the service and prints the link to the setup wizard.

Options: `--version X.Y.Z` (specific release, also beta/nightly or older ones – see [updates](updates.md)), `--source` (build from a cloned repository, needs Go), `--repo owner/name` (fork).

Running the installer again updates the program; database, settings and env file stay untouched.

## Docker

Every release is also published as a Docker image for amd64 and arm64: `dirdante/pebble-index-webapp` on Docker Hub.

Ready-made [compose.yaml](../docker/compose.yaml) – every variable and own folder is in it, commented out; remove the `#` to use one:

```bash
curl -fsSLO https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/docker/compose.yaml
docker compose up -d && docker compose logs pebble
```

```yaml
services:
  pebble:
    image: dirdante/pebble-index-webapp:stable   # stable, beta, nightly or a fixed version like 4.1.0
    container_name: pebble
    restart: unless-stopped                      # required: saving settings restarts the service
    ports:
      - "8080:8080"                              # host:container – container side must match LISTEN
    environment:
      # TZ: Europe/Berlin                        # time zone for appointments, reminders, backups (default UTC)
      PUID: "1000"                               # owner of all data on the host (id -u) – default, keeps environment valid
      PGID: "1000"                               # group of all data on the host (id -g)
      # LISTEN: 0.0.0.0:8080                     # listen address inside the container
      # DATA_DIR: /config/data                   # databases (system.db, users/<id>/daten.db)
      # KEY_DIR: /config/keys                    # encryption key per user – not in backups, keep a copy
      # MEDIA_DIR: /config/medien                # recordings and images per user
      # BACKUP_DIR: /config/backup               # backup archives
      # PEBBLE_ENV_FILE: /config/env             # optional file with further KEY=value lines
      # OPENAI_API_KEY: sk-...                   # fixed from here and locked in the app; else set in the app
      # YOUTUBE_API_KEY: AIza...                 # fixed from here and locked in the app; else set in the app
    volumes:
      - ./config:/config                         # everything in one folder (or a Docker volume: pebble-config:/config)
      # Own folders, e.g. on a NAS or separate disks – set the matching variable above as well:
      # - /srv/pebble/data:/data                 # DATA_DIR: /data
      # - /srv/pebble/keys:/keys                 # KEY_DIR: /keys
      # - /mnt/nas/pebble-media:/media           # MEDIA_DIR: /media
      # - /mnt/nas/pebble-backup:/backup         # BACKUP_DIR: /backup
```

All variables at a glance (all optional):

| Variable | Default | Holds / does |
|---|---|---|
| `DATA_DIR` | `/config/data` | databases: `system.db` and `users/<id>/daten.db` |
| `KEY_DIR` | `/config/keys` | encryption key per user (never in backups – keep it safe) |
| `MEDIA_DIR` | `/config/medien` | recordings and images per user (`<id>/audio`, `<id>/bilder`) |
| `BACKUP_DIR` | `/config/backup` | backup archives |
| `PEBBLE_ENV_FILE` | `/config/env` | optional file with further `KEY=value` lines |
| `LISTEN` | `0.0.0.0:8080` | listen address in the container (port must match `ports`) |
| `PUID` / `PGID` | `1000` / `1000` | owner and group of all data on the host |
| `TZ` | `UTC` | time zone for appointments, reminders and backups |
| `OPENAI_API_KEY` | – | wins over the web app and is locked there |
| `YOUTUBE_API_KEY` | – | wins over the web app and is locked there |

- **One folder:** without own paths everything lives in `/config` (data, keys, media, backups, env file). Own folders: mount them and set the matching variable, e.g. `/mnt/nas/pebble-media:/media` with `MEDIA_DIR: /media`.
- **User and group:** the container starts as root, sets owner and modes of `/config` and all data folders (also bind-mounted host folders) to `PUID:PGID` and then runs as that user. `1000:1000` is the first user on most Linux systems; check yours with `id`. With `docker run --user` it never runs as root; then the folders must already be writable for that user.
- **API keys** (`OPENAI_API_KEY`, `YOUTUBE_API_KEY`) set in the environment always win over the web app and are locked there. Without them, enter the keys in the setup wizard as usual. The same applies to the systemd installation (`/etc/pebble-webhook/env`).
- **Setup wizard:** the link is in the log (`docker compose logs pebble`).
- **Updates:** the app shows new versions and the `docker pull` command, but installs nothing itself. Update with `docker compose pull && docker compose up -d`. The tag decides the channel – see [updates](updates.md#docker). When a different version starts than last time, it first makes a backup *before update* of the data (without recordings, kept 30 days), then brings databases and user data up to date by itself.
- **Restore a backup:** in the web app under Backups, as usual (also *Upload backup*). The container restarts and brings the data back before it opens them; the current state is backed up first. The version stays that of the image – a backup from a newer version needs that version's image first.
- **Back to an older version:** set the image to the old version, then mark the backup made before the update and start:

  ```bash
  docker compose run --rm pebble wiederherstellen            # list backups
  docker compose run --rm pebble wiederherstellen pebble-4.1.0-stable-20261124-120000-update.tar.gz
  docker compose up -d
  ```

  This also works when the app no longer starts.

## Storage: own mounts (recommended)

Give the three data folders a volume each – in a Proxmox LXC a mount point per folder. They can then be sized, backed up and restored on their own, and a full disk in one does not stop the others. **1 GB each** is enough to start; grow them later if needed.

| Folder | Holds | Start value |
|---|---|---|
| `/var/lib/pebble-webhook/users` | one encrypted database per user | part of `DATA_DIR` |
| `/srv/pebble-medien` | recordings and images per user (`<id>/audio`, `<id>/bilder`) | `MEDIA_DIR` |
| `/srv/pebble-backup` | backup archives | `BACKUP_DIR` |

Proxmox example (container 209, storage `local-zfs`, sizes in GB; create them before installing):

```bash
pct set 209 -mp0 local-zfs:1,mp=/var/lib/pebble-webhook/users,backup=1
pct set 209 -mp1 local-zfs:1,mp=/srv/pebble-medien,backup=1
pct set 209 -mp2 local-zfs:1,mp=/srv/pebble-backup,backup=1
pct reboot 209
```

New or restored mount points usually belong to `root` – you do not need to fix that. Before every start (systemd `ExecStartPre`) and on every install, `pebble-webhook verzeichnisse` creates missing folders and sets owner `pebble:pebble` and modes (0750, keys 0700). If a folder is still not writable, the service refuses to start and names the folder in `journalctl -u pebble-webhook`.

Adding a mount to a running installation: `systemctl stop pebble-webhook`, rename the folder (e.g. `mv users users-alt`), create the mount point at the old path, copy the content back (`cp -a users-alt/. users/`), delete the old folder and start the service.

## Reverse proxy

Forward your domain to `http://<server>:8080` with WebSocket/SSE support and no buffering. Example for nginx:

```nginx
location / {
    proxy_pass http://10.0.0.5:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

Enter the proxy's IP as *Reverse-Proxy* in the settings so rate limits use the real client address.

## Setup wizard

Open the printed link (`/ersteinrichtung?code=…`) – ideally already through your HTTPS domain. Lost the link? Run `sudo pebble ersteinrichtung`. See [configuration](configuration.md).

## Files

| What | Where |
|---|---|
| Program | `/usr/local/bin/pebble-webhook`, CLI `/usr/local/bin/pebble` |
| Start values | `/etc/pebble-webhook/env` (`LISTEN`, `DATA_DIR`, `MEDIA_DIR`, `BACKUP_DIR`) |
| Database | `/var/lib/pebble-webhook/system.db` (users, passkeys, system settings) and `users/<id>/daten.db` per user (encrypted) |
| Media | `/srv/pebble-medien/<id>/audio/…` and `<id>/bilder/…` (`MEDIA_DIR`, encrypted per user) |
| Keys | `/etc/pebble-webhook/keys/<id>.key` (`KEY_DIR`) – one per user, never part of a backup |
| Service | `systemctl status pebble-webhook`, logs `journalctl -u pebble-webhook` |
| Updater | `/usr/local/lib/pebble-webhook/install.sh`, units `pebble-webhook-update.{path,service}` |
| Backups | `/srv/pebble-backup/` (`BACKUP_DIR`) – archives with database, settings, version and channel: before every update (30 days) and scheduled (see [updates](updates.md#backups)) |

Back up `/var/lib/pebble-webhook` (with `users/`), `/srv/pebble-medien` and, separately and safely, `/etc/pebble-webhook/keys`. Without its key file a user's data opens only with their recovery code (shown once after setup; a new one under *Status → Access and security*). The keys protect backups and copies of the data folder, not a server someone has root access to.

## Uninstall

```bash
sudo systemctl disable --now pebble-webhook pebble-webhook-update.path
sudo rm -f /etc/systemd/system/pebble-webhook* /usr/local/bin/pebble-webhook /usr/local/bin/pebble
sudo rm -rf /usr/local/lib/pebble-webhook /etc/pebble-webhook   # and /var/lib/pebble-webhook to delete all data
sudo systemctl daemon-reload
```
