# Mimir MCP Instructions

Git-backed AI memory system. **Recall before answering, store after solving.**

## Tools

| Tool | Intent |
|------|--------|
| `mimir_recall` | Find stored information (use `topic`, `exact`, or `list_all`) |
| `mimir_remember` | Create/update memories (requires `title`, `content`) |
| `mimir_history` | View changes over time |
| `mimir_connect` | Link related memories |
| `mimir_forget` | Archive outdated info |
| `mimir_restore` | Undelete archived memories |
| `mimir_sync` | Manual git sync |

## Key Behaviors

1. **Check first** - Use `mimir_recall` before answering questions
2. **Store valuable info** - Decisions, solutions, context, action items
3. **Supersede, don't duplicate** - Use `replaces` param when updating
4. **Connect while storing** - Use `connections` param to link memories

## Remember Params

- `title`, `content` (required)
- `tags`, `slug`, `path` (optional)
- `replaces`: slug of memory being superseded
- `connections`: `[{"to": "slug", "relationship": "type"}]`

## Relationship Types

`related` (default), `references`, `follows`, `supersedes`, `part_of`, `person`, `project`

## Don't Store

Credentials/secrets, or anything user doesn't want stored.
