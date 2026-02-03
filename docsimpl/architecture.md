
# Mimir MCP - Architecture Documentation

## System Overview

Mimir is a git-backed LLM memory system that provides persistent storage with full version control and graph-based associations. The system is built as an MCP (Model Context Protocol) server that LLMs can interact with through standardized tools.

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────┐
│                        LLM Client                            │
│                    (Claude, GPT, etc.)                       │
└────────────────────────┬─────────────────────────────────────┘
                         │ HTTP + JSON-RPC
                         │ MCP Protocol
                         ▼
┌──────────────────────────────────────────────────────────────┐
│                   Mimir MCP Server                   │
│  ┌────────────────────────────────────────────────────────┐  │
│  │             Authentication Layer                       │  │
│  │  - SAML 2.0 (DUO/Okta)                               │  │
│  │  - Token Management (Access + Refresh)                │  │
│  │  - Auth Middleware (validates every request)          │  │
│  └────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │        MCP Tools Layer (7 Human-Aligned Tools)        │  │
│  │  - mimir_recall (smart retrieval)                     │  │
│  │  - mimir_remember (store/update)                      │  │
│  │  - mimir_history, mimir_connect                       │  │
│  │  - mimir_forget, mimir_restore, mimir_sync            │  │
│  └────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │             Business Logic Layer                       │  │
│  │  - Memory Manager (markdown + frontmatter)            │  │
│  │  - Graph Manager (associations + traversal)           │  │
│  │  - Git Manager (commit, push, pull)                   │  │
│  │  - Crypto (PAT encryption)                            │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────┬────────────────────┬──────────────────────┘
                   │                    │
                   ▼                    ▼
    ┌──────────────────────┐   ┌──────────────────────┐
    │   Database Layer     │   │   Git Repository     │
    │   (Metadata Index)   │   │   (Primary Storage)  │
    │                      │   │                      │
    │  SQLite or Postgres  │   │  ~/.mimir/store/  │
    │                      │   │  mimir-{UUID}/    │
    │  Tables:             │   │                      │
    │  - users             │   │  Files:              │
    │  - auth_tokens       │   │  - *.md (memories)   │
    │  - git_repos         │   │  - Full git history  │
    │  - memories (index)  │   │  - Associations      │
    │  - associations      │   │  - Archive/          │
    │  - tags              │   │                      │
    └──────────────────────┘   └───────────┬──────────┘
                                           │
                                           ▼
                                 ┌──────────────────────┐
                                 │   GitHub Remote      │
                                 │   (Optional Sync)    │
                                 │   - Hourly sync      │
                                 │   - On-demand sync   │
                                 └──────────────────────┘
```

## Component Descriptions

### 1. Authentication Layer

**Purpose**: Secure access control using enterprise SAML authentication

**Components**:
- **SAML Authenticator**: Integrates with DUO or Okta for SSO
- **Token Manager**: Generates and validates JWT-style access tokens
- **Auth Middleware**: Validates tokens on every MCP request

**Flow**:
1. User accesses `/auth`
2. Redirected to SAML IdP (DUO/Okta)
3. IdP authenticates user, returns SAML assertion
4. Server extracts username, generates access/refresh tokens
5. Tokens stored in database with TTL
6. All subsequent MCP requests require valid token in `Authorization: Bearer <token>` header

### 2. Storage Architecture

**Two-Layer Model**: Git Repository (Primary) + Database (Index)

#### Git Repository Layer (Primary Storage)
- Stores actual memory content as markdown files
- Provides version control for every change
- Enables time-travel and audit trails
- Supports conflict resolution via commit history
- All memories exist as files in the repository

**Directory Organization**:
```
mimir-{UUID}/
├── 2024/
│   ├── 01/
│   │   ├── projects/
│   │   │   └── memory-1.md
│   │   └── meetings/
│   │       └── memory-2.md
│   └── 02/
├── tags/
│   ├── important/
│   └── research/
└── archive/
    └── deleted-memory.md
```

#### Database Layer (Index)
- Stores only metadata for fast queries
- Tracks file paths to locate memories in git repo
- Enables quick searches by tag, date, associations
- Duplicates association data for query performance
- Does NOT store actual memory content

**Why This Architecture?**:
- Git provides version control, audit trails, and sync capabilities
- Database provides fast querying without scanning all files
- Best of both worlds: durability + performance

### 3. MCP Tools Layer

All 7 human-aligned tools follow this pattern:
1. Validate authentication (middleware)
2. Parse and validate input
3. Query database for metadata
4. Read/write git repository files
5. Perform git operations (commit immediately)
6. Update database index
7. Return structured response

**Tool Categories**:
- **Retrieval**: `mimir_recall` (smart search, combines DB + grep + graph)
- **Storage**: `mimir_remember` (create, update, supersede, annotate)
- **Temporal**: `mimir_history` (git log, diffs, timeline)
- **Graph**: `mimir_connect` (link/unlink memories)
- **Lifecycle**: `mimir_forget`, `mimir_restore` (archive/unarchive)
- **Sync**: `mimir_sync` (manual git push/pull)

### 4. Graph System

**Purpose**: Create knowledge graphs of related memories

**Storage**: Hybrid approach
- Associations stored in markdown frontmatter (git-tracked)
- Duplicated in database table for fast queries

**Traversal**:
- Breadth-First Search (BFS): Explores all neighbors at current depth first
- Depth-First Search (DFS): Explores as far as possible along each branch

**Association Types**:
- `related_project`: Links to related projects
- `person`: Links to people/contacts
- `follows`: Sequential relationship (A follows B)
- `precedes`: Inverse of follows (A precedes B)
- `references`: Citations or references
- `related_to`: General relationship
- `supersedes`: A replaces/updates B (marks B as outdated)
- `part_of`: A is part of larger concept B

### 5. Security Model

**Defense in Depth**:

1. **Network Layer**: HTTPS/TLS for production
2. **Authentication**: SAML 2.0 enterprise SSO
3. **Authorization**: Token-based API access
4. **Encryption**: AES-256-GCM for PAT tokens at rest
5. **Audit**: Full git history for all operations

**Token Management**:
- Access tokens: Short-lived (configurable, default 24h)
- Refresh tokens: Longer-lived (2x access token TTL)
- Auto-refresh mechanism
- 401 response triggers reauthentication

### 6. Sync Mechanism

**Hourly Background Sync**:
- Scheduler runs every 60 minutes (configurable)
- Syncs all repositories with PAT tokens
- Uses last-write-wins for conflicts
- Continues on individual repository failures

**On-Demand Sync**:
- `mimir_sync` tool
- User-triggered via MCP
- Immediate push/pull operation
- Returns detailed sync status

**Conflict Resolution**:
- Strategy: Last-write-wins
- Local changes always take precedence
- Full commit history preserved for rollback
- Manual resolution possible via git commands

## Data Flow

### Storing a Memory (mimir_remember)

```
1. LLM calls mimir_remember with title, content
   ↓
2. Auth middleware validates token
   ↓
3. Check if slug exists (update vs create)
   ↓
4. Generate slug from title if not provided
   ↓
5. Create/update Memory object with frontmatter
   ↓
6. Determine file path (date/category/tags/path param)
   ↓
7. Write markdown file to git repo
   ↓
8. Git commit with message: "feat/update: memory 'slug'"
   ↓
9. Handle supersession if 'replaces' specified
   ↓
10. Insert/update record in MIMIR_MEMORIES table
   ↓
11. Insert tags into MIMIR_TAGS tables
   ↓
12. Return success with slug ID
```

### Retrieving Information (mimir_recall)

```
1. LLM calls mimir_recall with topic/exact/list_all
   ↓
2. Auth middleware validates token
   ↓
3. Search database by title, tags (if topic)
   ↓
4. Fall back to git grep (if exact)
   ↓
5. Expand results via 1-hop graph associations
   ↓
6. Filter superseded memories (unless requested)
   ↓
7. Rank by relevance + recency
   ↓
8. Load full content from markdown files
   ↓
9. Load annotations from database
   ↓
10. Return ranked results with full content
```

### Viewing History (mimir_history)

```
1. LLM calls mimir_history with slug/topic
   ↓
2. Auth middleware validates token
   ↓
3. Resolve slug (search if topic provided)
   ↓
4. Query git log for file history
   ↓
5. Optionally compute diffs between versions
   ↓
6. Format human-readable timeline
   ↓
7. Return history with commit messages
```

## Scalability Considerations

### Current Design (Single User / Small Team)
- SQLite for simplicity
- Local git repository
- In-process scheduler
- Single server instance

### Future Enhancements
- PostgreSQL for multi-user deployments
- Distributed git repositories per user
- Background job queue for sync operations
- Load balancer + multiple server instances
- Caching layer (Redis) for frequently accessed memories
- Full-text search (Elasticsearch/Meilisearch)
- Vector embeddings for semantic search

## Technology Choices

### Why Go?
- Excellent concurrency support
- Strong standard library (crypto, http, etc.)
- mcp-go framework available
- Easy deployment (single binary)
- Great performance for I/O operations

### Why Git?
- Built-in version control
- Industry-standard tool
- Conflict resolution mechanisms
- Easy sync to remote repositories
- Audit trail out of the box
- Time-travel capabilities

### Why GORM?
- Database agnostic (SQLite, Postgres)
- Auto-migrations
- Clean API
- Good performance for our use case

### Why Markdown + Frontmatter?
- Human-readable
- Git-friendly (text-based diffing)
- Standardized format (YAML frontmatter)
- Easy to edit manually if needed
- Portable across tools

## Deployment Patterns

### Development
```
└── Single machine
    ├── SQLite database
    ├── Local git repository
    └── HTTP server (no TLS)
```

### Production
```
└── Server (EC2, VPS, etc.)
    ├── PostgreSQL database
    ├── HTTPS/TLS enabled
    ├── SAML configured
    └── GitHub sync enabled
```

### Enterprise
```
└── Kubernetes cluster
    ├── Multiple replicas
    ├── PostgreSQL (RDS/managed)
    ├── Load balancer
    ├── Git repositories on persistent volumes
    └── SAML with company IdP
```

## Monitoring & Observability

**Recommended Metrics**:
- Authentication success/failure rates
- Token expiration and refresh rates
- Memory creation/update/delete rates
- Git sync success/failure rates
- Database query performance
- Storage usage (git repo size)

**Logging**:
- Structured logging with levels (DEBUG, INFO, WARN, ERROR)
- All authentication events logged
- Git operations logged
- Tool invocations logged with user ID

## Future Roadmap

1. **Vector Search**: Semantic search using embeddings
2. **AI Summaries**: Auto-generate memory summaries
3. **Multi-User Sharing**: Share memories between users
4. **Web UI**: Browse and visualize memory graphs
5. **Import/Export**: Bulk operations
6. **Webhooks**: Notifications on memory changes
7. **API Gateway**: REST API alongside MCP
8. **Mobile Clients**: iOS/Android apps
