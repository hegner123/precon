# OpenClaude Prompt Library

Extracted prompt patterns from the OpenClaude codebase for studying how a production agentic coding tool instructs its LLM. Use this to inform the design of your own agent prompts.

## Documents

| Document | What It Covers |
|----------|---------------|
| [01-system-prompt.md](01-system-prompt.md) | The core system prompt — identity, safety rails, behavioral framing, and section architecture |
| [02-tool-use-core.md](02-tool-use-core.md) | Core tool prompts: Bash, FileRead, FileEdit, FileWrite, Glob, Grep |
| [03-tool-use-agent.md](03-tool-use-agent.md) | Agent/subagent tool: delegation, fork semantics, prompt-writing guidance |
| [04-tool-use-tasks.md](04-tool-use-tasks.md) | Task management: TodoWrite, TaskCreate/Get/List/Update/Stop |
| [05-tool-use-web-search.md](05-tool-use-web-search.md) | Web tools: WebFetch, WebSearch, AskUserQuestion |
| [06-tool-use-specialized.md](06-tool-use-specialized.md) | Specialized tools: LSP, MCP, Plan Mode, Worktree, Skills, Config, Cron |
| [07-context-and-memory.md](07-context-and-memory.md) | Context window management: compaction, session memory, memory extraction |
| [08-multi-agent.md](08-multi-agent.md) | Multi-agent orchestration: teams, teammates, message routing, task coordination |
| [09-decision-making.md](09-decision-making.md) | Classifiers and decision systems: auto-mode security classifier, permission explainer |
| [10-design-patterns.md](10-design-patterns.md) | Cross-cutting patterns, prompt engineering techniques, and lessons learned |

## Source Map

All prompts are extracted from the `src/` directory of the OpenClaude repository. Key source locations:

- **System prompt assembly**: `src/constants/prompts.ts`
- **System prompt sections**: `src/constants/systemPromptSections.ts`
- **Security rail**: `src/constants/cyberRiskInstruction.ts`
- **Output styles**: `src/constants/outputStyles.ts`
- **Tool prompts**: `src/tools/{ToolName}/prompt.ts`
- **Context compaction**: `src/services/compact/prompt.ts`
- **Session memory**: `src/services/SessionMemory/prompts.ts`
- **Memory extraction**: `src/services/extractMemories/prompts.ts`
- **Teammate addendum**: `src/utils/swarm/teammatePromptAddendum.ts`
- **Auto-mode classifier**: `src/utils/permissions/yoloClassifier.ts`
- **Permission explainer**: `src/utils/permissions/permissionExplainer.ts`
- **System prompt resolver**: `src/utils/systemPrompt.ts`
