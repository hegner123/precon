# Precon

Pre-conscious context management for LLM conversations. A multi-tier memory system that automatically retrieves, synthesizes, and persists context across sessions — so your agent remembers what matters without manual bookkeeping.

## Features

- **Tiered memory hierarchy** (L1-L5): active context, SQLite hot storage with FTS5, summaries, pgvector semantic search, and cold archive
- **Automatic retrieval**: every prompt triggers unconscious retrieval from L2/L3/L4 tiers — no explicit recall needed
- **Background persistence**: a Haiku agent reviews each turn and decides what to save, trim, or promote
- **Semantic search**: pgvector-backed L4 tier with 768-dimensional embeddings for meaning-based recall
- **Dreaming**: configurable cron-based analysis that runs prompts across session history to surface patterns and insights
- **Built-in tool system**: file operations, code search, diffing, refactoring tools available to the working agent
- **Styled REPL**: readline-based interactive shell with lipgloss styling and glamour markdown rendering
- **Connection checker**: `precon check` validates all external dependencies (SQLite, PostgreSQL, pgvector, embeddings API, Anthropic API)

## Prerequisites

- Go 1.25+
- PostgreSQL with [pgvector](https://github.com/pgvector/pgvector) extension (default: `localhost:5431`)
- Embedding endpoint (RunPod with nomic-embed-text, 768 dimensions)
- [Anthropic API](https://console.anthropic.com/) key
- [just](https://github.com/casey/just) command runner (optional, for build shortcuts)

## Installation

```bash
# Build from source
go install github.com/hegner123/precon/cmd/precon@latest

# Or clone and build locally
git clone https://github.com/hegner123/precon.git
cd precon
just install
```

## Configuration

Copy the example config to `~/.precon/config.json`:

```bash
mkdir -p ~/.precon
cp precon.json.example ~/.precon/config.json
```

Set required environment variables (or add to config file):

```bash
export ANTHROPIC_API_KEY="sk-..."
export RUNPOD_API_KEY="..."
```

## Usage

```bash
# Start the REPL
precon

# Check all external connections
precon check

# Run dream analysis
precon dream
```

The REPL automatically manages context across sessions. Prior conversations are retrieved and injected into each prompt based on topic relevance. No manual save/load required.

## Architecture

```
User Prompt -> [Retriever] -> [Synthesizer] -> API Request -> Streaming Response
                   |              |               |
              L2/L3/L4      Compressed ctx    [Persister] -> L2
                                                   |
                                              [Dreamer] -> Reports
```

Three-layer metaphor:
- **Unconscious** — autonomic retrieval and persistence on every prompt
- **Conscious** — the working agent doing actual work with tools
- **Dreaming** — periodic cross-session analysis and pattern recognition

## License

[MIT](LICENSE)
