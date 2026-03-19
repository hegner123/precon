# Precon Memory Model — Technical Deep-Dive

## Design Principle

Every component in precon serves one goal: **unconscious remembering**. Context from prior sessions is automatically retrieved, compressed, and injected into the agent's system prompt before every interaction. The agent never requests its memories. The user never manages them. Memory is the default state, not an action.

This is fundamentally different from giving the agent a memory tool (e.g., MCP `retrieve_memories()`). A tool requires the agent to *decide* to recall — conscious memory. Precon makes memory *systematic and automatic* — unconscious memory. The infrastructure handles recall so the agent's job is pure reasoning.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  L1 — Active Prompt Context                          │
│  (ephemeral — the assembled API request)             │
├──────────────────────────────────────────────────────┤
│  L2 — Hot Storage (SQLite + FTS5)                    │
│  500K token budget · keyword search · sub-ms queries │
├──────────────────────────────────────────────────────┤
│  L3 — Summaries (SQLite + FTS5)                      │
│  Compressed references to L4 originals               │
├──────────────────────────────────────────────────────┤
│  L4 — Semantic Storage (pgvector)                    │
│  768-dim embeddings · cosine similarity · unlimited  │
├──────────────────────────────────────────────────────┤
│  L5 — Cold Archive (future)                          │
│  Object storage for bulk historical data             │
└──────────────────────────────────────────────────────┘
```

Each tier serves the unconscious memory pipeline differently:

- **L1** is the injection target — the assembled prompt where retrieved context lands
- **L2** is the fast-access store — where recent memories live for instant keyword retrieval
- **L3** is the bridge — compressed summaries that keep evicted content discoverable by keyword
- **L4** is meaning-based long-term memory — finds relevant content even when the user's phrasing differs from stored phrasing
- **L5** (future) is cold archive — bulk storage for aged-out content

## Tier Details

### L1 — Active Prompt Context

The working agent's context window. L1 is not a persistent store — it's the assembled prompt sent to the API on each turn. The retriever and synthesizer populate L1 from lower tiers *before every request*. This is where unconscious injection happens.

**Budget**: ~200K tokens (model-dependent)

### L2 — Hot Storage

Primary persistence tier. Every turn writes to L2 immediately. The retriever looks here first.

**Implementation**: SQLite with FTS5 full-text search
**Budget**: 500,000 tokens (configurable via `Evictor.SetThreshold`)
**Target after eviction**: 80% of budget (400K tokens)

**What gets stored**: Each turn persists up to 4 categories as separate memories:
1. **User message** (relevance 1.0)
2. **Agent thinking** (relevance 0.8)
3. **Tool interactions** — one per tool call with name, input, output (relevance 0.9)
4. **Final response** (relevance 1.0)

Storing categories separately enables fine-grained unconscious retrieval — a future prompt about "that tool error" retrieves just the tool memory, not the entire turn.

### L3 — Summaries

Compressed pointers to L4 originals. When memories are evicted from L2, the summarizer generates a 2-3 sentence summary with extracted keywords. L3 entries are small enough to stay in SQLite while keeping evicted content discoverable via keyword search.

**Implementation**: SQLite with FTS5 (same DB as L2)
**Fallback**: On LLM failure, truncates first 200 characters and reuses existing keywords

L3 is what makes unconscious memory work across the eviction boundary — content that's been moved to L4 for space reasons is still findable by keyword through L3.

### L4 — Semantic Storage

Long-term memory with meaning-based retrieval. Memories evicted from L2 are embedded as 768-dimensional vectors and stored in pgvector for cosine similarity search.

**Implementation**: PostgreSQL + pgvector extension
**Embedding**: RunPod serverless endpoint running nomic-embed-text (768 dimensions)

L4 is what makes unconscious memory robust against paraphrasing. When the user says "that auth problem" and the stored memory says "authentication failure in the login handler," L4 finds the match through vector similarity.

L4 is unbounded by token budget — it grows with usage.

### L5 — Cold Archive (Future)

Planned tier for bulk historical data. Object storage (S3-compatible) for memories that have aged out of L4.

## The Memory Lifecycle

Every phase operates automatically. No user action, no agent action — just infrastructure.

```
User types a prompt
        │
        ▼
┌───────────────┐     ┌──────────────┐     ┌───────────────┐
│   Retriever   │────▶│  Synthesizer │────▶│  L1 Assembly  │
│ (automatic)   │     │ (automatic)  │     │  (API request) │
└───────┬───────┘     └──────────────┘     └───────┬───────┘
        │                                          │
  Queries L2/L3/L4                          Sent to Claude
  in parallel                                      │
                                                   ▼
                                          Agent Response
                                                   │
                                    ┌──────────────┴──────────────┐
                                    │                             │
                              ┌─────▼─────┐              ┌───────▼───────┐
                              │ Immediate │              │  Background   │
                              │ L2 Write  │              │   Persister   │
                              │ (raw turn)│              │ (Haiku review)│
                              └───────────┘              └───────┬───────┘
                                                                 │
                                                    Curates: save / score / skip
                                                                 │
                                                          Writes to L2
```

### Phase 1: Retrieval (Pre-Prompt) — Unconscious Recall

The **Retriever** fires on every user prompt. Not when the agent asks for it — on *every single prompt*, by default. It queries all configured tiers in parallel, collecting up to 10 results per tier. It casts a wide net intentionally — relevance filtering is the synthesizer's job.

```
Retriever.Retrieve(ctx, "how do I fix the eviction bug?")
  → L2: FTS5 keyword/content match
  → L3: FTS5 across summaries
  → L4: Embed prompt → cosine similarity
  → Returns []RetrievalResult (all tiers, sorted by score)
```

Failures in any single tier are logged and skipped — retrieval never blocks on a failed tier.

### Phase 2: Synthesis (Pre-Prompt) — Unconscious Compression

The **Synthesizer** takes raw retrieval results and compresses them into a concise context block that gets injected into the system prompt.

**Rules**:
- Extract only information relevant to the current prompt
- Preserve specifics: names, numbers, decisions, code snippets
- Drop conversational filler, greetings, meta-discussion
- Aggressive compression to a 4K token budget

This is what makes default injection practical. You can't inject raw history into every prompt — it would consume the context window. But 4K tokens of synthesized, relevant context carries more information density than 40K tokens of raw conversation.

If no retrieved content is relevant, the synthesizer outputs nothing — no wasted context.

### Phase 3: Immediate Persistence (Post-Response) — Unconscious Storage

After each turn, the REPL writes raw turn data to L2 immediately. Fast, synchronous, non-lossy. Each category (user, thinking, tools, response) becomes a separate memory with appropriate relevance scores.

Tool outputs are truncated at 4,000 characters — full output isn't worth persisting when the agent already consumed it in-turn.

### Phase 4: Background Curation (Post-Response, Async) — Unconscious Editing

The **Persister** runs in a background goroutine. It sends the turn to Haiku for structured analysis:

- **New topics**: Knowledge worth persisting with name, keywords, concise summary
- **Score updates**: Relevance adjustments for existing topics
- **What to skip**: Greetings, acknowledgments, repetitive output

The REPL is ready for the next input before Haiku finishes reviewing. Memory curation never blocks the conversation.

### Phase 5: Eviction (When L2 Overflows) — Unconscious Forgetting

When L2 exceeds its 500K token budget, the **Evictor** triggers:

1. **Score**: Each memory gets `relevance × recency_decay` (decay halves every 24 hours)
2. **Select**: Lowest-scoring memories collected until freed tokens reach target (80% capacity)
3. **Embed**: All candidates batch-embedded in a single API call
4. **Migrate**: Each memory stored in L4 (pgvector), deleted from L2
5. **Summarize**: L3 summary generated for each evicted memory

This is unconscious forgetting — not deletion, but migration. Low-relevance memories move to a tier where they're still findable by meaning (L4) and keyword (L3), but they stop consuming L2's fast-access budget.

### Phase 6: Promotion (On-Demand) — Unconscious Recall of Distant Memory

The **Promoter** handles reverse flow — bringing L4 memories back to L2 when they become relevant:

- **Promote**: Copy L4 memory to L2 with a reference back to the original
- **Demote**: Remove the L2 copy when it's no longer relevant

Old knowledge resurfaces when the conversation makes it relevant again.

### Phase 7: Dreaming (Cron-Scheduled) — Unconscious Consolidation

The **Dreamer** runs periodically across stored memories:

1. Loads memories within a configurable lookback window
2. Runs analysis prompts against the memory set
3. For large sets (>80K tokens), chunks and merges
4. Generates reports that are persisted as L2 memories

Dream reports become part of the unconscious context — the system builds meta-knowledge about its own patterns, and that meta-knowledge automatically surfaces in future retrievals.

## Key Design Decisions

### Why Separate Memories Per Category?
Fine-grained unconscious retrieval. A search related to tool errors returns just the tool memories, not unrelated user prompts. The system can surface the exact slice of a prior turn that's relevant.

### Why Two-Stage Persistence?
Immediate writes (raw L2) ensure no data loss. Background curation (Haiku review) ensures quality. If the background persister fails, the raw data is already safe. Belt and suspenders — unconscious memory can't afford gaps.

### Why Graceful Degradation Everywhere?
Unconscious processes must be invisible when they work and silent when they fail. A retriever timeout shouldn't produce an error dialog — it should produce slightly less context. The user should never know memory management is happening, whether it succeeds or fails.

### Why Token-Aware Budgeting?
Every wasted token shortens the conversation. Unconscious injection must be efficient enough that it *never* crowds out the user's actual work:

| Layer | Budget | Mechanism |
|-------|--------|-----------|
| L2 storage | 500K tokens | Eviction when exceeded |
| Synthesis | 4K tokens | LLM compression |
| Tool storage | 4K chars | Truncation |
| Dreamer chunks | 80K tokens | Chunked analysis |

## Configuration Defaults

| Parameter | Default | Source |
|-----------|---------|--------|
| L2 token budget | 500,000 | `storage.L2MaxTokens` |
| Eviction target | 80% | `storage.L2TargetPercent` |
| Synthesis budget | 4,000 tokens | `pkg/precon.SynthesisBudget` |
| Retriever limit | 10 per tier | `pkg/precon.RetrieverLimit` |
| Working model | claude-opus-4-5 | `pkg/precon.WorkingModel` |
| Fast model | claude-haiku-4-5 | `pkg/precon.FastModel` |
| Embedding dimensions | 768 | `embedding.Dimensions` |
| Decay half-life | 24 hours | `evictionScore()` |
| Persister retries | 2 | `persister.maxRetries` |
| Dreamer chunk budget | 80,000 tokens | `dreamer.maxChunkTokens` |
