# Installation

## Requirements

- Debian 12/13 or Ubuntu 22.04+ with systemd.
- amd64 or arm64; 1 CPU and 512 MB RAM are enough.
- HTTPS reverse proxy with a domain (nginx, Caddy, Nginx Proxy Manager …). Passkeys and web push need HTTPS.
- An AI provider: OpenRouter account (recommended), a local model or an API key (OpenAI & co.). Without one, recordings are only transcribed.
- Optional: a Joplin server for the one-way export.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash -s -- --channel stable
```

- Installation starts with the newest stable release.
- Switch to beta or nightly later in the web app under *Updates → Channel*.
- Or install one directly with `--channel beta` / `--channel nightly`.

The installer:

1. Downloads the latest release for your architecture and checks it against `SHA256SUMS`.
2. Creates:
   - system user `pebble`
   - `/var/lib/pebble-webhook` (databases)
   - `/srv/pebble-medien` (recordings, images)
   - `/srv/pebble-backup`
   - `/etc/pebble-webhook/env`
3. Installs the binary to `/usr/local/bin/pebble-webhook`, the CLI wrapper `/usr/local/bin/pebble` and the systemd units.
4. Starts the service and prints the address of the setup wizard.

Open the wizard within 5 minutes and choose *Setup*. Too late: `systemctl restart pebble-webhook`.

| Option | Effect |
|---|---|
| `--version X.Y.Z` | Specific release, also beta/nightly or older ones – see [updates](updates.md) |
| `--source` | Build from a cloned repository (needs Go) |
| `--repo owner/name` | Use a fork |

Running the installer again updates the program. Database, settings and env file stay untouched.

## Docker

Every release is also a Docker image for amd64 and arm64: `dirdante/pebble-index-webapp` on Docker Hub.

Ready-made [compose.yaml](../docker/compose.yaml). Every variable and own folder is in it, commented out. Remove the `#` to use one.

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
      # OPENROUTER_API_KEY: sk-or-...            # recommended: OpenRouter is ready as AI provider in the setup wizard
      # OPENAI_API_KEY: sk-...                   # fixed from here and locked in the app; else set in the app
      # YOUTUBE_API_KEY: AIza...                 # fixed from here and locked in the app; else set in the app
    volumes:
      - pebble-config:/config                    # everything in one Docker volume (default)
      # - ./config:/config                       # or a folder on the host instead of the volume
      # Own folders, e.g. on a NAS or separate disks – set the matching variable above as well:
      # - /srv/pebble/data:/data                 # DATA_DIR: /data
      # - /srv/pebble/keys:/keys                 # KEY_DIR: /keys
      # - /mnt/nas/pebble-media:/media           # MEDIA_DIR: /media
      # - /mnt/nas/pebble-backup:/backup         # BACKUP_DIR: /backup
volumes:
  pebble-config:
```

### Variables

All optional.

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
| `OPENROUTER_API_KEY` | – | recommended: OpenRouter is ready as AI provider in the setup wizard |
| `OPENAI_API_KEY` | – | wins over the web app and is locked there |
| `YOUTUBE_API_KEY` | – | wins over the web app and is locked there |

### Folders

- Without own paths, everything lives in `/config`: data, keys, media, backups, env file.
- Own folders: mount them and set the matching variable.
- Example: `/mnt/nas/pebble-media:/media` with `MEDIA_DIR: /media`.

### User and group

- The container starts as root.
- It sets owner and modes of `/config` and all data folders to `PUID:PGID`. This includes bind-mounted host folders.
- Then it runs as that user.
- `1000:1000` is the first user on most Linux systems. Check yours with `id`.
- With `docker run --user` it never runs as root. The folders must then already be writable for that user.

### API keys

- Recommended: set `OPENROUTER_API_KEY` (key from openrouter.ai/keys). The setup wizard then has OpenRouter ready as AI provider, with default models per area.
- `OPENAI_API_KEY` and `YOUTUBE_API_KEY` set in the environment always win over the web app.
- They are locked in the web app.
- Without them, enter the keys in the setup wizard as usual.
- The same applies to the systemd installation (`/etc/pebble-webhook/env`).

### Setup wizard (Docker)

- Open the address within 5 minutes after the first start and choose *Setup*.
- The address is in `docker compose logs pebble`.
- Too late: `docker compose restart pebble`.

### Updates

```bash
docker compose pull && docker compose up -d
```

- The app shows new versions and the `docker pull` command. It installs nothing itself.
- The tag decides the channel – see [updates](updates.md#docker).
- When a different version starts than last time, it first backs up the data as *before update*.
  - The backup has no recordings and is kept 30 days.
  - Then it brings databases and user data up to date by itself.

### Restore a backup

- In the web app under Backups, as usual (also *Upload backup*).
- The container restarts and brings the data back before it opens them.
- The current state is backed up first.
- The version stays that of the image. A backup from a newer version needs that version's image first.

### Back to an older version

1. Set the image to the old version.
2. Mark the backup made before the update and start:

```bash
docker compose run --rm pebble wiederherstellen            # list backups
docker compose run --rm pebble wiederherstellen pebble-4.1.0-stable-20261124-120000-update.tar.gz
docker compose up -d
```

This also works when the app no longer starts.

## Storage: own mounts (recommended)

Give the three data folders a volume each. In a Proxmox LXC, use a mount point per folder.

- Each can be sized, backed up and restored on its own.
- A full disk in one does not stop the others.
- **1 GB each** is enough to start. Grow them later if needed.

| Folder | Holds | Start value |
|---|---|---|
| `/var/lib/pebble-webhook/users` | one encrypted database per user | part of `DATA_DIR` |
| `/srv/pebble-medien` | recordings and images per user (`<id>/audio`, `<id>/bilder`) | `MEDIA_DIR` |
| `/srv/pebble-backup` | backup archives | `BACKUP_DIR` |

Proxmox example (container 209, storage `local-zfs`, sizes in GB). Create them before installing:

```bash
pct set 209 -mp0 local-zfs:1,mp=/var/lib/pebble-webhook/users,backup=1
pct set 209 -mp1 local-zfs:1,mp=/srv/pebble-medien,backup=1
pct set 209 -mp2 local-zfs:1,mp=/srv/pebble-backup,backup=1
pct reboot 209
```

### Ownership

- New or restored mount points usually belong to `root`. You do not need to fix that.
- Before every start (systemd `ExecStartPre`) and on every install, `pebble-webhook verzeichnisse` runs.
  - It creates missing folders.
  - It sets owner `pebble:pebble` and modes (0750, keys 0700).
- If a folder is still not writable, the service refuses to start.
- It names the folder in `journalctl -u pebble-webhook`.

### Add a mount to a running installation

1. `systemctl stop pebble-webhook`
2. Rename the folder, e.g. `mv users users-alt`.
3. Create the mount point at the old path.
4. Copy the content back: `cp -a users-alt/. users/`.
5. Delete the old folder.
6. Start the service.

## Reverse proxy

Forward your domain to `http://<server>:8080`. It needs WebSocket/SSE support and no buffering. Example for nginx:

```nginx
location / {
    proxy_pass http://10.0.0.5:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

Enter the proxy's IP as *Reverse-Proxy* in the settings. Then rate limits use the real client address.

## Setup wizard

- Open the address of the service, ideally already through your HTTPS domain.
- Choose *Setup*.
- Works only within 5 minutes after the service started, and only for the first browser.
- Afterwards restart the service and try again: `sudo systemctl restart pebble-webhook` or restart the container.
- See [configuration](configuration.md).

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

### What to back up

- `/var/lib/pebble-webhook` (with `users/`)
- `/srv/pebble-medien`
- `/etc/pebble-webhook/keys` – separately and safely

Without its key file, a user's data opens only with their recovery code. The code is shown once after setup. A new one is under *Status → Access and security*.

The keys protect backups and copies of the data folder. They do not protect a server someone has root access to.

## Uninstall

```bash
sudo systemctl disable --now pebble-webhook pebble-webhook-update.path
sudo rm -f /etc/systemd/system/pebble-webhook* /usr/local/bin/pebble-webhook /usr/local/bin/pebble
sudo rm -rf /usr/local/lib/pebble-webhook /etc/pebble-webhook   # and /var/lib/pebble-webhook to delete all data
sudo systemctl daemon-reload
```
