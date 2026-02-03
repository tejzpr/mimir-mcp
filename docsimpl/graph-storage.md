# Graph Storage Patterns in Mimir

## Overview

Mimir implements a hybrid graph storage system that combines the benefits of git-based text storage with database-indexed graph queries. This document explains the graph data model, storage patterns, retrieval strategies, and performance characteristics.

## Graph Data Model

### Nodes (Memories)
Each memory is a node in the knowledge graph with these properties:

- **Identity**: Unique human-readable slug
- **Attributes**: Title, content, tags, timestamps
- **Location**: File path in git repository
- **State**: Active or archived (soft-deleted)

### Edges (Associations)
Associations represent relationships between memories:

- **Source**: Origin memory slug
- **Target**: Destination memory slug
- **Type**: Relationship semantic (related_project, person, follows, etc.)
- **Strength**: Weight from 0.0 to 1.0 (confidence/importance)
- **Direction**: Can be unidirectional or bidirectional

### Association Types

| Type | Description | Example |
|------|-------------|---------|
| `related_project` | Links to a related project | Meeting → Project Plan |
| `person` | Links to a person/contact | Project → Team Member |
| `follows` | Sequential relationship | Chapter 1 → Chapter 2 |
| `precedes` | Inverse of follows | Chapter 2 ← Chapter 1 |
| `references` | Citation or reference | Paper → Source Material |
| `related_to` | General association | Idea A ↔ Idea B |

## Storage Architecture

### Hybrid Storage Model

```
┌─────────────────────────────────────────────────────────┐
│              Memory with Associations                   │
└───────────┬────────────────────────┬────────────────────┘
            │                        │
            ▼                        ▼
   ┌────────────────┐      ┌────────────────────┐
   │  Git Storage   │      │  Database Storage  │
   │  (Source of    │      │  (Query Index)     │
   │   Truth)       │      │                    │
   └────────────────┘      └────────────────────┘
```

### 1. Git-Based Storage (Primary)

Associations are stored in markdown frontmatter:

```markdown
---
id: project-alpha-2024-01-15
title: "Project Alpha Planning"
tags: [project, planning]
created: 2024-01-15T10:00:00Z
updated: 2024-01-15T14:00:00Z
associations:
  - target: contact-john-doe
    type: person
    strength: 1.0
  - target: project-beta-2023-12-01
    type: related_project
    strength: 0.7
---

# Project Alpha Planning

## Overview
...
```

**Advantages**:
- Version controlled (every association change is a git commit)
- Human-readable and editable
- Portable (standard markdown format)
- Time-travel (can view associations at any point in history)
- Audit trail (git log shows association evolution)

**Trade-offs**:
- Slower for complex graph queries
- Requires file parsing for traversal
- No built-in graph algorithms

### 2. Database Storage (Index)

Associations are duplicated in `MIMIR_MEMORY_ASSOCIATIONS` table:

```sql
CREATE TABLE mimir_memory_associations (
    id SERIAL PRIMARY KEY,
    source_memory_id INTEGER NOT NULL,
    target_memory_id INTEGER NOT NULL,
    association_type VARCHAR(50) NOT NULL,
    strength FLOAT DEFAULT 0.5,
    metadata TEXT,
    created_at TIMESTAMP,
    FOREIGN KEY (source_memory_id) REFERENCES mimir_memories(id),
    FOREIGN KEY (target_memory_id) REFERENCES mimir_memories(id)
);

CREATE INDEX idx_associations_source ON mimir_memory_associations(source_memory_id);
CREATE INDEX idx_associations_target ON mimir_memory_associations(target_memory_id);
CREATE INDEX idx_associations_type ON mimir_memory_associations(association_type);
```

**Advantages**:
- Fast graph queries (indexed)
- SQL-based traversal
- Aggregation queries (count connections, find hubs)
- Filtering by type, strength, date

**Trade-offs**:
- Data duplication (also in git)
- Must stay synchronized with git
- No version history (only current state)

## Graph Traversal Algorithms

### Breadth-First Search (BFS)

**Use Case**: Find all memories within N degrees of separation

**Algorithm**:
```
1. Start with source memory
2. Add to queue
3. While queue not empty and depth < maxHops:
   a. Dequeue memory
   b. Get all associations (outgoing + incoming)
   c. For each neighbor not visited:
      - Mark as visited
      - Add to graph
      - Enqueue with depth+1
4. Return graph
```

**Example**: "Show all memories within 2 hops of Project Alpha"

```
Depth 0: Project Alpha
Depth 1: John Doe (person), Budget Doc (related_project)
Depth 2: Jane Smith (linked to John), Q1 Goals (linked to Budget)
```

**Performance**: O(V + E) where V = vertices, E = edges

### Depth-First Search (DFS)

**Use Case**: Explore deep chains of associations

**Algorithm**:
```
1. Start with source memory
2. Mark as visited
3. For each unvisited neighbor:
   a. Recursively traverse neighbor
   b. Add to graph
4. Return when maxHops reached or no unvisited neighbors
```

**Example**: "Follow the chain of 'follows' associations"

```
Chapter 1 → Chapter 2 → Chapter 3 → Chapter 4 → Chapter 5
```

**Performance**: O(V + E), but explores deeper before wider

## Query Patterns

### 1. Direct Association Query

**SQL**:
```sql
SELECT * FROM mimir_memory_associations
WHERE source_memory_id = ?
```

**Use Case**: "What is this memory linked to?"

**Performance**: O(1) with index lookup

### 2. Bidirectional Query

**SQL**:
```sql
SELECT * FROM mimir_memory_associations
WHERE source_memory_id = ? OR target_memory_id = ?
```

**Use Case**: "All associations involving this memory"

**Performance**: O(1) with two index lookups

### 3. Type-Filtered Query

**SQL**:
```sql
SELECT * FROM mimir_memory_associations
WHERE source_memory_id = ? AND association_type = 'person'
```

**Use Case**: "Show me all people associated with this project"

**Performance**: O(1) with composite index

### 4. Multi-Hop Traversal

**Recursive SQL** (PostgreSQL):
```sql
WITH RECURSIVE memory_graph AS (
    -- Base case
    SELECT id, slug, 0 as depth
    FROM mimir_memories
    WHERE id = ?
    
    UNION ALL
    
    -- Recursive case
    SELECT m.id, m.slug, mg.depth + 1
    FROM mimir_memories m
    JOIN mimir_memory_associations a ON
        a.source_memory_id = mg.id OR a.target_memory_id = mg.id
    JOIN memory_graph mg ON
        (m.id = a.target_memory_id OR m.id = a.source_memory_id)
    WHERE mg.depth < 5
)
SELECT DISTINCT * FROM memory_graph;
```

**Use Case**: Graph traversal in database

**Performance**: O(N^depth) worst case, but pruned by visited tracking

### 5. Find Hubs (Most Connected)

**SQL**:
```sql
SELECT m.slug, m.title, COUNT(a.id) as connection_count
FROM mimir_memories m
LEFT JOIN mimir_memory_associations a ON
    a.source_memory_id = m.id OR a.target_memory_id = m.id
GROUP BY m.id
ORDER BY connection_count DESC
LIMIT 10;
```

**Use Case**: "Most connected memories (central concepts)"

**Performance**: O(N * M) where N = memories, M = avg associations

## Storage Optimization

### Indexing Strategy

**Primary Indexes**:
- `mimir_memories.slug` (UNIQUE) - O(1) lookup by slug
- `mimir_memory_associations.source_memory_id` - Fast outgoing query
- `mimir_memory_associations.target_memory_id` - Fast incoming query

**Composite Indexes**:
- `(source_memory_id, association_type)` - Type-filtered queries
- `(target_memory_id, association_type)` - Reverse type-filtered queries
- `(user_id, created_at)` - User timeline queries
- `(user_id, updated_at)` - Recent updates queries

### Caching Strategies

**Memory-Level Caching**:
- Cache frequently accessed memories in memory
- TTL-based invalidation
- Warm cache on server startup

**Graph-Level Caching**:
- Cache computed graph traversals
- Invalidate on association changes
- Key: `graph:{slug}:{hops}:{traversal_type}`

## Comparison with Traditional Graph Databases

### Mimir (Hybrid) vs Neo4j/Neptune

| Feature | Mimir | Neo4j/Neptune |
|---------|---------------|---------------|
| **Storage** | Git + SQL | Native graph |
| **Version Control** | Full (git) | Limited/None |
| **Query Performance** | Good (<1000 nodes) | Excellent (millions) |
| **Audit Trail** | Complete (git history) | Application-level |
| **Human Readable** | Yes (markdown files) | No (binary) |
| **Portability** | Excellent | Database-specific |
| **Setup Complexity** | Low | Medium-High |
| **Cost** | Free (self-hosted) | $$ (managed) |
| **Best For** | Personal/team memory | Large-scale graphs |

## Advanced Patterns

### Pattern 1: Temporal Chains

Sequential memories linked with `follows`/`precedes`:

```
Meeting 1 → Meeting 2 → Meeting 3 → Meeting 4
  (Jan)      (Feb)       (Mar)       (Apr)
```

**Query**: "Show meeting history in order"

### Pattern 2: Hub-and-Spoke

Central concept with many related memories:

```
         Person A ───┐
         Person B ───┤
                     │
         Project X ──┼── Central Meeting
                     │
         Budget Doc ─┤
         Timeline ───┘
```

**Query**: "Show all aspects of this meeting"

### Pattern 3: Hierarchical

Parent-child relationships:

```
Project Root
├── Phase 1
│   ├── Task 1.1
│   └── Task 1.2
└── Phase 2
    ├── Task 2.1
    └── Task 2.2
```

**Query**: "Show project breakdown"

### Pattern 4: Semantic Network

Concept maps:

```
AI ←[related_to]→ Machine Learning
                      ↓
                  [related_to]
                      ↓
                  Deep Learning
                      ↓
                  [related_to]
                      ↓
              Neural Networks
```

**Query**: "Explore AI concepts"

## Performance Characteristics

### Memory Operations

| Operation | Time Complexity | Notes |
|-----------|-----------------|-------|
| Write | O(1) | File write + DB insert |
| Read by slug | O(1) | Index lookup + file read |
| Update | O(1) | File rewrite + DB update |
| Delete (soft) | O(1) | File move + DB update |
| Search by tag | O(N) | N = memories with tag |
| Search by date | O(log N) | Indexed query |

### Graph Operations

| Operation | Time Complexity | Notes |
|-----------|-----------------|-------|
| Add association | O(1) | File update + DB insert |
| Get direct links | O(1) | Indexed query |
| BFS traversal (N hops) | O(b^N) | b = avg branches per node |
| DFS traversal (N hops) | O(b^N) | b = avg branches per node |
| Find shortest path | O(E + V) | Not yet implemented |
| Detect cycles | O(V) | During traversal |

### Scalability Limits

**With SQLite**:
- Memories: ~100,000 comfortably
- Associations: ~1,000,000 comfortably
- Concurrent users: 1-10

**With PostgreSQL**:
- Memories: Millions
- Associations: Tens of millions
- Concurrent users: 100+

**Git Repository**:
- Files: Limited by filesystem (~1M files in ext4)
- Size: Limited by disk space
- Commits: Unlimited (git handles billions)

## Best Practices

### 1. Association Design

- **Use specific types**: Prefer `person` over `related_to`
- **Set meaningful strength**: 1.0 for strong, 0.3 for weak
- **Bidirectional by default**: Easier graph traversal
- **Avoid over-connection**: Only create meaningful links

### 2. Graph Queries

- **Limit hops**: Max 3-5 hops for performance
- **Filter by type**: Narrow down association types
- **Cache results**: For frequently accessed graphs
- **Use BFS for breadth**: Finding all related
- **Use DFS for chains**: Following sequences

### 3. Performance

- **Index heavily used fields**: Tags, dates, user_id
- **Batch operations**: Group git commits when possible
- **Lazy load content**: Load only when needed
- **Prune dead links**: Clean up associations to deleted memories

### 4. Version Control

- **Meaningful commits**: Describe what changed
- **Atomic operations**: One association change = one commit
- **Branch for experiments**: Test complex graph changes
- **Tag important states**: Mark milestones

## Example Use Cases

### Personal Knowledge Management

```
Research Topic
├── Paper 1 (references)
├── Paper 2 (references)
├── Notes on Paper 1 (related_to)
└── Summary (related_to)
```

### Project Tracking

```
Project Alpha
├── Meeting 1 (follows) → Meeting 2 (follows) → Meeting 3
├── Budget Document (related_project)
├── Team Member 1 (person)
└── Team Member 2 (person)
```

### Contact Management

```
Contact: John Doe
├── Meeting 2024-01-15 (person)
├── Email Thread (person)
├── Project Alpha (person)
└── Company: Acme Corp (related_to)
```

### Learning Path

```
Learn Go
├── Tutorial 1 (follows) → Tutorial 2 (follows) → Tutorial 3
├── Practice Project (related_to)
└── Reference Docs (references)
```

## Implementation Details

### Synchronization Strategy

**Write Path**:
```
1. Create/Update memory in git
2. Commit to git (source of truth)
3. Update database index (derived)
```

**Read Path**:
```
1. Query database for metadata + file path
2. Read file from git repository
3. Parse frontmatter for associations
4. Return combined data
```

**Consistency**:
- Git is always source of truth
- Database can be rebuilt from git if corrupted
- Eventual consistency model (database slightly delayed)

### Conflict Resolution

**Scenario**: Two users modify same memory simultaneously

**Resolution**:
1. Both commit locally
2. Sync attempts to push
3. First push succeeds
4. Second push detects conflict
5. Last-write-wins: Second user's changes overwrite
6. First user's changes preserved in git history
7. Can be recovered via `git log` and `git checkout`

### Graph Traversal Example

**Query**: "Find all memories within 2 hops of 'project-alpha'"

**Execution**:
```sql
-- Hop 1: Direct associations
SELECT target_memory_id, association_type, strength
FROM mimir_memory_associations
WHERE source_memory_id = (
    SELECT id FROM mimir_memories WHERE slug = 'project-alpha'
);

-- Results: [john-doe (person), budget-doc (related_project)]

-- Hop 2: Associations of john-doe
SELECT target_memory_id, association_type, strength
FROM mimir_memory_associations
WHERE source_memory_id = (
    SELECT id FROM mimir_memories WHERE slug = 'john-doe'
);

-- Results: [jane-smith (person), meeting-notes (related_to)]

-- Final graph:
-- Depth 0: project-alpha
-- Depth 1: john-doe, budget-doc
-- Depth 2: jane-smith, meeting-notes
```

## Advanced Topics

### Cycle Detection

During traversal, track visited nodes to avoid infinite loops:

```go
visited := make(map[uint]bool)

func traverse(node uint, depth int) {
    if visited[node] {
        return // Cycle detected
    }
    visited[node] = true
    // Continue traversal...
}
```

### Weighted Paths

Find strongest connection path between two memories:

```
A --0.9--> B --0.8--> C     (path strength: 0.72)
A --0.5--> D --0.9--> C     (path strength: 0.45)

Choose first path (stronger connections)
```

### Graph Statistics

Compute interesting metrics:

- **Degree centrality**: Number of connections per memory
- **Betweenness centrality**: Memories that bridge clusters
- **Clustering coefficient**: How interconnected neighbors are
- **Connected components**: Separate sub-graphs

### Graph Visualization

Export graph to formats for visualization tools:

- **DOT format** (Graphviz)
- **JSON** (D3.js, Cytoscape.js)
- **GEXF** (Gephi)

## Migration from Other Systems

### From Obsidian

Obsidian uses `[[wikilinks]]` for connections:

```markdown
Related: [[Other Note]], [[Another Note]]
```

**Migration**:
1. Parse `[[...]]` syntax
2. Extract target note names
3. Create `references` associations
4. Convert to frontmatter format

### From Notion

Notion uses database relations:

**Migration**:
1. Export Notion database to CSV
2. Parse relation columns
3. Create associations with appropriate types
4. Import content as markdown

### From Roam Research

Roam uses bidirectional links `[[...]]`:

**Migration**:
1. Export Roam to markdown
2. Parse all `[[...]]` references
3. Create bidirectional associations
4. Maintain block references as associations

## Future Enhancements

### 1. Vector Embeddings

Add semantic similarity alongside explicit associations:

```
Memory A --[explicit: person]--> Memory B
         --[implicit: 0.85 similarity]--> Memory C
```

### 2. Auto-Association

Use LLM to suggest associations:

```
"Based on content analysis, this memory might be related to:
- project-beta (similarity: 0.82)
- contact-jane (mentioned in text)
"
```

### 3. Temporal Queries

Query graph at specific points in time:

```
"Show me the association graph as it was on 2024-01-01"
```

Uses git history to reconstruct past state.

### 4. Pattern Matching

Find common graph patterns:

```
"Find all memories with structure:
  Project → (person × 3+) → Budget Document"
```

## Conclusion

Mimir's hybrid graph storage provides:
- **Durability**: Git-based version control
- **Performance**: Database-indexed queries
- **Flexibility**: Human-readable formats
- **Scalability**: Suitable for personal to small team usage
- **Auditability**: Complete historical record

The system prioritizes data integrity and human-readability over raw query performance, making it ideal for LLM memory storage where version control and audit trails are critical.
