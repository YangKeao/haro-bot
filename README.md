# Haro Bot (Agent Skeleton)

This service provides an OpenAI-compatible API and a Telegram webhook, with all memory stored in TiDB. Skills follow the Agent Skills filesystem spec and are synced from a Git repo. Data access uses GORM; schema migrations are applied from embedded SQL.

**Quick Start**
1. Start TiDB and create a database.
2. Set `TIDB_DSN` (all other config is stored in `app_config` and defaults will be seeded on first boot).
3. Run the server.

**Environment Variables**
- `TIDB_DSN` (default `root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true`) is the only required value to boot. Most configuration is stored in the database (`app_config`).
- The following env vars are optional overrides and will update the in-memory config (and seed DB defaults on first boot):
  - `SERVER_ADDR` (default `:8080`)
  - `LLM_BASE_URL` (default `https://api.openai.com/v1`)
  - `LLM_API_KEY`
  - `LLM_MODEL` (default `gpt-4o-mini`)
  - `LLM_PROMPT_FORMAT` (default `openai`; use `claude`/`anthropic`/`xml` for XML skill injection)
  - `TELEGRAM_BOT_TOKEN`
  - `SKILLS_DIR` (default `./skills`)
  - `SKILLS_REPO_ALLOWLIST` (comma-separated URL prefixes)
  - `SKILLS_SYNC_INTERVAL` (default `10m`)
  - `BRAVE_SEARCH_API_KEY`
  - `FS_ALLOWED_ROOTS` (comma-separated absolute/relative paths; default `SKILLS_DIR`)
  - `FS_ALLOWED_EXEC_DIRS` (comma-separated relative paths for exec; default `scripts/`)
  - `SKILLS_ALLOWED_SCRIPT_DIRS` is accepted for backward compatibility.
  - `TOOL_MAX_TURNS` (default `1024`)
  - `LOG_LEVEL` (debug/info/warn/error)
  - `LOG_DEV` (true/false; enables console logging)
  - `LOG_ENCODING` (json/console; overrides default)

**Skills Repo Layout**
Each skill is a directory containing `SKILL.md` with YAML frontmatter. The skill directory name must match the `name` field in the frontmatter.
Filesystem tools are global (available even without skill activation) and are protected by `FS_ALLOWED_ROOTS`: `read`, `write`, `search`, `edit`, `exec`.
Search tools: `brave_search` (requires `BRAVE_SEARCH_API_KEY`).
Browser tools use headless Playwright: `browser_goto`, `browser_go_back`, `browser_get_page_state`, `browser_take_screenshot`, `browser_click`, `browser_fill_text`, `browser_press_key`, `browser_scroll`.

**Security Notes**
- Skills are fetched only from allowed repo URL prefixes.
- Filesystem tools operate only within `FS_ALLOWED_ROOTS` and reject symlink traversal.
- Script execution is always available but restricted to allowed roots and exec dirs. Consider running the service inside a container or sandbox for stronger isolation.
- Skills are synced using `go-git` (no system `git` binary required).
- Subdirectory-only repositories are supported via `source_subdir` (TODO: sparse checkout when go-git adds support).
- Filesystem tool usage is audited in `tool_audit`.

**HTTP Endpoints**
- `GET /healthz`
- `POST /skills/register`
- `POST /skills/refresh`

Telegram integration uses long polling (`getUpdates`) and does not require a webhook.

**Register Skill Source Example**
```json
{
  "source_type": "git",
  "install_method": "git",
  "url": "https://github.com/anthropics/skills.git",
  "ref": "main",
  "subdir": "skills"
}
```

**In-Process Example**
Call `agent.Handle`/`agent.HandleWithModel` directly from your code to use the agent.
