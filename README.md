# Pebble Index Webapp

> **Hardware:** made for the [Pebble Index 01](https://repebble.com/index) voice ring by Pebble (Core Devices) – [buy it on repebble.com](https://repebble.com/index) · official website: [repebble.com](https://repebble.com/). This is an independent community project, not affiliated with Pebble.

Self-hosted backend for the **Pebble Index 01** voice ring. It receives recordings via webhook, transcribes them locally with Whisper – by default, so no audio leaves your server – and turns them into notes, appointments or research plannings. An AI sorts everything into groups and lists; a mobile web app (PWA, English or German) shows the history, sends web push notifications and lets you answer follow-up questions.

**Main use:** whatever you say into the ring is sorted into the right list automatically. AI agents (Claude, ChatGPT & co. via the built-in MCP server) then read a list's open entries in one batch, work through them and tick them off.

One Go binary: webhook, worker, SQLite and web app. No queue, no second database.

## Features

- **AI of your choice**: OpenRouter with model ratings, local models via Ollama, OpenAI or any compatible service; per area fallback and monthly cost limit
- **Lists for AI agents**: entries sorted into groups and lists; Claude, ChatGPT & co. read, process and tick them off via the built-in MCP server (OAuth)
- **Private by default**: Whisper transcribes on your server, audio never leaves it
- **Encrypted**: each user's database and recordings encrypted on disk
- **Notes, appointments, plannings**: key facts, reminders via web push, web research in rounds
- **Ring usage**: recording time per ring, warnings and forecast (battery lasts about 12–15 h)

**More**

- Memory: learns names, terms and sorting rules you confirm
- Multiple users: own data per user, roles, invite links
- Tags, full-text search, trash with restore, web search via SearXNG
- Passkey login, setup wizard, themes, updates in the web app
- Calendar subscription (iCal), export to Joplin
- English or German

## Screenshots

Demo data, English UI, mobile, dark mode.

| History | Lists | Note | Planning |
|---|---|---|---|
| <img src="docs/screenshots/history.png" width="200" alt="History with filters and next appointment"> | <img src="docs/screenshots/lists.png" width="200" alt="Lists by group"> | <img src="docs/screenshots/note.png" width="200" alt="Note detail"> | <img src="docs/screenshots/planning.png" width="200" alt="Planning with research"> |

More: [all screenshots](docs/screenshots/) (menu, single list, reschedule, theme, ring usage).

## Quick start

On a Debian/Ubuntu server with systemd:

```bash
curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash -s -- --channel stable
```

### Docker

Image [`dirdante/pebble-index-webapp`](https://hub.docker.com/r/dirdante/pebble-index-webapp) for amd64 and arm64:

```bash
docker run -d --name pebble --restart unless-stopped -p 8080:8080 \
  -e TZ=Europe/Berlin -e PUID=1000 -e PGID=1000 \
  -v pebble-config:/config dirdante/pebble-index-webapp:stable
docker logs pebble    # address of the setup wizard
```

Or with Docker Compose ([compose.yaml](docker/compose.yaml)):

```bash
curl -fsSLO https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/docker/compose.yaml
docker compose up -d && docker compose logs pebble
```

Tags, paths, `PUID`/`PGID` and updates: [installation](docs/installation.md#docker).

### First start

1. Open the address shown by the installer (or the container log).
2. Do this within 5 minutes after the first start.
3. Choose *Setup*.
4. Too late? Restart the service. Only the first browser gets the wizard.
5. Put the service behind an HTTPS reverse proxy (passkeys need HTTPS).
6. Enter the public address and connect an AI provider (OpenRouter recommended; others under Status → AI).
7. Create your first passkey.

Transcription runs locally with Whisper from the start. The model is downloaded after saving.

## Tips

pebble-webhook is free and open source. If it is useful to you, a small tip on [Ko-fi](https://ko-fi.com/dirdante) helps keep it going – thank you!

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/A7O02B4AY7)

## Documentation

- [Installation](docs/installation.md) – requirements, installer, Docker, reverse proxy, uninstall
- [Configuration](docs/configuration.md) – setup wizard and all settings
- [Pebble app](docs/pebble-app.md) – connecting the ring
- [Usage](docs/usage.md) – what the web app does
- [Updates](docs/updates.md) – update check, changelog, releases
- [MCP server](docs/mcp.md) – read lists and tick items off from Claude, ChatGPT and other AI assistants
- [CHANGELOG](CHANGELOG.md)

## Development

```bash
go test ./...
printf 'LISTEN=127.0.0.1:8080\nDATA_DIR=./lokal/data\nMEDIA_DIR=./lokal/medien\n' > lokal/env
mkdir -p lokal/data lokal/audio
PEBBLE_ENV_FILE=./lokal/env go run ./cmd/pebble-webhook serve
```

The first start runs the setup wizard: open the address and choose *Setup* within 5 minutes. `http://localhost` is accepted as public address for local tests.

## License

Public domain ([Unlicense](LICENSE)): anyone may use, change, share and sell it, without conditions.
