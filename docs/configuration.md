# Configuration

All settings except the three start values live in the database and are edited in the web app. The UI is English or German (setting `LANGUAGE`); this page uses the English labels, with the German one in brackets where it differs.

## Start values (`/etc/pebble-webhook/env`)

| Key | Default | Meaning |
|---|---|---|
| `LISTEN` | `0.0.0.0:8080` | address and port of the HTTP server |
| `DATA_DIR` | `/var/lib/pebble-webhook` | database and state |
| `AUDIO_DIR` | `/var/lib/pebble-webhook/audio` | recordings |
| `BACKUP_DIR` | `/srv/pebble-backup` | backups; after changing it run `install.sh` again (creates the folder, allows the service to write there) |

Restart after changes: `sudo systemctl restart pebble-webhook`.

## Setup wizard

On the first start the service only runs the setup wizard. It is protected by a one-time code (`sudo pebble ersteinrichtung` prints the link). Every field is prefilled with its default; webhook token and signing secret are generated. *Prüfen* tests the OpenAI key. After saving, the service restarts and you create the first passkey.

If the stored settings ever become invalid, the service starts in the wizard again and shows the error.

## Settings in the web app

*Status → Konfiguration* lists every value. **?** opens a short explanation below it, the **pencil** opens a page to change just that value. Secrets are never shown again – leave the field empty to keep them. The service restarts after saving (a few seconds). Changing the public address invalidates all passkeys.

Every save is validated first: number ranges (e.g. max recording 1–2048 MB, retries 1–50), valid addresses (the public address must be `https://host` without path, query or invalid characters), reverse proxy as an IP, header and model names, update source (`owner/repo` or `aus`). Invalid values are rejected with a message and not stored, so the service keeps running.

| Group | Settings |
|---|---|
| Language (Sprache) | German or English (`LANGUAGE`, `de`/`en`): web app, messages, push, MCP answers and the texts the AI writes; transcription follows it. The setup wizard starts in the browser language and switches at once |
| Address (Adresse) | public HTTPS address, reverse proxy IP |
| Pebble app | token header name, webhook token, signing secret, gesture → notes or planning, max size, rate limit |
| OpenAI | API key and base URL, transcription (OpenAI or Pebble transcript), models for transcription, titles, research and notes, reasoning effort, cost limit per research, max output tokens |
| Prices (Preise) | prices for the cost overview and the research cost limit; research stays disabled while a research price is missing |
| Joplin | switch *aktiv/inaktiv*; when active, server, account, password and top notebook are set up and tested under *Status → Joplin* |
| Retention (Fristen) | empty trash after N days (recordings are only deleted together with their entry from the trash), move empty or failed recordings to the trash, auto-close plannings, retries, minimum free disk space |
| Display and memory | how long done items stay active, visit gap for "new" (shared by all devices), minimum history entries, memory training size |
| Access and updates | API token for machine access, web push contact, update channel (stable / beta / nightly), automatic update (on by default, daily at `AUTO_UPDATE_TIME`, default 04:00), update source (`owner/repo`, `aus` = off) |
| Backup | schedule (`täglich` / `wöchentlich` / `monatlich` / `aus`, default daily), time (default 03:00), days to keep (default 30), with or without recordings |

## Upgrading from an env-only setup

Versions before 1.14 kept every setting in `/etc/pebble-webhook/env`. On the first start of 1.14 these values are imported into the database once; from then on only the start values in the env file are used.
