Medha - MCP Memory Tools
---
description: Proactively manage information using Medha MCP server
alwaysApply: true
---

# Medha Memory Protocol

Proactively store, retrieve, and manage information via the `medha` MCP server.

## Decision Tree

1. User asks about something → `medha_recall` first, include context if found
2. Decision/solution/insight emerges → `medha_remember` it (with `connections` and `tags`)
3. Updated info replaces old → `medha_remember` with `replaces` param (don't duplicate)
4. User asks about timeline/changes → `medha_history`
5. Two things are related → `medha_connect`
6. Something is obsolete → `medha_forget` (soft delete, restorable via `medha_restore`)

## Tools Reference

**medha_recall** — Find information
- `topic`, `exact`, `list_all`, `path`, `include_archived`, `include_superseded`, `limit`

**medha_remember** — Create or update memories
- Required: `title`, `content`
- Optional: `slug` (ID; update if exists), `replaces` (supersede old slug), `tags`, `path`, `note`, `connections`
- `connections`: `[{"to": "slug", "relationship": "related|references|follows|supersedes|part_of|person|project", "strength": 0.5}]`

**medha_history** — View changes over time
- `slug`, `topic`, `show_changes`, `since` (`7d`/`1w`/`1m`/ISO date), `limit`

**medha_connect** — Link/unlink memories
- Required: `from`, `to`
- Optional: `relationship` (default `related`), `strength` (0.0–1.0), `disconnect`

**medha_forget** / **medha_restore** — Archive / unarchive by `slug`

**medha_sync** — Manual git push/pull (`force`)

## Guidelines

- **Recall before answering** — always check for existing context
- **Supersede, don't duplicate** — use `replaces` when updating
- **Connect while storing** — use `connections` param in `medha_remember`
- **Be selective** — store decisions, solutions, insights, action items; skip trivial info
- **Never store** credentials or secrets
