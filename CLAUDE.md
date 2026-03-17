# Precon — Pre-Conscious Context Management

## References

- **PLAN.md** — Full architecture, phases, data flow, open questions
- **START.md** — Quick start and project structure
- **RLM.pdf** — MIT paper (Zhang, Kraska, Khattab) that inspired the anti-compaction insight
- **nostop** — Sibling project (../nostop) that proved topic-based archival; Precon evolves this

## Architecture

Three-layer metaphor: **Unconscious** (autonomic retrieval/persistence on every prompt),
**Conscious** (working agent doing actual work), **Dreaming** (periodic cross-session analysis).

Five memory tiers: L1 (active context) → L2 (SQLite hot) → L3 (summaries) → L4 (pgvector semantic) → L5 (cold archive).

Standalone styled REPL — owns the full conversation loop and all memory tiers.
Not a Bubbletea TUI. Not a Claude Code plugin.

## Key Directories

```
cmd/precon/           # CLI entry point + REPL
internal/retriever/   # Pre-prompt retrieval agent (unconscious)
internal/synthesizer/ # Context synthesis agent (unconscious)
internal/persister/   # Background persistence agent (unconscious)
internal/dreamer/     # Cron-based analysis (dreaming)
internal/tier/        # Memory tier interfaces and types
internal/storage/     # SQLite + pgvector backends
internal/topic/       # Topic detection and tracking
internal/repl/        # Styled REPL (lipgloss + glamour + readline)
pkg/precon/           # Public engine API
```

## Dependencies

- Go 1.25+
- SQLite via modernc.org/sqlite (pure Go, no CGO)
- PostgreSQL + pgvector on localhost:5431
- Ollama with nomic-embed-text (768-dim embeddings)
- Anthropic API (Haiku for unconscious agents, Opus for working agent)
- lipgloss v2 (styling) + glamour (markdown rendering) + readline (input)
- JSON config (encoding/json, stdlib)
