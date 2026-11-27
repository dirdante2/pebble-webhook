# Configuration

All settings except the start values live in the database and are edited in the web app. The UI is English or German (setting `LANGUAGE`); this page uses the English labels, with the German one in brackets where it differs.

## Start values (`/etc/pebble-webhook/env`)

| Key | Default | Meaning |
|---|---|---|
| `LISTEN` | `0.0.0.0:8080` | address and port of the HTTP server |
| `DATA_DIR` | `/var/lib/pebble-webhook` | database and state |
| `MEDIA_DIR` | `/srv/pebble-medien` | recordings and images per user (`<id>/audio`, `<id>/bilder`; until 3.x `AUDIO_DIR`); own mount recommended, see [installation](installation.md#storage-own-mounts-recommended) |
| `BACKUP_DIR` | `/srv/pebble-backup` | backups; after changing it run `install.sh` again (creates the folder, allows the service to write there) |
| `KEY_DIR` | `/etc/pebble-webhook/keys` | one key file per user (never in a backup) |

Restart after changes: `sudo systemctl restart pebble-webhook`.

## Setup wizard

On the first start the service only runs the setup wizard. No link is printed: open the address and choose *Einrichten* within 5 minutes after the start (only the first browser gets it; afterwards restart the service). The same button creates the first passkey if none exists yet. It asks for your name (you become the first user and admin); every other field is prefilled with its default. *Prüfen* tests the OpenAI key. After saving, the service restarts, you create the first passkey and the welcome page shows your recovery code once – note it, it is the only way to reopen your encrypted data if the key file is lost.

If the stored settings ever become invalid, the service starts in the wizard again and shows the error.

## Settings in the web app

*Status → Konfiguration* lists every value. **?** opens a short explanation below it, the **pencil** opens a page to change just that value. Secrets are never shown again – leave the field empty to keep them. Changing the public address invalidates all passkeys.

Two parts: **personal** values (language, Pebble gestures, Joplin, display and memory) belong to each user, apply at once and are stored in that user's encrypted database; without an own value the system value applies. **System** values (address, OpenAI, prices, retention, updates, backups) apply to all users, only admins see and change them, and the service restarts after saving (a few seconds).

Every save is validated first: number ranges (e.g. max recording 1–2048 MB, retries 1–50), valid addresses (the public address must be `https://host` without path, query or invalid characters), reverse proxy as an IP, header and model names, update source (`owner/repo` or `aus`). Invalid values are rejected with a message and not stored, so the service keeps running.

| Group | Settings |
|---|---|
| Language (Sprache) | German or English (`LANGUAGE`, `de`/`en`): web app, messages, push, MCP answers and the texts the AI writes; transcription follows it. The setup wizard starts in the browser language and switches at once |
| Address (Adresse) | public HTTPS address, reverse proxy IP |
| Pebble app | token header name, gesture → notes or planning, max size, rate limit; webhook token and signing secret are per user (shown under *Status → Pebble app*) |
| OpenAI | API key and base URL, transcription (OpenAI or Pebble transcript), models for transcription, titles, research and notes, reasoning effort, cost limit per research, max output tokens |
| Prices (Preise) | prices for the cost overview and the research cost limit; research stays disabled while a research price is missing |
| Joplin | switch *aktiv/inaktiv*; when active, server, account, password and top notebook are set up and tested under *Status → Joplin* |
| Retention (Fristen) | empty trash after N days (recordings are only deleted together with their entry from the trash), delete empty recordings (no speech) after N hours – failed entries are never deleted automatically, auto-close plannings, retries, minimum free disk space |
| Display and memory | how long done items stay active, visit gap for "new" (shared by all devices), minimum history entries, memory training size |
| Access and updates | web push contact, update channel (stable / beta / nightly), automatic update (on by default, daily at `AUTO_UPDATE_TIME`, default 04:00), update source (`owner/repo`, `aus` = off) |
| Backup | schedule (`täglich` / `wöchentlich` / `monatlich` / `aus`, default daily), time (default 03:00), days to keep (default 30), with or without recordings |

## Users and roles

Every user has an own encrypted database, own recordings, devices, Pebble app access, API token, calendar link and MCP connections – nobody sees another user's data, admins included. **Admins** additionally manage updates, backups, system settings and users: *Menu → Users* invites a user (link valid 7 days, the user creates a passkey and gets a recovery code), changes the role, locks or deletes a user; per user only role, last sign-in, AI costs and storage are shown. The last admin cannot be demoted, locked or deleted.

The API token for machine access is created under *Status → Access and security* (shown once). Command line: `sudo pebble nutzer-anlegen <name> [admin]`, then `sudo PEBBLE_NUTZER=<id> pebble enroll` for the passkey link; `sudo pebble schluessel-wiederherstellen <id> <code>` writes a lost key file from the recovery code.

## Upgrading to 3.0

3.0 moves `pebble.db` into `system.db` and the encrypted `users/1/daten.db` of the first user (admin); passkeys, sessions, the Pebble app access, API token, calendar link and MCP connections keep working. The old file stays as `pebble.db.vor-3.0` (unencrypted – delete it once everything works). Older recordings are encrypted and moved 24 hours after the update. Going back to 2.x is only possible by restoring a 2.x backup. Update from 2.3 or later (its installer prepares the key folder).

## Upgrading from an env-only setup

Versions before 1.14 kept every setting in `/etc/pebble-webhook/env`. On the first start of 1.14 these values are imported into the database once; from then on only the start values in the env file are used.
