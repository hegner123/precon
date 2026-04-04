# 10 — Design Patterns & Prompt Engineering Lessons

Cross-cutting patterns extracted from the OpenClaude prompt system that are applicable to any agentic coding tool.

---

## 1. Static/Dynamic Prompt Split

**Pattern**: Divide the system prompt into a static prefix (cacheable) and a dynamic suffix (per-session).

**Implementation**: A boundary marker separates the two regions. Everything before it is identical across users and sessions; everything after contains runtime-specific content (model name, enabled tools, MCP servers, memory).

**Why it matters**: Prompt caching can dramatically reduce costs and latency. The static portion is often 80%+ of the system prompt. Without this split, any per-session variation busts the entire cache.

**Lesson**: Design your system prompt as composable sections with explicit stability guarantees. Track which sections can vary and ensure they're positioned after the cache boundary.

---

## 2. Section Registry Pattern

**Pattern**: Define system prompt sections as named, independently-cacheable units with explicit cache-break semantics.

```typescript
systemPromptSection('memory', () => loadMemoryPrompt())           // Cached
DANGEROUS_uncachedSystemPromptSection('mcp', () => getMcp(), 'MCP servers connect/disconnect')  // Recomputed
```

**Why it matters**: Without this, adding a single dynamic element forces recomputation of everything. The registry pattern lets you cache 95% of sections while recomputing only what changed.

**Lesson**: Make cache-breaking explicit and require justification. The `DANGEROUS_` prefix and required reason string are social engineering for your codebase — they force developers to think twice.

---

## 3. Tool Preference Hierarchy

**Pattern**: Explicitly steer the model away from shell commands toward dedicated tools.

```
Do NOT use Bash for: cat → use Read, sed → use Edit, find → use Glob, grep → use Grep
```

**Why it matters**: LLMs default to familiar shell commands. Dedicated tools provide better UX (permission prompts, diffs, structured output) and safety (sandboxing, validation).

**Lesson**: Don't just provide better tools — explicitly tell the model not to use the worse alternatives. Negative instructions ("don't use cat") are as important as positive ones ("use Read").

---

## 4. Prerequisite Enforcement

**Pattern**: Require reading a file before editing it, enforced in both the prompt AND the tool implementation.

**Why it matters**: Prevents blind modifications. The model must have seen the current state before proposing changes. Dual enforcement (prompt + code) catches both prompt-following and prompt-ignoring models.

**Lesson**: For critical safety invariants, enforce at multiple levels. The prompt teaches the model; the code catches failures.

---

## 5. Scratchpad-Then-Output

**Pattern**: Let the model think in an `<analysis>` tag that gets stripped before the result enters context.

```
<analysis>
[Thought process, checking all points...]
</analysis>
<summary>
[Clean output that enters context]
</summary>
```

**Why it matters**: The analysis improves output quality (the model "thinks out loud") without consuming context tokens in the final result. Used in compaction where context budget is critical.

**Lesson**: When you need high-quality structured output, give the model a scratchpad. Strip it before using the output. This is cheaper than extended thinking and works with any model.

---

## 6. Example-Driven Calibration

**Pattern**: Use extensive positive AND negative examples to teach the model when to use a capability.

The TodoWrite tool uses 9 examples: 5 showing when to create todos, 4 showing when NOT to. Each includes a `<reasoning>` tag explaining the decision.

**Why it matters**: Rules alone are ambiguous. "Use for complex tasks" means different things to different models. Examples establish concrete thresholds.

**Lesson**: Invest in negative examples. Showing when NOT to use a tool is as important as showing when to use it.

---

## 7. Dual-Form Labels

**Pattern**: Require both imperative ("Run tests") and progressive ("Running tests") forms for task descriptions.

**Why it matters**: The imperative form describes the work; the progressive form drives the UI spinner. This separation lets the same data serve both purposes.

**Lesson**: Think about how your data will be displayed in different contexts. Require the right form at creation time rather than trying to transform it later.

---

## 8. Fabrication Prevention

**Pattern**: Explicitly tell the model not to invent results it doesn't have.

Fork agents: "Never fabricate or predict fork results in any format — not as prose, summary, or structured output."

Verification: "Your own checks do NOT substitute — only the verifier assigns a verdict."

**Why it matters**: Models will confidently generate plausible-sounding results when they don't have the actual data. This is especially dangerous in coding agents where fabricated test results can mask bugs.

**Lesson**: Wherever there's a gap between what the model knows and what it might be asked about, add an explicit "don't fabricate" instruction with specific anti-patterns.

---

## 9. Authorization Scoping

**Pattern**: "A user approving an action once does NOT mean they approve it in all contexts."

**Why it matters**: Without explicit scoping, models generalize from a single approval to a blanket permission. Approving one `git push` shouldn't mean all future pushes are pre-approved.

**Lesson**: Treat every approval as scoped to its specific context. Require re-confirmation for new contexts, even with the same action.

---

## 10. Investigate Before Destroying

**Pattern**: "When you encounter an obstacle, do not use destructive actions as a shortcut."

Examples: Don't `--no-verify` past hook failures. Don't delete lock files without checking what holds them. Don't discard merge conflicts.

**Why it matters**: The path of least resistance for an agent is often destructive. Hook failing? Skip it. Lock file in the way? Delete it. Merge conflict? Force it.

**Lesson**: Explicitly list common destructive shortcuts and teach the model to investigate root causes instead.

---

## 11. Communication Visibility Model

**Pattern**: Explicitly state what IS and ISN'T visible to whom.

- "Text output is displayed to the user" (system section)
- "Text output is NOT visible to other agents" (multi-agent)
- "Agent results are not visible to the user" (agent tool)

**Why it matters**: Models assume their output is universally visible. In multi-agent systems, this leads to "talking past" other agents.

**Lesson**: For every communication channel, explicitly state the visibility rules. Don't assume the model understands the information flow.

---

## 12. Continuation Without Recap

**Pattern**: After context compaction, instruct: "Resume directly — do not acknowledge the summary, do not recap, do not preface with 'I'll continue.'"

**Why it matters**: Without this, the model wastes significant tokens re-explaining what it just learned from the summary. In context-constrained situations, this waste is especially costly.

**Lesson**: When providing a model with summarized history, explicitly prevent it from echoing that summary back.

---

## 13. Cost-Aware Sleep Guidance

**Pattern**: "Each wake-up costs an API call, but the prompt cache expires after 5 minutes of inactivity — balance accordingly."

**Why it matters**: In autonomous/proactive mode, the model controls its own polling frequency. Without cost awareness, it either polls too frequently (expensive) or too infrequently (cache expires, more expensive per-call).

**Lesson**: When the model controls a cost-bearing action (API calls, searches, deployments), teach it the cost model so it can make informed tradeoffs.

---

## 14. Dynamic Prompt Composition

**Pattern**: Assemble tool prompts from runtime state rather than static strings.

The Bash tool prompt is composed from ~10 sub-sections: sandbox config, git settings, background usage, commit protocol, PR protocol, embedded search tools. Each section is conditionally included based on feature flags, user type, and runtime config.

**Why it matters**: Static prompts either include everything (too long) or miss relevant guidance (too short). Dynamic composition keeps prompts relevant and concise.

**Lesson**: Build a section-based prompt assembly system. Each section has clear inclusion criteria and can be independently tested.

---

## 15. Meta-Prompting (Teaching Prompt Writing)

**Pattern**: The Agent tool includes a full guide on how to write good prompts for sub-agents.

```
Brief the agent like a smart colleague who just walked into the room.
Explain what you're trying to accomplish and why.
Never delegate understanding — write prompts that prove you understood.
```

**Why it matters**: Without meta-prompting guidance, models write terse, context-free delegation prompts that produce shallow work.

**Lesson**: If your agent can delegate to other agents, teach it how to delegate well. The quality of the delegation prompt directly determines the quality of the delegated work.
