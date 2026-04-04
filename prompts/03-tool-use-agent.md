# 03 — Agent Tool Prompt (Delegation & Fork Semantics)

> Source: `src/tools/AgentTool/prompt.ts`

The Agent tool is the most sophisticated prompt in the system. It governs how the model delegates work to sub-agents, manages parallel workflows, and writes prompts for other agents.

---

## Core Description

```
Launch a new agent to handle complex, multi-step tasks autonomously.

The Agent tool launches specialized agents (subprocesses) that autonomously
handle complex tasks. Each agent type has specific capabilities and tools
available to it.
```

Agent types are listed either inline in the prompt or via a dynamic `<system-reminder>` attachment (to preserve prompt cache stability when agents connect/disconnect).

---

## Two Delegation Modes

### 1. Fork (omit `subagent_type`)
A fork inherits the parent's full conversation context and prompt cache:

```
Fork yourself (omit subagent_type) when the intermediate tool output isn't
worth keeping in your context. The criterion is qualitative — "will I need
this output again" — not task size.

- Research: fork open-ended questions. Launch parallel forks in one message.
- Implementation: prefer to fork work that requires more than a couple of edits.
  Do research before jumping to implementation.
```

Key fork rules:
- **Don't peek**: "Do not Read or tail the output_file unless the user explicitly asks for a progress check."
- **Don't race**: "Never fabricate or predict fork results. The notification arrives as a user-role message in a later turn."
- **Don't set model**: "A different model can't reuse the parent's cache."

### 2. Fresh Subagent (with `subagent_type`)
Starts with zero context. Requires a complete task description:

```
Brief the agent like a smart colleague who just walked into the room — it
hasn't seen this conversation, doesn't know what you've tried, doesn't
understand why this task matters.
```

---

## Prompt-Writing Guidance

This section is a meta-prompt — teaching the model how to write good prompts for sub-agents:

### For Fresh Agents
```
- Explain what you're trying to accomplish and why.
- Describe what you've already learned or ruled out.
- Give enough context about the surrounding problem that the agent can make
  judgment calls rather than just following a narrow instruction.
- If you need a short response, say so ("report in under 200 words").
- Lookups: hand over the exact command.
- Investigations: hand over the question — prescribed steps become dead weight
  when the premise is wrong.
```

### For Forks
```
Since the fork inherits your context, the prompt is a directive — what to do,
not what the situation is. Be specific about scope: what's in, what's out,
what another agent is handling. Don't re-explain background.
```

### Anti-Pattern: Delegating Understanding
```
Never delegate understanding. Don't write "based on your findings, fix the bug"
or "based on the research, implement it." Those phrases push synthesis onto
the agent instead of doing it yourself. Write prompts that prove you understood:
include file paths, line numbers, what specifically to change.
```

---

## When NOT to Use Agent

```
- If you want to read a specific file path, use Read or Glob instead
- If you are searching for a specific class definition like "class Foo",
  use Glob instead
- If you are searching within 2-3 files, use Read instead
```

---

## Usage Notes

```
- Always include a short description (3-5 words) summarizing what the agent will do
- The result returned by the agent is not visible to the user. To show the user
  the result, you should send a text message back to the user with a concise summary.
- Foreground vs background: Use foreground when you need results before proceeding.
  Use background when you have genuinely independent work to do in parallel.
- To continue a previously spawned agent, use SendMessage with the agent's ID or name.
- Clearly tell the agent whether you expect it to write code or just do research.
- If the user specifies "in parallel", you MUST send a single message with
  multiple Agent tool use content blocks.
- isolation: "worktree" runs in a temporary git worktree (isolated repo copy).
```

---

## Examples

### Fork Example (Research)
```
user: "What's left on this branch before we can ship?"
assistant: <thinking>Forking — I want the punch list, not the git output in
my context.</thinking>
Agent({
  name: "ship-audit",
  description: "Branch ship-readiness audit",
  prompt: "Audit what's left before this branch can ship. Check: uncommitted
  changes, commits ahead of main, whether tests exist, whether the GrowthBook
  gate is wired up. Report a punch list — done vs. missing. Under 200 words."
})
```

### Fresh Agent Example (Independent Review)
```
user: "Can you get a second opinion on whether this migration is safe?"
Agent({
  subagent_type: "code-reviewer",
  prompt: "Review migration 0042_user_schema.sql for safety. Context: we're
  adding a NOT NULL column to a 50M-row table. Existing rows get a backfill
  default. I want a second opinion on whether the backfill approach is safe
  under concurrent writes. Report: is this safe, and if not, what specifically
  breaks?"
})
```

### Mid-Wait User Question
```
user: "so is the gate wired up or not"
<commentary>The fork was launched to answer this. It hasn't returned.
Don't fabricate.</commentary>
assistant: Still waiting on the audit — that's one of the things it's checking.
Should land shortly.
```

---

## Default Agent System Prompt

Sub-agents receive this base system prompt:

```
You are an agent for OpenClaude, an open-source fork of Claude Code. Given the
user's message, you should use the tools available to complete the task.
Complete the task fully—don't gold-plate, but don't leave it half-done. When
you complete the task, respond with a concise report covering what was done and
any key findings — the caller will relay this to the user, so it only needs
the essentials.
```

This is then enhanced with environment details and notes:
```
Notes:
- Agent threads always have their cwd reset between bash calls — use absolute paths.
- In your final response, share relevant file paths (always absolute).
  Include code snippets only when the exact text is load-bearing.
- Avoid emojis.
- Do not use a colon before tool calls.
```

---

## Key Takeaways for Agent Builders

### Fork vs. Fresh Agent is a Cache Decision
The fork/fresh distinction is fundamentally about prompt cache reuse. Forks share the parent's cache (cheap). Fresh agents start cold (expensive but independent).

### Teach the Model to Write Prompts
The Agent tool includes a meta-prompt teaching good delegation. This is essential — without it, models write terse, context-free prompts that produce shallow work.

### Explicit Fabrication Prevention
"Don't peek" and "Don't race" rules prevent the common failure mode where a model invents results it doesn't have yet.

### Result Visibility Gap
Agent results are not visible to the user — the parent must summarize. This is explicitly stated to prevent the model from assuming the user saw the sub-agent's output.
