<p align="center">
  <img src="assets/mimir.png" alt="Mimir Logo" width="200"/>
</p>

<h1 align="center">Mimir MCP</h1>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="https://opensource.org/licenses/MPL-2.0"><img src="https://img.shields.io/badge/License-MPL%202.0-brightgreen.svg" alt="License: MPL 2.0"></a>
  <a href="https://modelcontextprotocol.io/"><img src="https://img.shields.io/badge/MCP-Compatible-blue" alt="MCP"></a>
  <a href="https://hub.docker.com/r/tejzpr/mimir-mcp"><img src="https://img.shields.io/docker/v/tejzpr/mimir-mcp?label=Docker&logo=docker" alt="Docker Hub"></a>
</p>

<p align="center"><strong>Git-backed AI Memory System</strong></p>

Mimir is a Model Context Protocol (MCP) server that provides persistent, git-versioned memory storage for LLM applications. It combines the power of Git's version control with graph-based memory associations, enabling LLMs to maintain long-term memory.

## Features

- **🔐 Flexible Authentication**: Local mode (development) or SAML 2.0 (production with DUO/Okta)
- **📝 Git-Backed Storage**: Every memory change is a git commit with full history
- **🕸️ Graph Associations**: Link memories with typed relationships
- **🔍 Powerful Search**: Search by tags, dates, content, and associations
- **📊 Knowledge Graphs**: Traverse memory associations with N-hop queries
- **🔄 Auto-Sync**: Hourly synchronization to GitHub with PAT authentication
- **💾 Dual Storage**: Git repository (primary) + SQL database (index)
- **🗑️ Soft Delete**: Archive memories while preserving complete history
- **🏢 Multi-Database**: SQLite for development / local, PostgreSQL for production

## Architecture

<p align="center">
  <img src="assets/architecture.png" alt="Mimir Architecture" width="800"/>
</p>

## Requirements

- **Go 1.23+** - For building from source
- **Git 2.x+** - Required for version-controlled memory storage (git must be in your PATH)

## Quick Start

### 1. Build

```bash
make setup    # Run initial setup
make deps     # Install dependencies
make build    # Build the binary
```

### 2. MCP Client Integration

#### Option 1: Native Binary

Add to your MCP client config:

**Cursor** (`~/.cursor/mcp_settings.json`):
```json
{
  "mcpServers": {
    "mimir": {
      "command": "/path/to/bin/mimir",
      "env": {
        "ENCRYPTION_KEY": "your-32-char-encryption-key-here"
      }
    }
  }
}
```

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "mimir": {
      "command": "/path/to/bin/mimir",
      "args": [],
      "env": {
        "ENCRYPTION_KEY": "your-32-char-encryption-key-here"
      }
    }
  }
}
```

#### Option 2: Go Run

Run Mimir directly using `go run` without installing - similar to `uvx` for Python or `npx` for Node.js.

**Prerequisites:** Install Go 1.23+ [go.dev/dl](https://go.dev/dl/) and GIT in your PATH:

```bash
# macOS (Homebrew)
brew install go git

# Ubuntu/Debian
sudo apt update && sudo apt install golang-go git

# Windows (Chocolatey)
choco install golang git
```

**MCP Client Config:**

```json
{
  "mcpServers": {
    "mimir": {
      "command": "go",
      "args": ["run", "github.com/tejzpr/mimir-mcp/cmd/server@latest"],
      "env": {
        "ENCRYPTION_KEY": "your-32-char-encryption-key-here"
      }
    }
  }
}
```

#### Option 3: Docker Container

Configure your MCP client to use the Docker Hub image (no build required).

**Simple mode (single user):**

Uses `whoami` inside container, which returns `mimir`. All memories are stored under the `mimir` user.

```json
{
  "mcpServers": {
    "mimir": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-v", "/Users/yourname/.mimir:/home/mimir/.mimir",
        "-e", "ENCRYPTION_KEY=your-32-char-encryption-key-here",
        "tejzpr/mimir-mcp"
      ]
    }
  }
}
```

**Multi-user mode (with `--with-accessinguser`):**

Uses `ACCESSING_USER` env var for user identity. Useful when multiple users share the same Mimir instance or when integrating with authenticated systems.

```json
{
  "mcpServers": {
    "mimir": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-v", "/Users/yourname/.mimir:/home/mimir/.mimir",
        "-e", "ENCRYPTION_KEY=your-32-char-encryption-key-here",
        "-e", "ACCESSING_USER=yourname",
        "tejzpr/mimir-mcp",
        "--with-accessinguser"
      ]
    }
  }
}
```

> **Important:**
> - Replace `/Users/yourname` with your actual home directory path (e.g., `/Users/john` on macOS, `/home/john` on Linux)
> - Replace `yourname` in `ACCESSING_USER` with your actual username
> - MCP clients do not expand `$HOME` or `~` in JSON configs - use absolute paths
> - The `.mimir` mount persists your database and git repositories
> - The container runs as user `mimir` (uid 1000), data is stored at `/home/mimir/.mimir`

**Build locally (optional):**

```bash
docker build -t mimir-mcp .
```

Then use `mimir-mcp` instead of `tejzpr/mimir-mcp` in your config.

**Required mounts:**

| Mount | Purpose |
|-------|---------|
| `/Users/yourname/.mimir:/home/mimir/.mimir` | Persists database, config, and git repositories |

**Environment variables:**

| Variable | Purpose |
|----------|---------|
| `ENCRYPTION_KEY` | 32-character key for encrypting PAT tokens |
| `ACCESSING_USER` | Username for memory isolation (only with `--with-accessinguser`) |

**Flags explained:**
- `-i` = Interactive mode (required for stdio transport)
- `--rm` = Remove container when done
- `--with-accessinguser` = Use `ACCESSING_USER` env var instead of `whoami` for user identity

### 3. Run Modes

**stdio mode (default)**: For MCP client integration
```bash
./bin/mimir                      # Uses system user (whoami)
./bin/mimir --with-accessinguser # Uses ACCESSING_USER env var
```
- Communicates via stdin/stdout
- Automatically authenticates user
- No web interface needed

**HTTP mode**: For web interface
```bash
./bin/mimir --http              # Default port 8080
./bin/mimir --http --port=9000  # Custom port
```
- Provides web authentication at `http://localhost:8080/auth`
- Supports SAML 2.0 for enterprise SSO

### 4. Configuration (Optional)

Edit `~/.mimir/configs/config.json` for advanced settings:

```json
{
  "auth": {
    "type": "local"
  },
  "server": {
    "host": "localhost",
    "port": 8080
  },
  "database": {
    "type": "sqlite",
    "sqlite_path": "~/.mimir/db/mimir.db"
  },
  "git": {
    "sync_interval_minutes": 60
  },
  "security": {
    "token_ttl_hours": 24
  }
}
```

See [Configuration Guide](docs/configuration.md) for all options including SAML setup.

## MCP Tools

Mimir uses **human-aligned tools** that express intent rather than implementation. This makes them easier for LLMs to use correctly.

### mimir_recall
**"What do I know about X?"** - Find and retrieve information:
```json
{
  "topic": "authentication approach",
  "limit": 10
}
```

Also supports `list_all: true` for browsing and `exact: "text"` for literal search.

### mimir_remember
**"Store this for later"** - Create or update memories:
```json
{
  "title": "Project Alpha Kickoff",
  "content": "# Meeting Notes\n\nDiscussed project timeline...",
  "tags": ["project", "meeting"],
  "replaces": "old-meeting-notes"
}
```

Use `replaces` to supersede old information (marks it as outdated).

### mimir_history
**"When did I learn about X?"** - Temporal queries:
```json
{
  "slug": "project-alpha",
  "show_changes": true,
  "since": "7d"
}
```

### mimir_connect
**"These are related"** - Link or unlink memories:
```json
{
  "from": "project-alpha",
  "to": "contact-john-doe",
  "relationship": "person"
}
```

### mimir_forget
**"No longer relevant"** - Archive a memory:
```json
{
  "slug": "old-decision"
}
```

### mimir_restore
**"Bring back that archived memory"** - Undelete:
```json
{
  "slug": "old-decision"
}
```

### mimir_sync
Manual sync to GitHub:
```json
{
  "force": false
}
```

## Memory Format

Memories are stored as Markdown files with YAML frontmatter:

```markdown
---
id: project-alpha-kickoff-2024-01-15
title: "Project Alpha Kickoff Meeting"
tags: [project, meeting]
created: 2024-01-15T10:30:00Z
updated: 2024-01-15T14:00:00Z
associations:
  - target: contact-john-doe
    type: person
    strength: 1.0
---

# Project Alpha Kickoff Meeting

## Attendees
- John Doe (Project Lead)

## Key Decisions
...
```

## Directory Structure

```
~/.mimir/
├── configs/
│   └── config.json              # User configuration
├── db/
│   └── mimir.db                 # SQLite database (index)
└── store/
    └── mimir-{username}/        # User's git repository
        ├── 2024/
        │   └── 01/              # Date-organized memories
        ├── tags/
        │   └── meetings/        # Tag-based organization
        └── archive/             # Soft-deleted memories
```

## Development

```bash
# Run tests
make test

# Run with coverage
make test-unit

# Build
make build

# Lint
make lint
```

## Database Rebuild

If you need to rebuild the database index from your git repository:

```bash
# Rebuild from git (requires empty database)
mimir --rebuilddb

# Rebuild and overwrite existing data
mimir --rebuilddb --force
```

## Contributing

Contributions are welcome! Please ensure:
- All tests pass (`make test`)
- Code is properly formatted (`go fmt`)
- Linter passes (`make lint`)
- Documentation is updated

## License

This project is licensed under the [Mozilla Public License 2.0](LICENSE).


