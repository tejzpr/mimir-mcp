Mimir - Complete MCP Tools Guide
---
description: Proactively manage information using Mimir MCP server
alwaysApply: true
---

# Mimir Auto-Management Protocol

When having conversations with the user, proactively identify opportunities to store, retrieve, and manage important information using the `mimir` MCP server.

## Available Tools Overview

Mimir provides 7 **human-aligned tools** that express intent rather than implementation. Each tool has a clear purpose:

| Tool | Intent | When to Use |
|------|--------|-------------|
| **mimir_recall** | "What do I know about X?" | Finding any information |
| **mimir_remember** | "Store this for later" | Creating or updating memories |
| **mimir_history** | "When did I learn about X?" | Understanding changes over time |
| **mimir_connect** | "These are related" | Linking memories together |
| **mimir_forget** | "No longer relevant" | Archiving outdated info |
| **mimir_restore** | "Bring back that archived memory" | Undeleting |
| **mimir_sync** | "Sync with remote" | Git synchronization |

---

## 1. Finding Information (mimir_recall)

**Intent**: "What do I know about X?"

This is the **primary retrieval tool** - use it whenever you need to find information. It searches everything: titles, content, tags, and associations. Returns full content, ranked by relevance.

### When to Use
- User asks about something that might be stored
- Need context from previous conversations
- Looking for related information
- Browsing what's available

### Usage
```json
{
  "topic": "What you want to know about",
  "exact": "Literal text to find",
  "list_all": true,
  "path": "projects/alpha",
  "include_superseded": false,
  "include_archived": false,
  "limit": 10
}
```

**All parameters optional** (but provide at least one of: topic, exact, or list_all)

### Search Strategies
- Use `topic` for semantic/keyword search: "authentication approach", "database decisions"
- Use `exact` when you know specific text exists
- Use `list_all: true` to browse everything
- Use `path` to scope to a folder
- Superseded memories are hidden by default (old versions replaced by new)

### Example
```
User: "What did we decide about the API?"
Assistant: [Uses mimir_recall with topic="API decision"]
```

---

## 2. Storing Information (mimir_remember)

**Intent**: "Store this for later" / "Update my understanding"

Use for creating new memories OR updating existing ones. If this information replaces/supersedes something old, specify `replaces` to properly link them.

### When to Create
- After solving a problem or providing a solution
- When a decision is made
- After sharing important technical information
- When the user explicitly wants to remember something

### What to Store
- **Decisions & Solutions**: Architectural decisions, problem solutions, code fixes
- **Important Facts**: Key information, discoveries, or insights
- **Action Items**: TODOs, follow-ups, or tasks identified
- **Code Snippets**: Useful code examples or patterns
- **Meeting Notes**: Discussion summaries, key points

### Usage
```json
{
  "title": "Clear, descriptive title",
  "content": "Markdown-formatted content",
  "slug": "custom-id-or-existing-slug",
  "replaces": "slug-of-old-memory",
  "tags": ["relevant", "tags"],
  "path": "projects/alpha/decisions",
  "note": "Add annotation without changing content",
  "connections": [
    {"to": "related-memory-slug", "relationship": "part_of"},
    {"to": "person-slug", "relationship": "person"}
  ]
}
```

**Required**: title, content  
**Optional**: slug, replaces, tags, path, note, connections

### Key Patterns

**Create new:**
```json
{
  "title": "Database Architecture Decision",
  "content": "# Decision\n\nWe chose PostgreSQL because...",
  "tags": ["database", "architecture", "decision"]
}
```

**Update existing (provide slug):**
```json
{
  "title": "Database Architecture Decision",
  "content": "# Updated Decision\n\nAfter testing...",
  "slug": "database-architecture-decision"
}
```

**Supersede old information:**
```json
{
  "title": "New Database Choice",
  "content": "# Decision\n\nSwitched to CockroachDB...",
  "slug": "db-choice-v2",
  "replaces": "db-choice-v1"
}
```

**Add correction/note:**
```json
{
  "title": "Database Architecture Decision",
  "content": "...",
  "slug": "database-decision",
  "note": "This was later found incorrect - see db-choice-v2"
}
```

**Create with connections (single call):**
```json
{
  "title": "Auth Bug Fix",
  "content": "# Bug Fix\n\nFixed token expiry issue...",
  "tags": ["bug", "auth"],
  "connections": [
    {"to": "project-alpha", "relationship": "part_of"},
    {"to": "john-smith", "relationship": "person"}
  ]
}
```

### Storage Guidelines
- **Be Selective**: Don't store trivial information
- **Add Context**: Include enough detail to be useful later
- **Use Good Titles**: Make memories easy to find
- **Use `replaces`**: When new info supersedes old, link them properly
- **Format Well**: Use markdown for readability

---

## 3. Understanding History (mimir_history)

**Intent**: "When did I learn about X?" / "What changed?"

Use when you need to understand when things happened or how they evolved.

### When to Use
- User asks when something was created or changed
- Need to understand evolution of a decision
- Looking at recent activity
- Debugging or auditing changes

### Usage
```json
{
  "slug": "memory-slug",
  "topic": "find by topic if slug unknown",
  "show_changes": true,
  "since": "7d",
  "limit": 10
}
```

**All parameters optional**

### Date Formats for `since`
- ISO 8601: `2024-01-15T00:00:00Z`
- Date only: `2024-01-15`
- Relative: `7d` (days), `1w` (weeks), `1m` (months)

### Example
```
User: "When did we change the authentication approach?"
Assistant: [Uses mimir_history with topic="authentication"]
```

---

## 4. Linking Memories (mimir_connect)

**Intent**: "These are related" / "Unlink these"

Creates connections in the knowledge graph so related information can be found together.

### When to Use
- Connecting related concepts
- Linking a decision to its implementation
- Marking that one memory supersedes another
- Building knowledge structure

### Usage
```json
{
  "from": "first-memory-slug",
  "to": "second-memory-slug",
  "relationship": "related",
  "strength": 0.5,
  "disconnect": false
}
```

**Required**: from, to  
**Optional**: relationship, strength, disconnect

### Relationship Types
| Type | Use For |
|------|---------|
| `related` | General relationship (default) |
| `references` | One cites/mentions the other |
| `follows` | Sequential (A comes after B) |
| `supersedes` | A replaces B (auto-marks B as outdated) |
| `part_of` | A is part of B |
| `person` | Related to a person/contact |
| `project` | Related to a project |

### Example
```
User: "This bug fix relates to the security discussion"
Assistant: [Uses mimir_connect from="bug-fix" to="security-discussion" relationship="references"]
```

---

## 5. Archiving Memories (mimir_forget)

**Intent**: "No longer relevant"

Archives a memory that's outdated. Not deleted - can be restored later.

### When to Use
- Information is obsolete
- User requests removal
- Cleaning up outdated decisions

### Usage
```json
{
  "slug": "memory-to-archive"
}
```

**Required**: slug

### Important Notes
- This is a **soft delete** - moves to archive
- Git history is preserved
- Can be restored with `mimir_restore`
- Use sparingly - superseding with `mimir_remember` is often better

---

## 6. Restoring Memories (mimir_restore)

**Intent**: "Bring back that archived memory"

Restores an archived memory to active status.

### When to Use
- User wants to recover archived information
- Accidentally archived something
- Need historical reference back in active memory

### Usage
```json
{
  "slug": "archived-memory-slug"
}
```

**Required**: slug

### Finding Archived Memories
Use `mimir_recall` with `include_archived: true` to find archived slugs.

---

## 7. Syncing Repository (mimir_sync)

**Intent**: "Sync with remote"

Manually trigger git synchronization to remote repository.

### When to Use
- After important changes to ensure backup
- Resolving sync conflicts
- Usually not needed (auto-syncs on writes)

### Usage
```json
{
  "force": false
}
```

**Optional**: force (default false)

---

## Proactive Interaction Patterns

### Pattern 1: Recall Before Answering
```
1. User asks a question → First check mimir_recall for context
2. If found → Include relevant memories in response
3. If not found → Answer and offer to store
```

### Pattern 2: Store and Connect
```
1. User solves a problem → Store with mimir_remember
2. Use "connections" param to link in same call (preferred)
   OR use mimir_connect afterward
3. Inform: "I've stored this and linked it to [related topic]"
```

### Pattern 3: Supersede Don't Duplicate
```
1. User provides updated information → Use mimir_recall to find old version
2. If found → Use mimir_remember with "replaces" parameter
3. Old memory automatically marked as superseded
```

### Pattern 4: Contextual Retrieval
```
1. User asks about past work → mimir_recall with topic
2. Present relevant information with context
3. Offer: "Would you like me to update this with new insights?"
```

---

## Example Interactions

### Example 1: Finding and Using Context
```
User: "How should we handle authentication?"
Assistant: [Uses mimir_recall with topic="authentication"]
Assistant: "Based on our previous discussion, we decided to use JWT tokens.
           [Shows relevant memory]
           Has anything changed, or should I update this?"
```

### Example 2: Storing a Decision
```
User: "Let's use PostgreSQL for the main database"
Assistant: "Got it. I'll remember this decision."
[Uses mimir_remember with title="Database Choice", content="...", tags=["database", "decision"]]
Assistant: "Stored. I've tagged it under 'database' and 'decision' for easy retrieval."
```

### Example 3: Updating Understanding
```
User: "Actually, let's switch to CockroachDB instead"
Assistant: [Uses mimir_recall to find old database decision]
Assistant: "I'll update our database decision and mark the old one as superseded."
[Uses mimir_remember with replaces="old-db-decision"]
```

### Example 4: Exploring History
```
User: "What changes have we made to the API design?"
Assistant: [Uses mimir_history with topic="API design", show_changes=true]
Assistant: "Here's the evolution of our API design decisions..."
```

---

## Quick Reference

| Task | Tool | Key Parameters |
|------|------|----------------|
| Find information | mimir_recall | topic, exact, list_all |
| Store/update | mimir_remember | title, content, slug, replaces, connections |
| Check history | mimir_history | slug, topic, show_changes, since |
| Link memories | mimir_connect | from, to, relationship, disconnect |
| Archive | mimir_forget | slug |
| Restore | mimir_restore | slug |
| Git sync | mimir_sync | force |

---

## Best Practices Summary

1. **Recall First**: Check for existing memories before answering questions
2. **Be Proactive**: Offer to store valuable information automatically
3. **Supersede, Don't Duplicate**: Use `replaces` when updating understanding
4. **Connect While Storing**: Use `connections` param to link in a single call
5. **Tag Consistently**: Use clear, consistent tags for searchability
6. **Add Context**: Include enough detail for future understanding
7. **Let Old Info Age**: Superseded memories are hidden by default - that's intentional

---

Remember: The goal is to build a rich, interconnected knowledge base that helps the user recall and build upon past work seamlessly. The tools express **intent** - use them naturally based on what you're trying to accomplish.
