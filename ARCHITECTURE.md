# Precon — Architecture Quick Reference

Compact cheat-sheet of core interfaces and types. Read this before `sig`-ing the same files again.

## tier.Store (`internal/tier/tier.go`)

```go
type Store interface {
    Store(ctx context.Context, mem *Memory) error
    Retrieve(ctx context.Context, id string) (*Memory, error)
    Query(ctx context.Context, prompt string, limit int) ([]RetrievalResult, error)
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, conversationID string) ([]Memory, error)
    Level() Level
}
```

**Consumers**: retriever, persister, dreamer, pkg/precon

## tier.EvictionPolicy (`internal/tier/tier.go`)

```go
type EvictionPolicy interface {
    ShouldEvict(ctx context.Context, memories []Memory) []Memory
    ShouldPromote(ctx context.Context, retrieved []RetrievalResult) []Memory
}
```

## tier.Memory (`internal/tier/tier.go`)

```go
type Memory struct {
    ID, ConversationID, TopicID, Content string
    IsSummary        bool
    FullContentRef   string
    TokenCount       int
    Relevance        float64
    Tier             Level        // L1..L5
    Keywords         []string
    CreatedAt        time.Time
    LastAccessedAt   time.Time
    PromotedFrom     Level
}
```

## persister.LLM (`internal/persister/persister.go`)

```go
type LLM interface {
    Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
    CompleteWithTools(ctx context.Context, req *api.Request) (*api.Response, error)
}
```

**Note**: `synthesizer.LLM` and `dreamer.LLM` are separate interfaces with `Complete` only.

## persister.L2Writer (`internal/persister/persister.go`)

```go
type L2Writer interface {
    tier.Store
    UpdateRelevance(ctx context.Context, id string, score float64) error
}
```

## api.Client key methods (`internal/api/client.go`)

- `SendMessage(ctx, *Request) (*Response, error)` — synchronous
- `StreamMessage(ctx, *Request) (*StreamReader, error)` — SSE streaming
- `CountTokens(ctx, *Request) (int, error)` — token counting
- `ListModels(ctx) ([]ModelInfo, error)` — model discovery

## Model constants (`internal/api/types.go`)

```
ModelOpus45Latest    = "claude-opus-4-5"
ModelSonnet45Latest  = "claude-sonnet-4-5"
ModelSonnet4         = "claude-sonnet-4-20250514"
ModelHaiku45Latest   = "claude-haiku-4-5"
ModelHaiku35Latest   = "claude-3-5-haiku-latest"
```

## Config defaults (`pkg/precon/precon.go`)

```
MaxContextTokens = 200000
SynthesisBudget  = 4000
RetrieverLimit   = 10
WorkingModel     = "claude-opus-4-6"
FastModel        = "claude-haiku-4-5-20251001"
DreamModel       = "claude-sonnet-4-6"
PgConnStr        = "postgres://postgres:postgres@localhost:5431/postgres"
```

## Package layout

```
cmd/precon/           CLI entry (main.go, check.go, dream.go)
internal/api/         Anthropic HTTP client, streaming, retry, model resolver
internal/dreamer/     Cron-based cross-session analysis
internal/embedding/   RunPod serverless embedding client (nomic-embed-text, 768d)
internal/mcp/         MCP protocol integration
internal/persister/   Background persistence agent (Haiku + tool_use)
internal/repl/        REPL split: repl.go, send.go, commands.go, format.go, persist.go, prune.go
internal/retriever/   Pre-prompt FTS5 + semantic retrieval
internal/storage/     SQLite (L2/L3), pgvector (L4), eviction, promotion, summarizer
internal/synthesizer/ Context compression (Phase 6+ — Haiku compresses when over budget)
internal/tier/        Memory/Level/Store/EvictionPolicy types
internal/tools/       37 files — tool definitions, registry, executor, file walker, read tracker
internal/topic/       Topic detection via Haiku
pkg/precon/           Public Config type, DefaultConfig, LoadConfig
```
