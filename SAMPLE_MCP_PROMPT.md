# Medha MCP Instructions

Git-backed AI memory. **Recall before answering, store after solving.**

## Tools

| Tool | Use |
|------|-----|
| `medha_recall` | Find info (`topic`, `exact`, `list_all`, `path`) |
| `medha_remember` | Create/update (`title`+`content` required; `slug`, `replaces`, `tags`, `path`, `note`, `connections` optional) |
| `medha_history` | Timeline (`slug`/`topic`, `show_changes`, `since`: `7d`/`1w`/`1m`) |
| `medha_connect` | Link (`from`+`to` required; `relationship`, `strength`, `disconnect`) |
| `medha_forget` | Archive by `slug` (soft delete, restorable) |
| `medha_restore` | Unarchive by `slug` |
| `medha_sync` | Git push/pull (`force`) |

## Rules

1. **Recall first** — check `medha_recall` before answering
2. **Supersede, don't duplicate** — use `replaces` param
3. **Connect while storing** — use `connections`: `[{"to": "slug", "relationship": "related|references|follows|supersedes|part_of|person|project"}]`
4. **Never store** credentials/secrets
