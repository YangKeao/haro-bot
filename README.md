# Haro Bot

Haro Bot is a self-hosted AI agent workspace with a React web UI and Telegram integration.

## Features

- Multi-agent web workspace with shared Provider connections and per-agent model, instructions, visual identity, and skill selection
- Automatic model discovery with optional reasoning, context-window, compaction, and modality metadata
- Agent-owned profile tools for updating its instructions, identity, avatar, model, context, and skills when explicitly requested
- Multi-session chat with streaming output, reasoning/tool activity, cancellation, rename, archive, and restore
- Image attachments backed by a private S3-compatible object store
- Global guidelines shared by web agents and the Telegram agent
- Git-backed skill source management
- Responsive light UI for desktop, tablet, and mobile, with a persistent accent color and labeled navigation drawer
- WYSIWYG Markdown authoring for conversations, agent instructions, and global guidelines
- Single-user web authentication with an HttpOnly session cookie
- Telegram routing to any ordinary agent, with streamed draft previews and tool execution

## Requirements

- Go 1.22+
- Node.js 22+
- TiDB 8.5+
- An S3-compatible object store such as MinIO
- An OpenAI-compatible Chat Completions provider

## Quick Start

1. Start TiDB and create the `haro_bot` database.
2. Start an S3-compatible object store and create credentials Haro Bot can use. The configured bucket is created automatically.
3. Build the web application:

   ```bash
   cd web
   npm ci
   npm run build
   cd ..
   ```

4. Copy `config.example.toml` to `config.toml` and set the database, web token, and object storage values.
5. Start Haro Bot:

   ```bash
   go run ./cmd/agentd -config config.toml
   ```

6. Open `http://localhost:8080`, sign in with `web.access_token`, create a Provider, and then create an Agent.

The web configuration is mandatory. Startup fails if the access token, built assets, or object storage are unavailable.

## Configuration

See `config.example.toml` for all settings. The main settings are:

| Setting | Purpose |
| --- | --- |
| `server.addr` | HTTP listen address |
| `db.tidb_dsn` | TiDB connection string |
| `web.access_token` | Single-user web sign-in token; use a long random value |
| `web.cookie_secure` | Require HTTPS when sending the authentication cookie |
| `web.assets_dir` | Vite production build directory |
| `web.object_storage.*` | Private S3-compatible attachment storage |
| `telegram.token` | Telegram bot token; leave empty to disable Telegram startup |
| `skills.*` | Local skill directory and Git source policy |

Providers are global connection records containing an OpenAI-compatible Base URL, optional API key, and prompt format. Agents select a Provider and keep their own model, reasoning override, instructions, context overrides, skills, and visual identity. Haro reads `/models` and caches any capability metadata the Provider exposes; ID-only responses and manual model/runtime values remain supported. Provider keys are stored in the database but are never returned by the web API or written to logs; protect database access accordingly.

Telegram keeps only its token in TOML. Select the Telegram Agent under Settings → Integrations; changing the binding takes effect without restarting and preserves separate conversation history per Agent.

Web-agent runtimes expose `get_own_profile` and `update_own_profile`. These tools are bound to the current agent and cannot change provider credentials, prompt format, archive state, global guidelines, or another agent. An avatar URL used by the update tool must resolve to a public HTTP or HTTPS address; downloaded images are size- and type-checked before being copied to private object storage.

For production, serve Haro Bot over HTTPS, set `web.cookie_secure = true`, keep the object bucket private, and use a high-entropy access token.

## Web API

The UI uses a versioned JSON/SSE API under `/api/v1`:

- `/auth`: login, logout, and session status
- `/providers`: manage shared connections and refresh cached model catalogs
- `/agents`: create, update, archive, and restore agents; create/update accepts JSON or multipart form data for avatar uploads
- `/integrations/telegram`: inspect token status and select the Telegram agent
- `/agents/{id}/avatar`: return an authenticated agent avatar from private object storage
- `/agents/{id}/sessions`: create and list sessions
- `/sessions/{id}`: rename, archive, restore, list messages, run, and cancel
- `/sessions/{id}/attachments`: upload image attachments
- `/guidelines`: edit the global guideline and inspect its history
- `/skills` and `/skill-sources`: inspect skills and manage Git sources

All endpoints except login require the signed HttpOnly cookie. Mutating requests also enforce same-origin checks.

Operational endpoints remain available at `GET /healthz` and `/debug/pprof/*`.

## Development

Run the backend checks:

```bash
go test ./...
```

Run database-backed integration tests against the configured TiDB instance:

```bash
go test -tags=integration ./internal/... -count=1
```

Tests that call a real model endpoint are intentionally excluded from hosted CI. Run
the complete integration and E2E suite from a network that can reach the provider:

```bash
go test -tags='integration live_llm' ./internal/... -count=1
```

Run the frontend locally (API requests proxy to port 8080):

```bash
cd web
npm ci
npm run dev
```

Run all frontend checks:

```bash
npm run typecheck
npm run lint
npm test
npx playwright install chromium
npm run test:e2e
npm run build
```

Build the complete production image with `docker build .`. The Dockerfile builds the Vite assets before compiling the Go server.

## License

MIT
