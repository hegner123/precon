# Precon — Pre-Conscious Context Management

## Origin

Inspired by the RLM paper (Zhang, Kraska, Khattab — MIT CSAIL, Jan 2026) which proves
that compaction/summarization destroys information on dense tasks and proposes recursive
self-invocation as an alternative. Precon takes the same anti-compaction insight and
applies it to a different domain: long-running agentic conversations over time.

**RLM solves breadth** (processing massive inputs right now via recursive sub-queries).
**Precon solves depth** (maintaining coherent context over sessions/weeks/months/years via tiered memory).

Evolved from the [nostop](../nostop) project which proved the core concept: topic-based
archival with full message preservation in SQLite. Precon extends this with multi-tier
memory, pre-prompt retrieval agents, and background dreaming.

## Core Metaphor: Conscious / Unconscious / Dreaming

Not AGI. A system of recall inspired by how brains and bodies work.

### Unconscious (Autonomic)

Happens on every prompt. No LLM steering. Like a heartbeat or sensory computation —
you don't choose to get information input, your body just does it automatically.

- **Retriever**: Queries hot storage (SQLite FTS5) + semantic storage (pgvector) for
  topics relevant to the incoming user prompt. Pure DB query — no LLM call, no judgment,
  just exhaustive relevant results ranked by score.
- **Direct Injection** (Phases 3-5): Top-N retrieval results are formatted and injected
  directly into the system prompt. The working agent (Opus) handles relevance filtering.
- **Synthesizer** (Phase 6+): When L4 semantic search returns more content than fits in
  the context budget, a Haiku call compresses retrieval results before injection. Not used
  until retrieval volume exceeds the direct injection budget.
- **Background Persister**: Haiku reviews working agent output after each turn, determines
  what to save/trim, writes persistence. Will use tool_use with a JSON schema for structured
  output (not free-form JSON parsing) — the persister's `LLM` interface has been expanded
  beyond the original `Complete(ctx, systemPrompt, userPrompt) (string, error)` signature to
  include `CompleteWithTools(ctx, *api.Request) (*api.Response, error)` for tool definitions
  and structured responses (completed in Phase 5). Runs on the engine
  context, not the request context, so persistence survives request cancellation.
- **Tier Eviction**: Automatic promotion/demotion between memory tiers based on recency
  and relevance scoring.

### Conscious (Deliberate)

LLM-steered decisions. The thinking part.

- **Working Agent**: Receives user prompt + injected retrieval context + active context (L1).
  Does the actual work. Never touches the DB directly. Never manages its own memory.
- **Tool Use**: Working agent decides what tools to call.
- **Explicit Recall**: User or agent explicitly queries historical context.

### Dreaming (Periodic)

Cron-scheduled or passive agentic process. No user prompt required.

- Trims recent topics and organizes into persistence layers.
- Runs user-configurable analysis prompts across sessions.
  - "Look at what I was doing recently and compare to a week ago — recognize pain points
    or patterns in my workflow and generate a report."
- Dream reports become searchable entries in the memory hierarchy.
- Turns the memory multiplex from passive storage into an active knowledge base.

## Memory Multiplex (Tiered Storage)

Like a CPU cache hierarchy for knowledge:

```
Tier   Analogy       Storage         Access Pattern          Queried When
────   ───────       ───────         ──────────────          ────────────
L1     Registers     Prompt context  Direct (in the prompt)  Always (it IS the prompt)
L2     L1 Cache      SQLite + FTS5   Full-text search        Every prompt (retriever)
L3     L2 Cache      SQLite          Summary pointers        Retriever checks before L4
L4     RAM           pgvector        Semantic (embeddings)   Cache miss on L2/L3
L5     Disk          Deferred        Archive retrieval       Deferred until L1-L4 proven
```

### Tier Behavior

**L1 — Active Context**: The messages currently in the LLM's prompt. Bounded by model
context window. The working agent sees only this tier. Precon owns L1 directly since
it controls the REPL and API calls. Token counting (Anthropic count_tokens API, with character-based fallback) enforces
the context budget. When the token count exceeds (MaxContextTokens - SynthesisBudget -
8192 response reserve), prune messages from the middle of the conversation history,
preserving: (a) the system prompt, (b) the last 6 message pairs (user+assistant), and
(c) any message containing tool_use or tool_result blocks. Middle messages are removed
oldest-first. Pruned messages are written to L2 if not already persisted.

**L2 — Hot Storage**: Recent topics in SQLite with FTS5 full-text indexing. Full message
content. Queried on every prompt by the retriever using FTS5 `MATCH` queries against
the user prompt. When too many topics accrue, begin replacing with summaries and push
full content to L4.

**L3 — Warm Storage**: Summaries with pointers to full content in L4. Lightweight enough
to scan quickly. L3 is a special case in the Store interface: its `Query` implementation
internally dereferences pointers to L4 when a summary matches, so the retriever does not
need to orchestrate a two-phase lookup. L3 holds a reference to the L4 store at
construction time.

**L4 — Semantic Storage**: Full conversations stored in pgvector with embeddings
(nomic-embed-text via RunPod serverless, 768 dimensions). Semantic similarity search. This is
where "find me anything I've ever discussed that's similar to this task" lives.

**L5 — Cold Storage**: Deferred. Content does not leave L4 until L5 is implemented.
When implemented: files, cloud storage, or remote databases. Always retrievable through
explicit queries or another agent tunneling through layers.

### Eviction / Promotion Policy

**Eviction (hot → cold)**: Content moves away from L1 as it becomes less relevant to
the current work. Relevance scoring (from nostop) + temporal decay. Full content is
always preserved — only the *location* changes.

**Promotion (cold → hot)**: Follows a concrete state machine:

```
State: L4_ONLY
  Content exists only in L4 (semantic storage).
  Trigger: Retriever finds content relevant to current prompt.
  Action: Copy content to L2. Set L2 entry's `promoted_from = L4`, `l4_ref = <L4 ID>`.
  Next state: L2_PROMOTED

State: L2_PROMOTED
  Content exists in both L2 (hot) and L4 (original).
  The L2 copy is marked as promoted, not native.
  Trigger A: The persister's per-turn review finds the promoted content was not referenced
    or modified in the working agent's response. If the content was promoted more than 3
    turns ago and has not been accessed since promotion, delete the L2 copy.
    Action: Delete L2 copy. L4 original unchanged.
    Next state: L4_ONLY
  Trigger B: Working agent built on or modified the content.
    Action: Create new native L2 entry with the updated content. Delete promoted L2 copy.
    L4 original remains as historical record (immutable).
    Next state: L2_NATIVE (new entry) + L4_ONLY (old entry, unchanged)

State: L2_NATIVE
  Content was created directly in L2 (not promoted).
  Trigger: Total L2 token count exceeds L2_MAX_TOKENS (default: 500000, configurable).
  Content scores lowest on (relevance * recency_decay).
  Action: Generate L3 summary. Move full content to L4 with embeddings. Delete L2 entry.
  Repeat until L2 is at 80% of threshold.
  Next state: L3_SUMMARY + L4_ONLY
```

## Data Flow

```
User types prompt in REPL
    |
    v
[Unconscious] Retriever
    |-- Queries L2 (SQLite FTS5 - full-text match)
    |-- Queries L3 (SQLite - summary scan, dereferences to L4 internally)
    |-- Queries L4 (pgvector - semantic similarity)
    |-- Returns: ranked retrieval results
    v
[REPL] Builds API request
    |-- Formats top-N retrieval results into system prompt (direct injection)
    |-- OR (Phase 6+): Synthesizer compresses if results exceed context budget
    |-- L1 active messages + injected context + user prompt
    |-- Token counting: prune low-relevance L1 messages if near budget
    |-- Sends to Anthropic API, streams response to terminal
    v
[Conscious] Working Agent (Claude via API)
    |-- Sees: curated context (L1 + injected retrieval)
    |-- Does work: reasoning, tool use, response generation
    |-- Streams response back to REPL
    v
[Unconscious] Background Persister (Haiku, engine context)
    |-- Reviews: working agent's output + tool results
    |-- Uses tool_use with JSON schema for structured output
    |-- Decides: what to save, what to trim, relevance scores
    |-- Writes: new entries to L2, updates topic metadata
    |-- Triggers: tier eviction if L2 grows too large
    |-- Errors: logged via structured logger, never silently swallowed
    v
[Dreaming] Cron Process (periodic, separate invocation)
    |-- Scans: L2 recent topics (paginated, token-budgeted)
    |-- Analyzes: cross-session patterns
    |-- Generates: reports (stored as L2/L3 entries)
    |-- Organizes: promotes/demotes across tiers
```

## Interface: Styled REPL

Not a Bubbletea TUI. A standard REPL like Claude Code — readline input, streaming
output, styled with lipgloss and glamour for markdown rendering. Precon owns the
entire conversation loop, so all unconscious behavior happens natively inside the
send path.

**Why REPL over TUI**: Simpler. Precon owns L1 directly — it builds the API request,
controls what messages are included, injects retrieval context, prunes stale messages.
No hooks, no proxies, no fighting another tool's architecture.

**Why REPL over integrating with Claude Code**: Claude Code uses Max subscription auth.
Intercepting its API calls would require API pricing. The REPL uses its own API key
and owns the full pipeline.

### REPL Features

- Styled prompt with conversation/topic indicator
- Streaming response output with markdown rendering (glamour)
- Status indicators: print a dim-styled single line to stderr before the API call
  ("retrieving context...") and clear it when the streaming response begins. Use
  lipgloss Faint() style. No spinners or animation — keep compatible with piped output.
- `/dream` command to trigger dream analysis inline
- `/recall <query>` for explicit memory search
- `/tiers` to see memory tier stats
- `/topics` to see active/archived topics
- Conversation history with readline-style editing

## Differences from nostop

| Aspect | nostop | Precon |
|--------|--------|--------|
| Interface | Bubbletea TUI | Styled REPL |
| Storage tiers | 2 (active + archive) | 5 (L1-L5) |
| Retrieval | Manual (user picks from TUI) | Automatic (pre-prompt, FTS5 + pgvector) |
| Topic detection | Inline during send | Background (persister) |
| Archival trigger | Threshold (95% → 50%) | Continuous (background persister) |
| Semantic search | None | pgvector embeddings |
| Cross-session | SQLite archive only | Full multiplex + dreaming |
| Agent architecture | Single agent | Retriever + Worker + Persister (+ Synthesizer Phase 6+) |
| Context orchestration | LLM-driven (conscious) | System-driven (unconscious) |
| L1 ownership | Shared with user (TUI) | Fully owned by engine |

## What Carries Over from nostop

Each item classified by migration effort:

- **Full message preservation principle** ("archive, don't compact") — **Philosophy, no code**.
- **API client** (client.go, retry.go, stream.go, types.go, resolver.go) — **Copy and adapt**.
  Keep: retry logic, streaming SSE parser, model resolver, request/response types, CountTokens.
  Change: Remove TUI callback hooks in stream.go. Replace SendMessage signature to accept
  a `[]Message` slice (for L1 context injection) instead of nostop's conversation-based send.
  Add: Support for tool_use request/response types (needed by Phase 5 persister).
  Delete: Nothing from the copied files; unused code will be pruned after integration.
- **Topic detection via Haiku** (detector.go) — **Copy and adapt**. Keep: the detection prompt,
  JSON response parsing, TopicShift struct. Change: caller integration — runs inside the
  persister (background, engine context) instead of inline during send (request context).
- **Relevance scoring** (scorer.go) — **Copy and adapt**. Keep: ScoreRelevance method and prompt.
  Add: temporal decay multiplier based on last_accessed_at age.
- **SQLite storage patterns** — **Rewrite inspired by**. Nostop's schema is conversation/topic/
  message with archive flags. Precon's schema is tier-based with FTS5, promotion tracking,
  and summary pointers. Transaction handling patterns carry over; schema and queries do not.
- **Tool registry and executor** — **Copy and adapt**. Keep: Registry struct, Executor struct,
  Execute method, all built-in tool definitions. Change: nothing in Phase 1. MCP integration
  carries over unchanged.

## Implementation Phases

### Phase 1: Foundation ✅
- Go module, project structure, CI
- Core types: Memory, Tier, Topic, Message
- Storage interface (tier-agnostic)
- API client (copy and adapt from nostop)
- Configuration (JSON)
- **Structured logger** for the unconscious pipeline (slog-based). Every unconscious
  operation (retrieval, persistence, eviction, promotion) logs at minimum: operation name,
  tier, duration, success/failure, item count. Required from Phase 1 — debugging "why
  didn't it remember X" requires seeing what the pipeline actually did. All unconscious
  pipeline components (Retriever, Persister, Synthesizer, Dreamer) accept an `*slog.Logger`
  parameter in their constructor. No component logs to stderr directly.

### Phase 2: REPL + Basic Send Loop ✅
- Styled REPL with readline input and streaming output
- lipgloss styling, glamour markdown rendering
- API client wired up, basic send/receive working
- L1 context management (message history in memory)
- Token counting for L1 budget enforcement. Primary: Anthropic count_tokens API, called
  once per Send() to count the full outgoing message array. Fallback: character-based
  estimation (4 chars per token) when the API returns an error or when running offline.
  Use character estimation for per-message cost during L1 pruning decisions; use the
  API for final request validation before sending.
- No retrieval pipeline yet — just a working chat

### Phase 3: L2 — Hot Storage ✅
- SQLite schema with FTS5 full-text index on message content and topic keywords
- Topic detection and tracking (copy and adapt nostop's detector into persister)
- Messages persist to L2 after each turn
- Context manager with L1/L2 awareness
- Tests against SQLite

### Phase 4: Retriever + Direct Injection ✅
- Retriever: queries L2 via FTS5 for relevant topics given user prompt
- Direct injection: top-N results formatted and injected into system prompt
- No synthesizer LLM call — Opus handles relevance filtering from raw results
- Pre-prompt pipeline wiring: prompt → retriever → format → inject → API request

### Phase 5: Background Persister ✅
- Haiku-based output reviewer (runs after each working agent turn)
- Uses tool_use with a JSON schema for structured output (not free-form JSON parsing).
  Schema defines: `new_topics` (array), `updated_scores` (array), `should_evict` (bool).
  Retry on parse failure (max 2 retries). Fallback: skip persistence for this turn, log
  the failure.
- Runs on engine context (not request context) so persistence survives request cancellation
- Writes to L2 (hot storage)
- L1 pruning: drop low-relevance messages from the API request when near token budget
- Topic lifecycle management

### Phase 6: L4 — Semantic Storage (pgvector) + Synthesizer ✅
- pgvector integration (localhost:5431, existing infrastructure)
- nomic-embed-text embeddings via RunPod serverless (768 dimensions)
- Embedding generation for messages/topics
- Retriever extended: queries L2 (FTS5) + L4 (semantic similarity)
- Eviction path: L2 → L4 (full content preserved with embeddings)
- **Synthesizer introduced**: When combined L2 + L4 retrieval results exceed the
  `synthesis_budget` token count, a Haiku call compresses them before injection.
  Below the budget, direct injection continues unchanged.

### Phase 7: L3 — Warm Storage (Summary Layer)
- Summary generation when content moves from L2 → L4
- Summaries stored in SQLite as lightweight pointers to L4 entries
- L3 Store implementation holds a reference to L4 Store. L3's `Query` method scans
  summaries, and when a match is found, dereferences the pointer to return full content
  from L4. The retriever does not orchestrate this — L3 handles it internally.
- Promotion state machine implemented (see Eviction / Promotion Policy above):
  L4_ONLY → L2_PROMOTED → L2_NATIVE or back to L4_ONLY

### Phase 8: Dreaming
- `precon dream` subcommand (cron target)
- User-configurable analysis prompts (JSON config)
- Cross-session pattern recognition
- **Paginated memory loading**: Query L2 with time window filter and token budget cap.
  If a 30-day lookback exceeds the Haiku context window, chunk into multiple analysis
  calls and merge results. Note: the current `Store.List(ctx, conversationID)` interface
  does not support pagination or time-window filtering. The Store interface will need a
  `ListWithOptions` method or the dreamer will need to filter in-memory (acceptable for
  small datasets, not for years of accumulated data).
- Dream reports stored as L2/L3 entries (meta-knowledge)
- "You've been debugging auth-related issues in 4 of your last 7 sessions"

### Phase 9: Polish
- `/dream`, `/recall`, `/tiers`, `/topics` REPL commands
- JSON configuration with sensible defaults
- Graceful shutdown for all background goroutines (engine context cancellation)
- Integration tests

## Technical Decisions

- **Language**: Go 1.25+ (consistency with nostop, good concurrency for agent pipeline)
- **SQLite**: modernc.org/sqlite (pure Go, no CGO) with FTS5 for L2/L3
- **pgvector**: PostgreSQL on localhost:5431 for L4
- **Embeddings**: nomic-embed-text via RunPod serverless (768 dimensions)
- **REPL**: lipgloss v2 (styling) + glamour (markdown) + readline (input history)
- **API**: Anthropic Claude (Haiku for unconscious agents, Sonnet/Opus for working agent)
- **Config**: JSON (encoding/json, stdlib)
- **Logging**: slog (stdlib) for structured logging of the unconscious pipeline
- **Structured output**: Persister uses tool_use with JSON schema, not free-form text parsing
- **Default values**: MaxContextTokens=200000, SynthesisBudget=4000 tokens,
  RetrieverLimit=10 per tier, L2_MAX_TOKENS=500000, WorkingModel=claude-opus-4-6,
  FastModel=claude-haiku-4-5-20251001, DBPath=.precon/precon.db,
  PgConnStr=postgres://postgres:postgres@localhost:5431/postgres

## Open Questions

1. **Retriever result limit**: How many L2/L4 results should the retriever return per tier?
   Default 10 per tier. Tunable via config. Direct injection budget caps the total tokens
   injected regardless of result count.
2. **Dream frequency**: Default: daily. User-configurable via `dream_schedule` in config
   JSON. Run manually with `precon dream` or schedule via system cron.
3. **Fast path**: Should there be a "fast path" that skips retrieval when the prompt is
   clearly a continuation of the current L1 context (e.g., follow-up question with no
   topic shift)? Potential optimization for Phase 9.
4. **L5 implementation**: Deferred until L1-L4 are proven. Content stays in L4 as the
   deepest tier for now.

## Dependents to Update

When modifying these shared interfaces, all consumers must be updated:

- **`tier.Store` interface** (`internal/tier/tier.go`) — used by: `internal/retriever/retriever.go`,
  `internal/persister/persister.go`, `internal/dreamer/dreamer.go`, `pkg/precon/precon.go`.
  If `List` is extended with pagination/time-window parameters, all implementations and
  consumers must update.
- **`persister.LLM` interface** (`internal/persister/persister.go`) — expanded in Phase 5 to
  include `CompleteWithTools`. Note: `synthesizer.LLM` and `dreamer.LLM` are separate identical
  interfaces (correct Go consumer-defined pattern) and remain at `Complete`-only.
