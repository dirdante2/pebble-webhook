# Installation

## Requirements

- Debian 12/13 or Ubuntu 22.04+ with systemd, amd64 or arm64; 1 CPU, 512 MB RAM are enough
- An HTTPS reverse proxy with a domain (nginx, Caddy, Nginx Proxy Manager …) – passkeys and web push need HTTPS
- An OpenAI API key
- Optional: a Joplin server for the one-way export

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash
```

The installer

1. downloads the latest release for your architecture and checks it against `SHA256SUMS`,
2. creates the system user `pebble`, `/var/lib/pebble-webhook` (database, audio) and `/etc/pebble-webhook/env`,
3. installs the binary to `/usr/local/bin/pebble-webhook`, the CLI wrapper `/usr/local/bin/pebble` and the systemd units,
4. starts the service and prints the link to the setup wizard.

Options: `--version X.Y.Z` (specific release, also beta/nightly or older ones – see [updates](updates.md)), `--source` (build from a cloned repository, needs Go), `--repo owner/name` (fork).

Running the installer again updates the program; database, settings and env file stay untouched.

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
| Start values | `/etc/pebble-webhook/env` (`LISTEN`, `DATA_DIR`, `AUDIO_DIR`) |
| Database | `/var/lib/pebble-webhook/pebble.db` (SQLite; settings, entries, passkeys) |
| Audio | `/var/lib/pebble-webhook/audio/YYYY/MM/` |
| Service | `systemctl status pebble-webhook`, logs `journalctl -u pebble-webhook` |
| Updater | `/usr/local/lib/pebble-webhook/install.sh`, units `pebble-webhook-update.{path,service}` |
| Backups | `/srv/pebble-backup/` (`BACKUP_DIR`) – archives with database, settings, version and channel: before every update (30 days) and scheduled (see [updates](updates.md#backups)) |

Back up `/var/lib/pebble-webhook`; it holds everything.

## Uninstall

```bash
sudo systemctl disable --now pebble-webhook pebble-webhook-update.path
sudo rm -f /etc/systemd/system/pebble-webhook* /usr/local/bin/pebble-webhook /usr/local/bin/pebble
sudo rm -rf /usr/local/lib/pebble-webhook /etc/pebble-webhook   # and /var/lib/pebble-webhook to delete all data
sudo systemctl daemon-reload
```
