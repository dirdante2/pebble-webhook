# MCP server

pebble-webhook has a built-in [MCP](https://modelcontextprotocol.io) server, so AI assistants (Claude, ChatGPT/OpenAI, …) can read your lists and tick items off in a chat.

- **Endpoint:** `https://<your-domain>/api/v1/mcp` (Streamable HTTP, JSON responses)
- **Auth:** OAuth 2.1 (for connectors in Claude, ChatGPT and other apps) or a fixed `Authorization: Bearer <API token>` – create it under *Status → Access and security*. Both give access only to the data of the user who connected or created the token.

## Tools

| Tool | What it does |
|---|---|
| `listen_uebersicht` | Groups and lists with their items (`[ ]` open, `[x]` done). Optional: `gruppe`, `liste` (`L23` or name), `alle` (include done items). |
| `punkt_lesen` | Full text for items: the note with key facts, hints and transcript, or the current state of a planning. |
| `punkte_abhaken` | Tick items off one by one (`offen: true` re-opens them). |
| `termine` | Appointments with date, time, place and number, starting with today's date. Optional: `tage` (next N days), `vergangene` (also past N days). |
| `notizen_suchen` | Full-text search in all notes, appointments and plannings (same as the search bar). |
| `notiz_lesen` | Full text of any note or planning by number (`#63`), also when it is in no list. |

Every item has an id: `#42`, or `#59.1`, `#59.2` … when one note produced several items. A bare `#59` is rejected if it is ambiguous; `#59.*` addresses all items of the note.

## Connecting

**Claude (claude.ai, desktop, mobile) and ChatGPT – with OAuth**

Add a custom connector with the URL `https://<your-domain>/api/v1/mcp`, no client id or secret needed. The app opens a page of your web app: sign in with your passkey and tap *Erlauben*. Connected apps are listed under *Status → Verbundene Apps (MCP)*, where *Trennen* revokes access immediately.

Details: dynamic client registration (`/api/v1/oauth/register`), authorization code with PKCE (S256), access tokens valid 1 hour, refresh tokens 90 days (rotated). Redirect addresses must be https (http only for localhost). Tokens only work for `/api/v1/mcp`.

**Claude Code**

OAuth (opens the browser on first use):

```bash
claude mcp add --transport http sprachnotizen https://<your-domain>/api/v1/mcp
```

Or with the API token:

```bash
claude mcp add --transport http sprachnotizen https://<your-domain>/api/v1/mcp --header "Authorization: Bearer <API token>"
```

**OpenAI (Responses API)**

```json
{"type": "mcp", "server_label": "sprachnotizen", "server_url": "https://<your-domain>/api/v1/mcp",
 "headers": {"Authorization": "Bearer <API token>"}, "require_approval": "never"}
```
