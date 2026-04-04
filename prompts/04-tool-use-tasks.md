# 04 — Task Management Tool Prompts

> Source: `src/tools/TodoWriteTool/prompt.ts`, `src/tools/Task{Create,Get,List,Update,Stop}Tool/prompt.ts`

Two parallel task systems exist: **TodoWrite** (simpler, single-agent) and **Task*** tools (richer, multi-agent with dependencies and ownership).

---

## TodoWrite Tool

The most heavily example-driven prompt in the system (~200 lines). Uses extensive positive and negative examples to calibrate when the model should/shouldn't create todos.

### When to Use
```
1. Complex multi-step tasks — 3+ distinct steps
2. Non-trivial tasks requiring careful planning
3. User explicitly requests todo list
4. User provides multiple tasks (numbered or comma-separated)
5. After receiving new instructions — immediately capture requirements
6. When starting work — mark in_progress BEFORE beginning
7. After completing a task — mark completed, add follow-up tasks
```

### When NOT to Use
```
1. Single, straightforward task
2. Trivial task with no organizational benefit
3. Completable in less than 3 trivial steps
4. Purely conversational or informational
```

### Task States
```
- pending: Not yet started
- in_progress: Currently working (limit to ONE at a time)
- completed: Finished successfully
```

### Dual-Form Requirement
Every task requires TWO descriptions:
- `content`: Imperative form ("Run tests", "Build the project")
- `activeForm`: Present continuous form ("Running tests", "Building the project")

### Completion Requirements
```
ONLY mark a task as completed when you have FULLY accomplished it.
Never mark completed if:
- Tests are failing
- Implementation is partial
- You encountered unresolved errors
- You couldn't find necessary files or dependencies
```

### Positive Example: Feature Implementation
```
User: I want to add a dark mode toggle. Make sure you run the tests!
→ Creates todo list:
  1. Creating dark mode toggle component
  2. Adding dark mode state management
  3. Implementing CSS-in-JS styles
  4. Updating components for theme switching
  5. Running tests and build, addressing failures
```

### Positive Example: Scope Discovery
```
User: Help me rename getCwd to getCurrentWorkingDirectory
→ First searches to understand scope
→ Finds 15 instances across 8 files
→ Creates todo list with specific items per file
```

### Negative Example: Simple Question
```
User: How do I print 'Hello World' in Python?
→ Answers directly, no todo list
```

### Negative Example: Single File Edit
```
User: Can you add a comment to the calculateTotal function?
→ Does it directly, no todo list
```

---

## TaskCreate Tool

Richer than TodoWrite, with dependency support and multi-agent awareness:

### Fields
- `subject`: Brief, actionable title in imperative form
- `description`: Detailed requirements
- `activeForm` (optional): Present continuous form for spinner display

### Multi-Agent Context
When agent swarms are enabled:
```
- Include enough detail in the description for another agent to understand
- New tasks are created with status 'pending' and no owner
- Use TaskUpdate with the owner parameter to assign them
```

---

## TaskGet Tool

```
Use this tool to retrieve a task by its ID.

When to use:
- Before starting work on a task — get full description and context
- To understand dependencies (what it blocks, what blocks it)
- After being assigned a task — get complete requirements
```

---

## TaskList Tool

### Output per Task
- `id`: Task identifier
- `subject`: Brief description
- `status`: pending / in_progress / completed
- `owner`: Agent ID if assigned, empty if available
- `blockedBy`: Task IDs that must resolve first

### Teammate Workflow
```
1. After completing current task, call TaskList to find available work
2. Look for tasks with status 'pending', no owner, empty blockedBy
3. Prefer tasks in ID order (lowest first) — earlier tasks set up context
4. Claim with TaskUpdate (set owner to your name)
5. If blocked, help resolve blocking tasks or notify team lead
```

---

## TaskUpdate Tool

### Updateable Fields
- `status`: pending → in_progress → completed (or `deleted` to remove)
- `subject`, `description`, `activeForm`
- `owner`: Agent name
- `metadata`: Merge keys (set to null to delete)
- `addBlocks`, `addBlockedBy`: Dependency management

### Staleness Warning
```
Make sure to read a task's latest state using TaskGet before updating it.
```

---

## TaskStop Tool

```
- Stops a running background task by its ID
- Returns a success or failure status
- Use when you need to terminate a long-running task
```

---

## Key Takeaways for Agent Builders

### Example-Driven Calibration
The TodoWrite prompt uses 9 examples (5 positive, 4 negative) to teach the model when task tracking adds value vs. when it's overhead. This is more effective than rules alone.

### Dual-Form Labels
Requiring both imperative ("Run tests") and progressive ("Running tests") forms enables better UI — the progressive form drives spinner text while work is happening.

### Completion Gating
The explicit "never mark completed if tests are failing" rule prevents the common failure mode of premature task closure.

### ID-Order Heuristic
"Prefer tasks in ID order" is a simple but effective heuristic — earlier tasks often establish context that later tasks depend on, even without explicit dependency edges.

### Separation of Concerns
TodoWrite (simple session todos) vs. Task* tools (rich project management) serves different use cases. Simple tasks get the lightweight path; complex multi-agent projects get the full dependency graph.
