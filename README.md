# Pebble Index Webapp

> **Hardware:** made for the [Pebble Index 01](https://repebble.com/index) voice ring by Pebble (Core Devices) – [buy it on repebble.com](https://repebble.com/index) · official website: [repebble.com](https://repebble.com/). This is an independent community project, not affiliated with Pebble.

Self-hosted backend for the **Pebble Index 01** voice ring. It receives recordings via webhook, transcribes them with OpenAI and turns them into notes, appointments or research plannings. An AI sorts everything into groups and lists; a mobile web app (PWA, English or German) shows the history, sends web push notifications and lets you answer follow-up questions.

One Go binary: webhook, worker, SQLite and web app. No queue, no second database.

## Features

- **Notes** with key facts, hints and list items; **appointments** with reminders (web push)
- **Plannings**: web research in rounds, continued by voice or text, with open questions
- **Lists and groups** sorted by the AI; tags, full-text search, trash with restore; new entries marked across all devices
- **Memory**: the AI learns names, terms and sorting rules you confirm
- **Calendar subscription** (iCal link) for Google Calendar, Apple or Outlook
- Optional one-way export to a **Joplin** server
- Built-in **MCP server** with OAuth: connect Claude, ChatGPT & co., read lists and tick items off from AI chats
- **Ring usage**: the Index 01 battery lasts about 12–15 hours of recording. Every recording is counted (seconds, notes vs. plannings, button presses) per ring ID (`#12345`); warnings at 60/80/95/100 %, forecast at constant use, ring swap with comparison
- **English or German** – UI, messages, push and MCP answers, and the texts the AI writes; chosen in the setup wizard (preset from the browser), changeable under Status → Configuration
- Passkey login (new devices with a 4-digit code), setup wizard, settings with explanations and validation, **themes** (day/night, colour palettes) and **updates** in the web app

## Screenshots

Demo data, English UI, mobile, dark mode.

| History | Menu | Lists | Note |
|---|---|---|---|
| <img src="docs/screenshots/history.png" width="200" alt="History with filters and next appointment"> | <img src="docs/screenshots/menu.png" width="200" alt="Menu with counters"> | <img src="docs/screenshots/lists.png" width="200" alt="Lists by group"> | <img src="docs/screenshots/note.png" width="200" alt="Note detail"> |
| **Planning** | **Reschedule** | **List** | **Theme** |
| <img src="docs/screenshots/planning.png" width="200" alt="Planning with research"> | <img src="docs/screenshots/reschedule.png" width="200" alt="Reschedule an appointment"> | <img src="docs/screenshots/list.png" width="200" alt="A single list"> | <img src="docs/screenshots/theme.png" width="200" alt="Theme settings"> |
| **Ring usage** | **Rings compared** | **Swap ring** | |
| <img src="docs/screenshots/ring.png" width="200" alt="Ring usage with forecast"> | <img src="docs/screenshots/ring-compare.png" width="200" alt="Recording time per month and rings compared"> | <img src="docs/screenshots/ring-swap.png" width="200" alt="Swap ring with instructions"> | |

## Quick start

On a Debian/Ubuntu server with systemd:

```bash
curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash -s -- --channel stable
```

The installer prints a link to the setup wizard. Put the service behind an HTTPS reverse proxy (passkeys need HTTPS), enter your OpenAI key and the public address, then create your first passkey.

## Tips

pebble-webhook is free and open source. If it is useful to you, a small tip on [Ko-fi](https://ko-fi.com/dirdante) helps keep it going – thank you!

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/A7O02B4AY7)

## Documentation

- [Installation](docs/installation.md) – requirements, installer, reverse proxy, uninstall
- [Configuration](docs/configuration.md) – setup wizard and all settings
- [Pebble app](docs/pebble-app.md) – connecting the ring
- [Usage](docs/usage.md) – what the web app does
- [Updates](docs/updates.md) – update check, changelog, releases
- [MCP server](docs/mcp.md) – read lists and tick items off from Claude, ChatGPT and other AI assistants
- [CHANGELOG](CHANGELOG.md)

## Development

```bash
go test ./...
printf 'LISTEN=127.0.0.1:8080\nDATA_DIR=./lokal/data\nAUDIO_DIR=./lokal/audio\n' > lokal/env
mkdir -p lokal/data lokal/audio
PEBBLE_ENV_FILE=./lokal/env go run ./cmd/pebble-webhook serve
```

The first start runs the setup wizard (link in the log). `http://localhost` is accepted as public address for local tests.

## License

Public domain ([Unlicense](LICENSE)): anyone may use, change, share and sell it, without conditions.
