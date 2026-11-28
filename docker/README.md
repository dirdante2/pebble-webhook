# Pebble Index Webapp

Self-hosted backend and web app for the Pebble Index 01 voice ring.

Recordings arrive via webhook and are transcribed with OpenAI. They become notes, appointments or research plans.

- Notes, appointments with push reminders, plannings with web research
- AI sorts everything into groups and lists and learns your terms
- Mobile web app (PWA, English or German), passkey login
- Multiple users, each with an own encrypted database
- Calendar subscription, optional Joplin export, built-in MCP server
- Backups in the app; a new version backs up and migrates by itself

## Install

```bash
docker run -d --name pebble --restart unless-stopped -p 8080:8080 \
  -e TZ=Europe/Berlin -e PUID=1000 -e PGID=1000 \
  -v pebble-config:/config dirdante/pebble-index-webapp:stable
docker logs pebble
```

- Connect the AI provider in the setup wizard (OpenRouter recommended). With Compose, `OPENROUTER_API_KEY` can be set in the file.
- Within 5 minutes after the first start, open the address and choose *Setup*.
- Too late: `docker restart pebble`.
- Put the container behind an HTTPS reverse proxy. Passkeys need HTTPS.

## Docker Compose

```bash
curl -fsSLO https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/docker/compose.yaml
docker compose up -d
docker compose logs pebble
```

The compose file lists every option, commented out.

Update:

```bash
docker compose pull && docker compose up -d
```

## More

Tags, options, backups and source: [github.com/dirdante2/pebble-webhook](https://github.com/dirdante2/pebble-webhook)
