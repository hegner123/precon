# Precon Use Cases

How unconscious memory transforms real workflows.

---

## 1. Long-Running Code Agent

**The situation**: You're building a feature over multiple sessions spanning days. Decisions accumulate, the codebase evolves, and the agent needs to understand not just the current state but *why* things are the way they are.

**Without unconscious memory**: Each session starts cold. You paste relevant files, re-explain the architecture, re-state constraints. By session 5, your "context preamble" is longer than your actual request. You are the memory system — manually shuttling context between sessions.

**With Precon**:
- Session 1: You discuss the architecture. The system persists "decided on interface-driven tier system" with relevant keywords.
- Session 3: You say "should we use the same approach for L3?" You don't reference session 1. You don't load anything. The retriever already found the architecture decision, the synthesizer compressed it, and it's in the agent's system prompt: *"Prior decision: interface-driven tier system (tier.Store interface). L2=SQLite, L4=pgvector."* The agent answers in continuity.
- Session 7: The dreamer reports: *"4 of 6 sessions this week involved storage layer changes. Consider stabilizing the Store interface before adding L5."* This report is itself a memory — it shows up in future context when relevant.

**Why it works**: Nobody decided to save the architecture decision. Nobody decided to retrieve it. The system persisted it by default and injected it by default. The agent accumulated project knowledge across sessions the way you accumulate knowledge across days — without consciously filing and retrieving.

---

## 2. Debugging Across Sessions

**The situation**: A bug appears intermittently. You've investigated across three sessions, each time getting closer but not finding the root cause.

**Without unconscious memory**: Each session restarts from symptoms. You re-run diagnostics, re-read code, re-discover dead ends. You are doing the remembering.

**With Precon**:
- Session 1: You investigate, find "the eviction score calculation looks wrong for memories accessed in the last hour." Automatically persisted.
- Session 2: You start working on the same project. The retriever surfaces the prior investigation — it was relevant to this project context. The agent says "Based on prior analysis, the issue is in the eviction score's recency decay — memories accessed within the last hour get a decay factor above 1.0, which should be capped."
- Session 3: The dreamer reports: *"Recurring topic: eviction scoring. Investigated in 3 sessions. Last finding: decay factor exceeds 1.0 for sub-hour recency. Status: unfixed."*

**Why it works**: Debugging context accumulates automatically. Each session picks up from the last session's findings because those findings were injected into the prompt before you typed anything. The agent doesn't need to be told "remember what we found last time" — it already has that context.

---

## 3. Research and Exploration

**The situation**: You're evaluating database options — reading docs, comparing approaches, gathering information over many turns.

**Without unconscious memory**: Notes get lost in scrollback. You forget which options you evaluated. You re-read documentation.

**With Precon**:
- Turns 1-5: You explore pgvector. Key findings are automatically persisted: "pgvector supports IVFFlat and HNSW indexes. HNSW is better for our use case."
- Turns 6-10: You explore Pinecone. Persisted: "Pinecone: managed service, $70/mo for 1M vectors. Serverless mode available."
- Turn 15: You ask "which vector DB should we use?" Both evaluations are already in the injected context. The agent presents a comparison without anyone re-searching either option.

**Why it works**: Exploratory research self-organizes into retrievable knowledge. The synthesis step acts as an automatic research summary that updates as you learn. You never explicitly saved your findings — the system did.

---

## 4. Multi-Project Context Isolation

**The situation**: You work on three projects daily, each with different conventions, architectures, and ongoing tasks.

**Without unconscious memory**: Context bleeds between projects. The agent suggests patterns from Project A when you're in Project B.

**With Precon**:
- Each project gets its own SQLite database (per working directory)
- `cd ~/projects/alpha && precon` queries only alpha's memories
- Project-specific decisions, conventions, and patterns are isolated
- The dreamer can analyze across projects for cross-cutting insights

**Why it works**: Unconscious memory is scoped. When you switch projects, the *system* switches memory banks. You don't configure this per-session — it's automatic, based on where you are.

---

## 5. Tool-Heavy Workflows

**The situation**: A refactoring session involves 10+ tool calls per turn — searching, reading, changing, testing. Tool output floods the context window.

**Without unconscious memory**: Early tool results get pushed out of the context window. The agent loses track of what it found at the start of the turn.

**With Precon**:
- Each tool interaction is stored as a separate memory with the tool name as a keyword
- Tool outputs are truncated for storage (the full output was already consumed in-turn)
- Future retrieval can surface specific tool results: "what did we find when we searched for `evictionScore`?"
- The background reviewer extracts *meaning* from tool results: "Found 3 call sites: eviction.go:90, eviction_test.go:15, promotion.go:42"

**Why it works**: Tool interactions become part of the unconscious knowledge base. The agent can reference specific tool results from prior sessions without re-running tools — because those results were automatically persisted, curated, and injected.

---

## 6. Continuous Self-Improvement via Dreaming

**The situation**: You want to understand your own workflow patterns — what you work on most, where you get stuck, what recurs.

**With Precon**: Configure dream analysis prompts:

```json
{
  "dream_prompts": [
    {
      "name": "weekly-review",
      "prompt": "Analyze the past week's sessions. What were the main themes? What problems recurred?",
      "lookback": "7d"
    },
    {
      "name": "pain-points",
      "prompt": "Identify recurring frustrations or blockers. What keeps coming up?",
      "lookback": "30d"
    }
  ]
}
```

Run `precon dream` weekly. Reports are persisted as memories — they enter the unconscious context for future sessions. The system builds meta-knowledge about your patterns, and that meta-knowledge automatically surfaces when relevant.

**Why it works**: The dreamer closes the loop. Memory isn't just stored — it's analyzed, and the analysis itself becomes part of the unconscious context. The system learns about its own patterns without anyone asking it to.

---

## Who Benefits Most

| User profile | Why unconscious memory matters |
|-------------|-------------------------------|
| **Daily AI-assisted developers** | Cross-session context is automatic — no re-explanation tax |
| **Long-session power users** | Context never degrades, even at 100+ turns |
| **Multi-project developers** | Memory isolation is automatic per project |
| **Research/exploration users** | Findings self-organize without manual note-taking |
| **Teams (future)** | Shared unconscious context across team members |

The common thread: **you stop being the memory system.** Precon handles recall so you can focus on reasoning.
