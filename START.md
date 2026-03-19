# Precon — Quick Start

## What Is This

Precon (pre-conscious) is a multi-tier memory management system for LLM-driven agentic
workflows. It takes inspiration from how brains work: unconscious processes (retrieval,
sensory input) happen automatically, while conscious processes (reasoning, tool use) are
deliberate.

Standalone Go REPL that owns the full conversation pipeline. Not a TUI — a styled
readline REPL like Claude Code.

Built in Go. Evolved from [nostop](../nostop).

## Key Files

1. **PLAN.md** — Full architecture, metaphor, phases, data flow
2. **RLM.pdf** — MIT paper that inspired the original nostop project (context on why
   compaction fails and recursive access patterns work)

## Architecture at a Glance

```
User Prompt → [Retriever] → [Synthesizer] → API Request → Streaming Response
                   ↓              ↓               ↓
              L2/L3/L4      Compressed ctx    [Persister] → L2
                                                   ↓
                                              [Dreamer] → Reports
```

**Memory Tiers**: L1 (prompt) → L2 (SQLite hot) → L3 (summaries) → L4 (pgvector) → L5 (cold)

## Project Structure

```
precon/
├── cmd/precon/           # CLI entry point + REPL
├── internal/
│   ├── api/              # Claude API client
│   ├── storage/          # SQLite + pgvector storage
│   ├── tier/             # Memory tier management (L1-L5)
│   ├── retriever/        # Pre-prompt retrieval agent (unconscious)
│   ├── synthesizer/      # Context synthesis agent (unconscious)
│   ├── persister/        # Background persistence agent (unconscious)
│   ├── dreamer/          # Cron-based analysis (dreaming)
│   ├── topic/            # Topic detection and tracking
│   └── repl/             # Styled REPL (lipgloss + glamour + readline)
├── pkg/precon/           # Public API / engine
├── PLAN.md               # Full implementation plan
├── START.md              # This file
└── RLM.pdf               # Reference paper
```

## Dependencies

- Go 1.25+
- SQLite (via modernc.org/sqlite, pure Go)
- PostgreSQL + pgvector on localhost:5431 (for L4 semantic storage)
- RunPod serverless with nomic-embed-text (768-dim embeddings)
- Anthropic API key

## Quick Commands

```bash
just build    # Build binary
just test     # Run tests
just run      # Build and run
just dev      # Build with race detector and run
```

## Current Status

Phase 6+ — L4 semantic storage and embedding integration. Phases 1–6 complete. See PLAN.md for the full roadmap and PROGRESS.md for completion status.
