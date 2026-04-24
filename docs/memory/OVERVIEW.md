# Living Memory

## Quick Example

```
User: "I prefer dark roast coffee, especially Ethiopian Yirgacheffe"

-> memory.SaveThought("I prefer dark roast coffee, especially Ethiopian Yirgacheffe",
                       source: "user", tags: ["preference", "coffee"])

-> memory.SaveKeyFact("Prefers dark roast coffee, especially Ethiopian Yirgacheffe",
                       category: "preference")

Later, agent retrieves:
-> memory.GetKeyFacts(category: "preference")
   => [{Fact: "Prefers dark roast coffee...", Category: "preference"}]
```

## Data Model

### Thoughts

Captured raw from conversations. Stored in `captured_thoughts` table.

```go
type Thought struct {
    ID        int64
    Content   string    // The raw thought
    Source    string    // "user" (default), "agent", "system"
    SessionID string   // Which conversation
    Tags      []string // JSON array: ["preference", "coffee"]
    CreatedAt string   // RFC3339
}
```

### Key Facts

Extracted, categorized knowledge. Stored in `key_facts` table. Fields: `ID`, `Fact`, `Category` (preference/contact/location/general), `SourceSession`, `ExtractedAt`.

## Operations

| Method                          | What it does                                     |
|---------------------------------|--------------------------------------------------|
| `SaveThought(content, source, session, tags)` | Store a captured thought           |
| `GetThoughts(query)`           | Retrieve with filters (tags, date range, LIKE search) |
| `ThoughtCount()`               | Total captured thoughts                          |
| `SaveKeyFact(fact, category, session)` | Store an extracted fact                   |
| `GetKeyFacts(category, limit)` | Retrieve facts, optionally by category           |

## Querying Thoughts

`ThoughtQuery` supports combining filters:

- **Tags**: OR match -- any matching tag returns the thought
- **Since/Until**: Date range (ISO format)
- **Contains**: LIKE search on content
- **Limit**: Default 50

Results ordered by `created_at DESC`.

## Storage

All data in local SQLite (encrypted via Adiantum VFS when passphrase is set). No external calls. No cloud sync.

## Source Files

- `internal/memory/memory.go` -- Store, SaveThought, GetThoughts, SaveKeyFact, GetKeyFacts
