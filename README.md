# Haro Bot

An AI agent with Telegram integration, long-term memory, and tool execution capabilities.

## Features

- **Telegram Integration**: Real-time message streaming with draft previews
- **Long-term Memory**: Vector-based memory storage and retrieval using TiDB
- **Skill System**: Extensible skills synced from Git repositories
- **Tool Execution**: Filesystem operations, browser automation, command execution
- **Session Management**: Fork sessions for parallel task execution

## Quick Start

1. Start TiDB and create a database
2. Create a config file (see `config.example.toml`)
3. Run the server:

```bash
./agentd -config config.toml
```

### Command-line Flags

- `-config <path>`: Path to config file (default: `config.toml`)
- `-unrestricted`: Skip path restrictions and symlink checks (audit logging still enabled)

## Configuration

Configuration is primarily stored in `config.toml`. See `config.example.toml` for a complete example.

### Required Settings

- `db.tidb_dsn`: TiDB connection string

### Key Optional Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `server.addr` | `:8080` | HTTP server address |
| `llm.base_url` | `https://api.openai.com/v1` | LLM API endpoint |
| `llm.model` | `gpt-4o-mini` | Model to use |
| `telegram.token` | - | Telegram bot token |
| `skills.dir` | `./skills` | Skills directory |
| `fs.allowed_roots` | `[skills.dir]` | Allowed filesystem paths |

## Tools

### Filesystem Tools
- `read_file`: Read file contents with indentation-aware mode
- `list_dir`: List directory contents
- `grep_files`: Search file contents
- `exec_command`: Run shell commands
- `write_stdin`: Write to running process stdin

### Browser Tools (Playwright)
- `browser_goto`, `browser_go_back`
- `browser_get_page_state`, `browser_take_screenshot`
- `browser_click`, `browser_fill_text`, `browser_press_key`, `browser_scroll`

### Search Tools
- `brave_search`: Web search (requires `BRAVE_SEARCH_API_KEY`)

### Memory Tools
- `memory_search`: Search long-term memory
- `session_summary`: Create session checkpoint/handoff

### Skill Tools
- `install_skill`: Install skills from Git repos
- `activate_skill`: Activate an installed skill

### Session Tools
- `session_fork`: Start a child session for parallel tasks
- `session_interrupt`: Interrupt a child session
- `session_status`: Check child session status
- `session_cancel`: Cancel a child session

## Security

### Normal Mode
- Filesystem access restricted to `fs.allowed_roots`
- Symlink traversal blocked
- Telegram approval for out-of-bounds access
- Command execution audited

### Unrestricted Mode (`--unrestricted`)
- Path restrictions disabled
- Symlink checks disabled
- Approval requests disabled
- **Audit logging still enabled**

## HTTP Endpoints

- `GET /healthz`: Health check
- `POST /skills/register`: Register a skill source
- `POST /skills/refresh`: Refresh all skill sources

## Skills

Skills are directories containing `SKILL.md` with YAML frontmatter. Example:

```
skills/
  my-skill/
    SKILL.md
    ...
```

Register from Git:
```json
{
  "source_type": "git",
  "url": "https://github.com/example/skills.git",
  "ref": "main",
  "subdir": "skills"
}
```

## Development

```bash
# Build
go build ./cmd/agentd

# Run tests
go test ./...
```

## License

MIT
