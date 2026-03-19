# Why Precon — Unconscious Memory for AI Agents

## The Core Insight

Precon exists to solve one problem: **AI agents don't remember.**

But the solution isn't giving agents a memory tool. If the agent has to invoke `retrieve_memories()` when it thinks it needs context, that's *conscious* recall — the agent must decide to remember, decide *what* to remember, and get the timing right. If it forgets to call the tool, the context is gone. You've offloaded the cognitive burden onto the agent, which is exactly the wrong place.

**Precon makes memory unconscious.** Context from prior sessions is systematically retrieved, compressed, and injected into the agent's system prompt *before every interaction* — automatically, with no agent participation. The agent never requests its memories. It never decides what to recall. It just... knows. The way you know your name without deciding to remember it.

This is the fundamental difference between pre-con and an MCP memory tool:

| | MCP Memory Tool | Precon |
|---|---|---|
| **Who decides to recall?** | The agent | The system |
| **When does recall happen?** | When the agent thinks to invoke it | Every single turn, by default |
| **What if recall is forgotten?** | Context is lost | Can't happen — injection is automatic |
| **Cognitive burden** | On the agent | On the infrastructure |
| **Metaphor** | Looking something up in a notebook | Just knowing it |

---

## What Unconscious Memory Gets You

### 1. Sessions That Pick Up Where You Left Off

Close your terminal. Come back two days later. Start a new session and say "what was that eviction bug we were looking at?" You don't paste anything. You don't load a context file. The system already retrieved the relevant memories, compressed them, and injected them before the agent saw your prompt. The agent answers as if it was there.

This happens because retrieval is the *default state*, not an action anyone takes.

### 2. Memory Without Effort

Every turn is automatically persisted — user messages, agent reasoning, tool interactions, responses — as separate searchable memories in L2 hot storage. There is no "save" command. Persistence is what happens. A background reviewer (Haiku) then curates: keeping decisions and discoveries, discarding greetings and filler, adjusting relevance scores as topics evolve.

You don't manage the memory. You don't tag things. You don't organize. You just work, and knowledge accumulates.

### 3. Signal, Not Noise

Raw logging would flood the agent's context with garbage. The synthesizer compresses potentially hundreds of thousands of stored tokens into a ~4K token context block — preserving specific details (names, numbers, code snippets, decisions) while dropping conversational filler. The agent gets high-density context, not a chat transcript.

This compression is what makes unconscious injection practical. You can't inject raw history into every prompt — it would consume the entire context window. But 4K tokens of compressed, relevant context carries more information than 40K tokens of raw conversation.

### 4. Conversations That Don't Degrade

Traditional LLM sessions fall apart as they get longer — early context drops out of the window, the model forgets, responses lose coherence. Precon breaks this:

- Old, low-relevance memories are evicted from L2 to L4 semantic storage
- Summaries in L3 keep them searchable by keyword
- When old context becomes relevant again, it's promoted back to L2
- Sessions run indefinitely without degradation

Old knowledge is never lost. It moves to a tier optimized for its access pattern, and the retriever brings it back when it matters.

### 5. Retrieval That Understands Meaning

A search for "that authentication issue" finds memories even if they never used the word "authentication." Three retrieval paths fire in parallel:

- **L2 FTS5**: Keyword matches in hot storage (sub-millisecond)
- **L3 FTS5**: Keyword matches across summaries of evicted content
- **L4 Semantic**: 768-dim vector cosine similarity (meaning-based)

The agent doesn't need to phrase its queries carefully because the agent doesn't phrase queries at all. The system takes the user's raw prompt and casts a wide net across all three paths.

### 6. Background Processing That Never Blocks You

The REPL is ready for your next input before memory processing finishes:

- L2 writes are fast SQLite inserts — no latency
- Background Haiku review runs async after each turn
- Eviction runs only when L2 exceeds its budget
- Dreaming runs on a cron schedule, completely outside interactive sessions

Memory management is invisible. Like unconscious processes should be.

### 7. Self-Improving Knowledge

The Dreamer process runs periodically across stored memories, surfacing patterns you might not notice:

- "3 of 5 sessions this week involved debugging test failures"
- "The pgvector connection keeps timing out — consider connection pooling"
- "You've been refactoring toward smaller interfaces over the last month"

Dream reports are themselves persisted as memories — the system builds meta-knowledge about its own patterns. Future retrievals can surface these insights, making the unconscious context richer over time.

### 8. Graceful Degradation

Every component fails without breaking the conversation. If L4 is down, L2 and L3 still work. If the synthesizer fails, raw results are injected. If the background persister fails, raw data is already safe in L2. The worst case is temporarily reduced recall quality — you never get an error that blocks your work.

Unconscious processes should be invisible when they work and silent when they fail.

---

## The Contrast

| Scenario | Without Precon | With Precon |
|----------|---------------|-------------|
| Start new session | Re-explain everything from scratch | Prior context is already in the prompt |
| Long session (100+ turns) | Early context lost as window fills | Old memories evicted to L4, promoted back when relevant |
| "What did we decide about X?" | Scroll through chat history | The agent already knows — it was in the injected context |
| Multiple projects | Context from project A leaks into B | Per-project databases, isolated unconscious memory |
| Recurring bug pattern | Re-diagnose each time | Dreamer surfaced the pattern last week; it's in context |

The fundamental shift: **you stop managing context and start just working.** The system handles recall the way your brain handles breathing — automatically, continuously, without conscious intervention.
