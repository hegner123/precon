# 06 — Specialized Tool Prompts

> Source: Various `src/tools/*/prompt.ts` files

---

## LSP Tool

Code intelligence via Language Server Protocol:
```
Supported operations:
- goToDefinition: Find where a symbol is defined
- findReferences: Find all references to a symbol
- hover: Get hover information (documentation, type info)
- documentSymbol: Get all symbols in a document
- workspaceSymbol: Search for symbols across workspace
- goToImplementation: Find implementations of interfaces
- prepareCallHierarchy: Get call hierarchy item at a position
- incomingCalls / outgoingCalls: Call hierarchy navigation

All operations require: filePath, line (1-based), character (1-based)
```

---

## Plan Mode Tools

### EnterPlanMode
Two prompt variants based on user type:

**External users** (more aggressive about planning):
```
Prefer using EnterPlanMode for implementation tasks unless they're simple.
```

**Internal users** (more restrained):
```
When in doubt, prefer starting work and using AskUserQuestion.
```

### ExitPlanMode
```
Write your plan to a file first, then call this tool to signal readiness
for user review.

Do NOT use AskUserQuestion to ask "Is this plan okay?" — that's what
ExitPlanMode does.

Only for implementation planning — not research/exploration.
```

---

## Worktree Tools

### EnterWorktree
```
Only use when the user explicitly says "worktree".
Creates a git worktree in .claude/worktrees/ with a new branch based on HEAD.
```

### ExitWorktree
```
Two actions: "keep" (leave on disk) or "remove" (delete).
discard_changes flag needed to force-remove with uncommitted work.
Restores original CWD and clears caches.
```

---

## Skill Tool

A meta-tool that invokes user-defined or bundled "skills" (slash commands):

```
When users reference a "slash command" or "/<something>" (e.g., "/commit",
"/review-pr"), they are referring to a skill. Use this tool to invoke it.

How to invoke:
- skill: "pdf"
- skill: "commit", args: "-m 'Fix bug'"
- skill: "review-pr", args: "123"
- skill: "ms-office-suite:pdf" (fully qualified name)

Important:
- Available skills are listed in system-reminder messages
- When a skill matches, this is a BLOCKING REQUIREMENT: invoke the Skill tool
  BEFORE generating any other response
- NEVER mention a skill without actually calling this tool
- Do not invoke a skill that is already running
```

---

## ToolSearch Tool

A bootstrapping tool for deferred/lazy-loaded tools:

```
Fetches full schema definitions for deferred tools so they can be called.

Deferred tools appear by name in <system-reminder> messages. Until fetched,
only the name is known — there is no parameter schema, so the tool cannot
be invoked.

Query forms:
- "select:Read,Edit,Grep" — fetch exact tools by name
- "notebook jupyter" — keyword search
- "+slack send" — require "slack" in name, rank by remaining terms
```

### Deferral Logic
Tools are deferred if:
- MCP tools (always deferred — workflow-specific)
- Tools with `shouldDefer: true`
- NEVER deferred: ToolSearch itself, Agent tool (when fork enabled), Brief tool

---

## Config Tool

Dynamically generates its prompt from the settings registry, splitting settings into Global (`~/.claude.json`) and Project (`settings.json`) sections. Model options generated from runtime provider data.

---

## SendMessage Tool

For multi-agent communication:

```
| to          |                                                           |
|-------------|-----------------------------------------------------------|
| "researcher"| Teammate by name                                          |
| "*"         | Broadcast to all (expensive, use sparingly)               |

Your plain text output is NOT visible to other agents — to communicate,
you MUST call this tool.
```

### Protocol Responses (Legacy)
Handles structured shutdown/plan approval protocol:
```json
{"to": "team-lead", "message": {"type": "shutdown_response", "request_id": "...", "approve": true}}
```

---

## Sleep Tool

```
Wait for a specified duration. The user can interrupt at any time.

Use when you have nothing to do, or when you're waiting for something.

You may receive <tick> prompts — periodic check-ins. Look for useful work first.

Prefer this over Bash(sleep ...) — it doesn't hold a shell process.

Each wake-up costs an API call, but the prompt cache expires after 5 minutes —
balance accordingly.
```

---

## Cron/Schedule Tools

```
Create (CronCreate), Delete (CronDelete), List (CronList) scheduled tasks.

Cron uses 5-field syntax in local timezone.
Supports one-shot and recurring jobs.

Key detail: avoid :00 and :30 minutes to spread load.
Recurring tasks auto-expire after default max age.
```

---

## MCP Tools (ListMcpResources, ReadMcpResource)

### ListMcpResources
```
Lists available MCP resources. Optional server parameter to filter.
Each resource includes a server field.
```

### ReadMcpResource
```
Reads a specific MCP resource by server (required) and uri (required).
```

### MCPTool (Dynamic)
The MCP tool prompt is empty in the source — it's overridden dynamically per-server in `mcpClient.ts` with server-specific descriptions.

---

## Notebook Edit Tool

```
Replaces the contents of a specific cell in a Jupyter notebook (.ipynb).
notebook_path must be absolute. cell_number is 0-indexed.
edit_mode=insert for new cells, edit_mode=delete to remove cells.
```

---

## Key Takeaways for Agent Builders

### Deferred Tool Loading
ToolSearch implements lazy loading for the LLM — tools exist by name only until the model explicitly requests their schemas. This reduces prompt size while maintaining discoverability.

### Skill as Meta-Tool
The Skill tool is a powerful abstraction — user-defined plugins invoked through a single tool interface. The "BLOCKING REQUIREMENT" ensures skills execute rather than being described.

### Communication Visibility Gap
In multi-agent mode, plain text output is explicitly NOT visible to other agents. This forces communication through the SendMessage tool, preventing the common error of assuming other agents can see your output.

### Sleep Tool Economics
The prompt explicitly mentions the cost tradeoff: "Each wake-up costs an API call, but the prompt cache expires after 5 minutes." This teaches the model to optimize its sleep duration.
