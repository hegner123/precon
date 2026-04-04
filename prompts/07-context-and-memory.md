# 07 — Context Window Management & Memory

> Source: `src/services/compact/prompt.ts`, `src/services/SessionMemory/prompts.ts`, `src/services/extractMemories/prompts.ts`

## Overview

OpenClaude uses three complementary systems to manage context:
1. **Compaction** — summarize conversation history when approaching context limits
2. **Session Memory** — structured notes file updated throughout the session
3. **Memory Extraction** — background agent that extracts persistent memories to disk

---

## Compaction (Context Summarization)

### Architecture
Compaction uses a side-query to summarize conversation history. The summary replaces the original messages, freeing context space. Three modes:

- **Full compaction**: Summarize the entire conversation
- **Partial compaction (from)**: Summarize only recent messages, keep earlier context
- **Partial compaction (up_to)**: Summarize prefix, keep recent messages verbatim

### No-Tools Preamble
Because the summarization query inherits the parent's tool set (for cache key match), the prompt aggressively prevents tool use:

```
CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail.
- Your entire response must be plain text: an <analysis> block followed by
  a <summary> block.
```

This preamble appears at both the start AND end of the compaction prompt (belt and suspenders).

### Summary Structure (9 Sections)
```
1. Primary Request and Intent
2. Key Technical Concepts
3. Files and Code Sections (with code snippets and why-important notes)
4. Errors and fixes (with user feedback on corrections)
5. Problem Solving
6. All user messages (non-tool-result messages — critical for intent tracking)
7. Pending Tasks
8. Current Work (most recent activity with file names and snippets)
9. Optional Next Step (with direct quotes from recent conversation)
```

### Analysis Scratchpad
The prompt uses an `<analysis>` tag as a drafting scratchpad:
```
Before providing your final summary, wrap your analysis in <analysis> tags.
In your analysis:
1. Chronologically analyze each message and section
2. Double-check for technical accuracy and completeness
```

The `<analysis>` block is **stripped** by `formatCompactSummary()` before the summary enters context. This is a "thinking out loud" pattern that improves summary quality without polluting the working context.

### Continuation Message
After compaction, the conversation resumes with:
```
This session is being continued from a previous conversation that ran out
of context. The summary below covers the earlier portion.

{formatted summary}

If you need specific details from before compaction, read the full transcript
at: {transcriptPath}
```

In non-interactive mode (CI/automation):
```
Continue the conversation from where it left off without asking the user any
further questions. Resume directly — do not acknowledge the summary, do not
recap what was happening, do not preface with "I'll continue" or similar.
Pick up the last task as if the break never happened.
```

### Custom Compaction Instructions
Users can provide custom instructions (e.g., "focus on test output and code changes") that are appended to the prompt. Examples from the prompt:
```
## Compact Instructions
When summarizing the conversation focus on typescript code changes and also
remember the mistakes you made and how you fixed them.
```

---

## Session Memory (Structured Notes)

### Template
A structured markdown file maintained throughout the session:

```markdown
# Session Title
_A short and distinctive 5-10 word descriptive title_

# Current State
_What is actively being worked on right now? Pending tasks. Immediate next steps._

# Task specification
_What did the user ask to build? Design decisions or explanatory context_

# Files and Functions
_Important files? What do they contain and why are they relevant?_

# Workflow
_Bash commands usually run and in what order? How to interpret output?_

# Errors & Corrections
_Errors encountered and fixes. What did the user correct? Failed approaches?_

# Codebase and System Documentation
_Important system components? How do they work/fit together?_

# Learnings
_What worked well? What has not? What to avoid?_

# Key results
_If user asked for specific output, repeat exact result here_

# Worklog
_Step by step, what was attempted, done? Very terse summary_
```

### Update Prompt
The session memory agent receives specific editing instructions:
```
CRITICAL RULES FOR EDITING:
- Maintain exact structure with all sections, headers, and italic descriptions
- NEVER modify or delete section headers (lines starting with '#')
- NEVER modify the italic _section description_ lines
- ONLY update content that appears BELOW the italic descriptions
- Write DETAILED, INFO-DENSE content — file paths, function names, error
  messages, exact commands
- Keep each section under ~2000 tokens
- IMPORTANT: Always update "Current State" to reflect most recent work
```

### Customization
Users can provide custom templates (`~/.claude/session-memory/config/template.md`) and custom prompts (`~/.claude/session-memory/config/prompt.md`) with `{{variable}}` substitution.

---

## Memory Extraction (Background Agent)

### Architecture
A background fork that processes recent messages and writes persistent memories to disk. Uses a two-turn strategy: read all relevant files in parallel (turn 1), then write all updates in parallel (turn 2).

### Opener
```
You are now acting as the memory extraction subagent. Analyze the most
recent ~{N} messages above and use them to update your persistent memory.

Available tools: Read, Grep, Glob, read-only Bash, and Edit/Write for paths
inside the memory directory only.

You MUST only use content from the last ~{N} messages. Do not waste turns
investigating or verifying content further.
```

### Memory File Format
```markdown
---
type: preference
title: User prefers functional style
---
User consistently rewrites class-based components as functions with hooks.
```

### Index (MEMORY.md)
```
- [Title](file.md) — one-line hook

MEMORY.md is always loaded into your system prompt — lines after 200 will
be truncated, so keep the index concise.
```

### Memory Types
Four-type taxonomy: preferences, codebase knowledge, workflows, corrections. Each type has guidance on what to save and where (private vs. team directory in multi-agent mode).

### Anti-Duplication
```
Check this list before writing — update an existing file rather than creating
a duplicate. Do not write duplicate memories. First check if there is an
existing memory you can update before writing a new one.
```

---

## Key Takeaways for Agent Builders

### Scratchpad Pattern
The `<analysis>` tag in compaction is a powerful technique: let the model think through the summary in a scratchpad that gets stripped before entering context. This improves quality without wasting context tokens.

### Structure Preservation
Session memory uses rigid markdown templates with section headers and descriptions that must never be modified. This prevents the model from reorganizing or reformatting the notes file over time.

### Two-Turn Memory Strategy
The memory extraction agent uses an explicit two-turn strategy (read all, then write all) to maximize parallelism and minimize turn count. This is a good pattern for any agent with limited turns.

### Continuation Without Recap
After compaction, the "resume directly — do not acknowledge the summary" instruction prevents the common failure of the model wasting tokens recapping what it just learned from the summary.

### Custom Instructions Pass-Through
Both compaction and session memory support user-provided custom instructions that are appended to the base prompt. This lets users shape the summarization without replacing the entire prompt.
