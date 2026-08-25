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
- Kubernetes Agent Sandbox workspaces with persistent storage, per-agent encrypted environment variables, and interactive process controls

## Requirements

- Go 1.22+
- Node.js 22+
- TiDB 8.5+
- An S3-compatible object store such as MinIO
- An OpenAI-compatible Chat Completions or Responses provider

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
| `web.public_url` / `HARO_WEB_PUBLIC_URL` | Public origin used for MCP OAuth callbacks |
| `web.object_storage.*` | Private S3-compatible attachment storage |
| `telegram.token` | Telegram bot token; leave empty to disable Telegram startup |
| `skills.*` | Local skill directory and Git source policy |
| `sandbox.*` | Optional Kubernetes Agent Sandbox control plane, image, runtime class, and resource limits |

MCP servers are managed in the web Settings page. Streamable HTTP servers connect from `agentd`; stdio servers run inside the assigned Agent Sandbox. Every Agent with a Sandbox also receives the fixed `agent-browser` core MCP profile. MCP credentials and OAuth tokens are encrypted with `HARO_SECRET_KEY` (the legacy `HARO_SANDBOX_SECRET_KEY` remains a fallback).

Providers are global connection records containing an OpenAI-compatible Base URL, optional API key, API mode, and prompt format. Responses providers may enable provider-hosted web search for interactive agent turns; citations returned in model text are preserved. Agents select a Provider and keep their own model, reasoning override, instructions, context overrides, skills, and visual identity. Haro reads `/models` and caches any capability metadata the Provider exposes; ID-only responses and manual model/runtime values remain supported. Provider keys are stored in the database but are never returned by the web API or written to logs; protect database access accordingly.

Telegram keeps only its token in TOML. Select the Telegram Agent under Settings → Integrations; changing the binding takes effect without restarting and preserves separate conversation history per Agent.

Web-agent runtimes expose `get_own_profile` and `update_own_profile`. These tools are bound to the current agent and cannot change provider credentials, prompt format, archive state, global guidelines, or another agent. An avatar URL used by the update tool must resolve to a public HTTP or HTTPS address; downloaded images are size- and type-checked before being copied to private object storage.

### Code execution sandboxes

Sandbox execution is disabled by default. Haro uses the Kubernetes SIGs [Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) `agents.x-k8s.io/v1beta1` API from release v0.5.3. Install its controller and CRDs first, configure the `gvisor` RuntimeClass on eligible nodes, and then apply the example namespace, RBAC, and network policy:

```bash
kubectl apply -f deploy/sandbox/namespace.yaml
kubectl apply -f deploy/sandbox/rbac.yaml
kubectl apply -f deploy/sandbox/runtimeclass-gvisor.yaml
kubectl apply -f deploy/sandbox/network-policy.yaml
```

The manifests assume Haro runs as service account `haro-bot` in namespace `haro-system`, while execution Pods run in `haro-sandboxes`. Adjust both the RoleBinding and NetworkPolicy if your deployment uses different names. The network policy permits runtime traffic only from labeled Haro Pods and deliberately leaves Sandbox egress unrestricted.

Generate the mandatory database encryption key without putting it in TOML:

```bash
openssl rand -base64 32
export HARO_SANDBOX_SECRET_KEY='the-generated-value'
```

Then set `sandbox.enabled = true`. A Sandbox is created explicitly in the UI and may be shared by multiple Agents; an Agent belongs to at most one Sandbox. Agents assigned to the same Sandbox are one trust domain: they share root access to the workspace and can inspect or control each other's processes, including process environment. Use separate Sandboxes when secrets must remain isolated. The Pod is not stopped for idleness and processes have no automatic lifetime limit. Agents receive only the Codex-style `exec_command` and `write_stdin` tools: an unfinished command returns an opaque `session_id`, and empty `write_stdin` calls wait at least five seconds while returning only new output. The default maximum wait for one empty poll is 300 seconds and is configurable with `sandbox.background_terminal_max_timeout_ms`; it does not limit the process lifetime. Pause, start, apply/restart, reset, and delete are manual operations. `/workspace` is a persistent PVC, while changes elsewhere in the container are lost when Agent Sandbox recreates the Pod. Secrets are encrypted at rest, injected only into the invoking Agent's process, omitted from API responses, and exactly matched values are masked from recorded commands and logs.

For container deployments, `HARO_SANDBOX_ENABLED`, `HARO_SANDBOX_NAMESPACE`, `HARO_SANDBOX_RUNTIME_CLASS`, `HARO_SANDBOX_STORAGE_CLASS`, `HARO_SANDBOX_DEFAULT_IMAGE`, `HARO_SANDBOX_HELPER_IMAGE`, and `HARO_SANDBOX_BACKGROUND_TERMINAL_MAX_TIMEOUT_MS` can override the corresponding non-secret TOML settings.

Keep `HARO_SANDBOX_SECRET_KEY` stable and back it up like any other application encryption key. Losing or changing it makes existing runtime credentials and Agent environment values unreadable; use **Apply & restart** to rotate a Sandbox's runtime certificate and bearer token.

The default development image includes Go, Node.js, Python, build tools, and a MySQL client. Build it with:

```bash
docker build -f sandbox/Dockerfile -t ghcr.io/yangkeao/haro-bot-sandbox:latest .
```

Custom OCI images are accepted but must provide `/bin/sh`; Haro injects the static `haro-sandboxd` runtime through an init container. Sandbox processes run as root inside gVisor with a bounded capability set and no privilege escalation. The main Haro image remains non-root and has no host execution tools registered.

For production, serve Haro Bot over HTTPS, set `web.cookie_secure = true`, keep the object bucket private, and use a high-entropy access token.

## Web API

The UI uses a versioned JSON/SSE API under `/api/v1`:

- `/auth`: login, logout, and session status
- `/providers`: manage shared connections and refresh cached model catalogs
- `/agents`: create, update, archive, and restore agents; create/update accepts JSON or multipart form data for avatar uploads
- `/sandboxes`: create and configure execution workspaces; explicit lifecycle actions live below each Sandbox
- `/agents/{id}/environment`: manage per-Agent process environment variables without returning secret values
- `/sessions/{id}/processes`: inspect processes launched in a conversation; process endpoints accept stdin, TERM, and KILL
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

Build the complete production image with `docker build .`. The Dockerfile builds the Vite assets before compiling the Go server. The Docker workflow publishes both `haro-bot` and `haro-bot-sandbox` images to GHCR. After a successful `master` build, it pins both new digests in `YangKeao/homelab`, opens a deployment PR, and squash-merges it when GitHub reports no conflicts. Configure the haro-bot Actions secret `HOMELAB_TOKEN` with a fine-grained token limited to the homelab repository and grant only Contents and Pull requests read/write access.

## License

MIT
