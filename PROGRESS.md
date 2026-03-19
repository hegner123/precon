# Precon — Phase Completion Tracker

| Phase | Status | Date | Summary |
|-------|--------|------|---------|
| 1 — Foundation | ✅ COMPLETE | 2026-03-16 | Go module, core types (Memory, Tier, Topic), tier.Store interface, API client (copy from nostop), JSON config, slog-based structured logger |
| 2 — REPL + Basic Send Loop | ✅ COMPLETE | 2026-03-16 | Styled REPL (readline + lipgloss + glamour), API client wired, streaming responses, L1 context management, token counting (Anthropic API + char fallback) |
| 3 — L2 Hot Storage | ✅ COMPLETE | 2026-03-16 | SQLite schema with FTS5, topic detection (Haiku via persister), messages persist to L2 after each turn, context manager with L1/L2 awareness |
| 4 — Retriever + Direct Injection | ✅ COMPLETE | 2026-03-16 | Retriever queries L2 via FTS5, top-N results injected into system prompt, pre-prompt pipeline wired |
| 5 — Background Persister | ✅ COMPLETE | 2026-03-16 | Haiku-based output reviewer with tool_use + JSON schema, persister.LLM expanded with CompleteWithTools, runs on engine context, writes to L2, L1 pruning, adapter for api.Client → persister.LLM |
| 6 — L4 Semantic Storage | ✅ COMPLETE | 2026-03-17 | pgvector integration, RunPod serverless embeddings (nomic-embed-text 768d), retriever extended for L2+L4, eviction path L2→L4, embedding client with batch support |
| 7 — L3 Warm Storage | 🔲 NOT STARTED | — | Summary generation, L3 Store with L4 dereference, promotion state machine |
| 8 — Dreaming | 🔲 PARTIAL | 2026-03-18 | `precon dream` subcommand exists, user-configurable prompts work, paginated memory loading not yet implemented |
| 9 — Polish | 🔲 NOT STARTED | — | REPL commands (/dream, /recall, /tiers, /topics), graceful shutdown, integration tests |

## Structural Milestones

- **repl.go split** — DONE (2026-03-17): Split into commands.go, format.go, persist.go, prune.go, repl.go, send.go
- **Model resolver** — DONE: Automatic fallback chains with API discovery when static fallbacks exhausted
- **Tool system** — DONE: 20+ built-in tools, registry, executor, agent.log audit trail

## Test Coverage

Packages with tests: `api`, `embedding`, `persister`, `repl`, `retriever`, `storage`, `tier`, `tools`, `topic`

Packages without tests: `dreamer`, `mcp`, `synthesizer`, `pkg/precon`, `cmd/precon`
