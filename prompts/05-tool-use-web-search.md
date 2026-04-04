# 05 — Web & User Interaction Tool Prompts

> Source: `src/tools/WebFetchTool/prompt.ts`, `src/tools/WebSearchTool/prompt.ts`, `src/tools/AskUserQuestionTool/prompt.ts`

---

## WebFetch Tool

### Description
```
- Fetches content from a URL and processes it using an AI model
- Takes a URL and a prompt as input
- Converts HTML to markdown, processes with a small, fast model
- Returns the model's response about the content
```

### Usage Notes
```
- IMPORTANT: If an MCP-provided web fetch tool is available, prefer using that
  tool instead, as it may have fewer restrictions.
- The URL must be a fully-formed valid URL
- HTTP URLs automatically upgraded to HTTPS
- Results may be summarized if content is very large
- Includes a self-cleaning 15-minute cache
- When a URL redirects to a different host, the tool informs you and provides
  the redirect URL — make a new request with the redirect URL
- For GitHub URLs, prefer using the gh CLI via Bash
```

### Secondary Model Prompt
WebFetch uses a two-model pipeline. The secondary (small) model receives:
```
Web page content:
---
{markdownContent}
---

{prompt}

{guidelines based on domain}
```

For pre-approved domains, guidelines are permissive. For others, copyright-safe:
```
- Enforce a strict 125-character maximum for quotes from any source document
- Use quotation marks for exact language from articles
- You are not a lawyer and never comment on the legality of your own responses
- Never produce or reproduce exact song lyrics
```

---

## WebSearch Tool

### Description
```
- Allows Claude to search the web and use results to inform responses
- Provides up-to-date information beyond knowledge cutoff
- Returns search result information with markdown hyperlinks
```

### Critical Requirement: Sources
```
CRITICAL REQUIREMENT - You MUST follow this:
- After answering the user's question, you MUST include a "Sources:" section
- List all relevant URLs as markdown hyperlinks: [Title](URL)
- This is MANDATORY - never skip including sources

Example format:
    [Your answer here]

    Sources:
    - [Source Title 1](https://example.com/1)
    - [Source Title 2](https://example.com/2)
```

### Year Awareness
```
IMPORTANT - Use the correct year in search queries:
- The current month is {currentMonthYear}. You MUST use this year when searching
  for recent information, documentation, or current events.
- Example: If the user asks for "latest React docs", search with the current year
```

---

## AskUserQuestion Tool

### Description
```
Asks the user multiple choice questions to gather information, clarify ambiguity,
understand preferences, make decisions or offer them choices.
```

### Usage Scenarios
```
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices about direction to take
```

### Design Features
- Users always have an "Other" option for free-text input
- `multiSelect: true` for multiple answers
- Recommended options go first with "(Recommended)" suffix
- Optional `preview` field for side-by-side comparison (ASCII mockups, code snippets, config examples)

### Plan Mode Constraint
```
In plan mode, use this tool to clarify requirements BEFORE finalizing your plan.
Do NOT use this to ask "Is my plan ready?" — use ExitPlanMode for that.
IMPORTANT: Do not reference "the plan" in questions because the user cannot see
the plan in the UI until you call ExitPlanMode.
```

### Preview Feature
```
Use the optional preview field when presenting concrete artifacts that users
need to visually compare:
- ASCII mockups of UI layouts
- Code snippets showing different implementations
- Configuration examples

Preview content is rendered as markdown in a monospace box. Do not use previews
for simple preference questions where labels and descriptions suffice.
```

---

## Key Takeaways for Agent Builders

### Two-Model Pipeline for Web Content
WebFetch uses a small, fast model to process web content before returning results. This keeps costs low while still allowing intelligent extraction. The copyright-safe guidelines for non-approved domains are a pragmatic approach.

### Mandatory Source Attribution
The WebSearch tool makes source listing a "CRITICAL REQUIREMENT" — not optional guidance. This prevents the common failure of presenting web information without attribution.

### Year Injection
Explicitly telling the model the current date in search prompts prevents the common failure of searching with outdated year references.

### Question Tool as UI Component
AskUserQuestion isn't just "ask something" — it's a rich UI component with multiple-choice, multi-select, previews, and recommended options. This turns the LLM into a form designer.

### Plan Mode Awareness
The AskUserQuestion prompt is aware of Plan Mode state and explicitly redirects certain question patterns (plan approval) to the correct tool. This cross-tool awareness prevents UX confusion.
