# Mimir MCP - API Reference

Complete reference for all 7 human-aligned MCP tools.

## Overview

Mimir uses a human-cognition-aligned tool design that expresses intent rather than implementation. This makes it easier for LLMs to use the tools correctly - each tool has a clear purpose and internally orchestrates multiple operations.

### Tool Summary

| Tool | Intent | Use When |
|------|--------|----------|
| `mimir_recall` | "What do I know about X?" | Finding information |
| `mimir_remember` | "Store this for later" | Creating/updating memories |
| `mimir_history` | "When did I learn about X?" | Temporal queries |
| `mimir_connect` | "These are related" | Linking memories |
| `mimir_forget` | "No longer relevant" | Archiving memories |
| `mimir_restore` | "Bring back that archived memory" | Undeleting |
| `mimir_sync` | "Sync with remote" | Git synchronization |

## Authentication

All MCP tools require authentication via Bearer token:

```http
Authorization: Bearer <access_token>
```

### Getting a Token

1. Navigate to `/auth`
2. Authenticate via SAML or local auth
3. Receive access token
4. Use token in all subsequent MCP requests

---

## Tool: mimir_recall

**Intent**: "What do I know about X?"

Find and retrieve information from memory. This is the primary tool for getting information - it searches everything: titles, content, tags, associations. Returns full content, ranked by relevance.

### Input Schema

```json
{
  "topic": "string (optional) - What you want to know about",
  "exact": "string (optional) - Exact text search",
  "list_all": "boolean (optional) - Browse all memories",
  "path": "string (optional) - Limit to folder path",
  "include_superseded": "boolean (optional, default false)",
  "include_archived": "boolean (optional, default false)",
  "limit": "number (optional, default 10)"
}
```

### Example Requests

**Search by topic:**
```json
{
  "tool": "mimir_recall",
  "arguments": {
    "topic": "authentication approach"
  }
}
```

**List all memories:**
```json
{
  "tool": "mimir_recall",
  "arguments": {
    "list_all": true,
    "limit": 20
  }
}
```

**Exact text search:**
```json
{
  "tool": "mimir_recall",
  "arguments": {
    "exact": "PostgreSQL",
    "path": "projects/alpha"
  }
}
```

### Response

```markdown
Found 3 memories:

## 1. Authentication Design
**Slug**: `auth-design` | **Match**: title | **Updated**: 2024-01-15

**Tags**: auth, security

We use JWT tokens for API authentication...

---

## 2. API Security Guidelines
**Slug**: `api-security` | **Match**: content | **Updated**: 2024-01-10

References to authentication patterns...

---
```

### Internal Orchestration

When you call `mimir_recall`, it internally:
1. Searches database by title and tags
2. Falls back to git grep for raw text
3. Expands results via 1-hop associations
4. Filters superseded content (by default)
5. Ranks by relevance + recency
6. Returns full content with annotations

### Notes

- Use `topic` for semantic search
- Use `exact` when you know the specific text
- Use `list_all` to explore what's stored
- Superseded memories are hidden by default

---

## Tool: mimir_remember

**Intent**: "Store this for later" / "Update my understanding"

Store information in memory. Use for new information OR updating existing information. If updating, the old version is preserved in history.

### Input Schema

```json
{
  "title": "string (required) - Clear title",
  "content": "string (required) - Markdown content",
  "slug": "string (optional) - Custom ID or existing slug to update",
  "replaces": "string (optional) - Slug this supersedes",
  "tags": ["string", "..."] (optional),
  "path": "string (optional) - Folder path",
  "note": "string (optional) - Add annotation only"
}
```

### Example Requests

**Create new memory:**
```json
{
  "tool": "mimir_remember",
  "arguments": {
    "title": "Database Architecture Decision",
    "content": "# Decision\n\nWe chose PostgreSQL for...",
    "tags": ["database", "architecture", "decision"]
  }
}
```

**Update existing memory:**
```json
{
  "tool": "mimir_remember",
  "arguments": {
    "title": "Database Architecture Decision",
    "content": "# Updated Decision\n\nAfter testing...",
    "slug": "database-architecture-decision"
  }
}
```

**Supersede old memory:**
```json
{
  "tool": "mimir_remember",
  "arguments": {
    "title": "New Database Choice",
    "content": "# Decision\n\nSwitched to CockroachDB...",
    "slug": "db-choice-v2",
    "replaces": "db-choice-v1"
  }
}
```

**Add annotation (correction):**
```json
{
  "tool": "mimir_remember",
  "arguments": {
    "title": "Database Architecture Decision",
    "content": "...",
    "slug": "database-decision",
    "note": "This was later found to be incorrect - see db-choice-v2"
  }
}
```

### Response

```
Memory created: Database Architecture Decision
Slug: database-architecture-decision
Path: projects/decisions/database-architecture-decision.md

Supersedes: 'db-choice-v1' (marked as outdated)
```

### Internal Orchestration

When you call `mimir_remember`, it internally:
1. Creates or updates memory file
2. Generates slug if not provided
3. Determines path from tags/category
4. Commits to git
5. Updates database index
6. Handles supersession (marks old, creates association)
7. Creates annotation if `note` provided

### Notes

- Provide `slug` of existing memory to update
- Use `replaces` to formally supersede old information
- Old versions preserved in git history
- Annotations don't change content, just add metadata

---

## Tool: mimir_history

**Intent**: "When did I learn about X?" / "What changed?"

Answer questions about when things happened and how they changed.

### Input Schema

```json
{
  "slug": "string (optional) - Memory to get history for",
  "topic": "string (optional) - Find history by topic",
  "show_changes": "boolean (optional) - Include diffs",
  "since": "string (optional) - Date filter",
  "limit": "number (optional, default 10)"
}
```

### Date Formats

- ISO 8601: `2024-01-15T00:00:00Z`
- Date only: `2024-01-15`
- Relative: `7d`, `1w`, `1m` (days, weeks, months)

### Example Requests

**History for specific memory:**
```json
{
  "tool": "mimir_history",
  "arguments": {
    "slug": "auth-decision",
    "show_changes": true
  }
}
```

**Recent activity:**
```json
{
  "tool": "mimir_history",
  "arguments": {
    "since": "7d",
    "limit": 20
  }
}
```

### Response

```markdown
# History for 'Authentication Decision'

**Slug**: `auth-decision`
**Created**: 2024-01-15 10:30
**Last Updated**: 2024-01-20 14:00
**Access Count**: 15

## Commit History

### 1. 2024-01-20 14:00
**Commit**: `abc12345`
**Message**: update: Modify memory 'auth-decision'

**Changes**:
```diff
- We use basic auth
+ We switched to OAuth2
```

### 2. 2024-01-15 10:30
**Commit**: `def67890`
**Message**: feat: Create memory 'auth-decision'
```

### Notes

- Shows git commit history for the memory
- `show_changes` includes actual diffs
- Useful for understanding how decisions evolved

---

## Tool: mimir_connect

**Intent**: "These are related" / "Unlink these"

Link or unlink related memories. Creates connections in the knowledge graph.

### Input Schema

```json
{
  "from": "string (required) - First memory slug",
  "to": "string (required) - Second memory slug",
  "disconnect": "boolean (optional) - Remove instead of create",
  "relationship": "string (optional) - Type of connection",
  "strength": "number (optional, 0.0-1.0, default 0.5)"
}
```

### Relationship Types

| Type | Use For |
|------|---------|
| `related` | General relationship |
| `references` | One cites/mentions the other |
| `follows` | Sequential (A comes after B) |
| `supersedes` | A replaces B (marks B as outdated) |
| `part_of` | A is part of B |
| `project` | Related project |
| `person` | Related to a person |

### Example Requests

**Connect memories:**
```json
{
  "tool": "mimir_connect",
  "arguments": {
    "from": "project-alpha",
    "to": "budget-doc",
    "relationship": "references"
  }
}
```

**Disconnect:**
```json
{
  "tool": "mimir_connect",
  "arguments": {
    "from": "memory-a",
    "to": "memory-b",
    "disconnect": true
  }
}
```

**Supersede (auto-marks old as outdated):**
```json
{
  "tool": "mimir_connect",
  "arguments": {
    "from": "new-decision",
    "to": "old-decision",
    "relationship": "supersedes"
  }
}
```

### Response

```
Connected: 'project-alpha' -references-> 'budget-doc' (strength: 0.50)
```

### Notes

- `supersedes` relationship automatically marks target as superseded
- Bidirectional by default for most relationship types
- Directional for `follows`, `precedes`, `supersedes`, `part_of`

---

## Tool: mimir_forget

**Intent**: "No longer relevant"

Archive a memory that's no longer relevant. Not deleted - moved to archive for potential restoration.

### Input Schema

```json
{
  "slug": "string (required) - Memory to archive"
}
```

### Example Request

```json
{
  "tool": "mimir_forget",
  "arguments": {
    "slug": "outdated-decision"
  }
}
```

### Response

```
Memory 'outdated-decision' archived (can be restored later)
```

### Notes

- Soft delete - moves to `archive/` directory
- Git history preserved
- Can be restored with `mimir_restore`
- Use `mimir_recall` with `include_archived: true` to find archived memories

---

## Tool: mimir_restore

**Intent**: "Bring back that archived memory"

Restore an archived memory to active status.

### Input Schema

```json
{
  "slug": "string (required) - Archived memory to restore"
}
```

### Example Request

```json
{
  "tool": "mimir_restore",
  "arguments": {
    "slug": "archived-decision"
  }
}
```

### Response

```
Memory 'archived-decision' restored to: projects/decisions/archived-decision.md
```

### Notes

- Use `mimir_recall` with `include_archived: true` to find archived slugs
- Restores to original location (or default path if structure changed)

---

## Tool: mimir_sync

**Intent**: "Sync with remote"

Manually trigger git synchronization to remote repository.

### Input Schema

```json
{
  "force": "boolean (optional, default false)"
}
```

### Example Request

```json
{
  "tool": "mimir_sync",
  "arguments": {
    "force": false
  }
}
```

### Response

```
Sync completed successfully

Status:
- Last sync: 2024-01-15T14:30:00Z
- Successful: true
```

### Notes

- Requires PAT token configured
- `force: true` applies last-write-wins for conflicts
- `force: false` fails on conflicts (manual resolution needed)
- Usually not needed - auto-sync handles most cases

---

## Example Workflows

### Complete Knowledge Management Flow

```javascript
// 1. Store a decision
await mcp.callTool("mimir_remember", {
  title: "API Architecture Decision",
  content: "# Decision\n\nWe chose REST over GraphQL because...",
  tags: ["api", "architecture", "decision"],
  slug: "api-arch-v1"
});

// 2. Later, find what you decided
const result = await mcp.callTool("mimir_recall", {
  topic: "API architecture"
});

// 3. Understanding evolved - supersede old decision
await mcp.callTool("mimir_remember", {
  title: "Updated API Architecture",
  content: "# Revised Decision\n\nAfter performance testing, we switched to GraphQL...",
  slug: "api-arch-v2",
  replaces: "api-arch-v1"
});

// 4. Connect related decisions
await mcp.callTool("mimir_connect", {
  from: "api-arch-v2",
  to: "performance-report",
  relationship: "references"
});

// 5. Check history of changes
await mcp.callTool("mimir_history", {
  topic: "API architecture",
  show_changes: true
});
```

### Quick Lookup Pattern

```javascript
// The simplest pattern - just ask what you know
const context = await mcp.callTool("mimir_recall", {
  topic: "user's current question or topic"
});
// Returns relevant memories with full content
```

---

## Error Handling

All tools return errors in this format:

```json
{
  "isError": true,
  "content": [
    {
      "type": "text",
      "text": "Error message here"
    }
  ]
}
```

### Common Errors

| Error | Cause |
|-------|-------|
| `memory not found: slug` | No memory with that slug |
| `memory is already archived` | Trying to archive twice |
| `memory is not archived` | Trying to restore non-archived |
| `no connection found` | Trying to disconnect unlinked memories |
| `invalid relationship type` | Unknown relationship |

---

## Migration from Old Tools

If you were using the previous 9-tool API:

| Old Tool | New Tool | Notes |
|----------|----------|-------|
| `mimir_write` | `mimir_remember` | Same but with `replaces` support |
| `mimir_read` | `mimir_recall` | Search by slug with `exact` |
| `mimir_update` | `mimir_remember` | Provide `slug` to update |
| `mimir_search` | `mimir_recall` | Use `topic` or `exact` |
| `mimir_list` | `mimir_recall` | Use `list_all: true` |
| `mimir_delete` | `mimir_forget` | Same behavior |
| `mimir_associate` | `mimir_connect` | Simplified parameters |
| `mimir_graph` | `mimir_recall` | Graph expansion built-in |
| `mimir_sync` | `mimir_sync` | Unchanged |

The new tools consolidate multiple operations into intent-based commands.
