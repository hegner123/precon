# 08 — Multi-Agent Orchestration

> Source: `src/tools/TeamCreateTool/prompt.ts`, `src/tools/TeamDeleteTool/prompt.ts`, `src/tools/SendMessageTool/prompt.ts`, `src/utils/swarm/teammatePromptAddendum.ts`

---

## Team Creation

### When to Create a Team
```
Use TeamCreate proactively whenever:
- The user explicitly asks to use a team, swarm, or group of agents
- The user mentions wanting agents to work together or collaborate
- A task is complex enough to benefit from parallel work (e.g., full-stack
  feature with frontend and backend, refactoring while keeping tests passing,
  multi-step project with research/planning/coding phases)

When in doubt about whether a task warrants a team, prefer spawning a team.
```

### Agent Type Selection
```
When spawning teammates, choose the subagent_type based on needed tools:
- Read-only agents (Explore, Plan) cannot edit files. Only assign research tasks.
- Full-capability agents have all tools. Use for implementation.
- Custom agents in .claude/agents/ may have restrictions — check descriptions.
```

### Team Workflow
```
1. Create team with TeamCreate
2. Create tasks using Task tools
3. Spawn teammates using Agent tool with team_name and name
4. Assign tasks with TaskUpdate (set owner)
5. Teammates work and mark tasks completed
6. Teammates go idle between turns (this is normal)
7. Shutdown team via SendMessage with shutdown_request
```

---

## Communication Model

### Visibility Rule
```
Your plain text output is NOT visible to other agents — to communicate,
you MUST call SendMessage.
```

This is reinforced in the teammate addendum:
```
IMPORTANT: You are running as an agent in a team. To communicate:
- Use SendMessage with to: "<name>" for specific teammates
- Use SendMessage with to: "*" sparingly for broadcasts

Just writing a response in text is not visible to others — you MUST use SendMessage.
```

### Message Delivery
```
Messages from teammates are automatically delivered to you. You do NOT need
to manually check your inbox.

- They appear automatically as new conversation turns
- If you're busy, messages are queued and delivered when your turn ends
- The UI shows a brief notification with sender's name
```

### Idle State Understanding
```
Teammates go idle after every turn — this is completely normal.
A teammate going idle immediately after sending a message does NOT mean
they are done or unavailable.

- Idle teammates can receive messages (sending wakes them up)
- Idle notifications are automatic
- Do not treat idle as an error
- Peer DM visibility: brief summaries included in idle notifications
```

---

## Task Coordination

### Shared Task List
Teams share a task list at `~/.claude/tasks/{team-name}/`.

### Teammate Workflow
```
1. Check TaskList periodically, especially after completing each task
2. Look for tasks with status 'pending', no owner, empty blockedBy
3. Prefer tasks in ID order (lowest first)
4. Claim tasks with TaskUpdate (set owner to your name)
5. Create new tasks with TaskCreate when identifying additional work
6. Mark completed with TaskUpdate, then check TaskList for next work
7. If all tasks blocked, notify team lead or help resolve blockers
```

### Communication Anti-Patterns
```
- Do not use terminal tools to view team activity — always send a message
- Your team cannot hear you without SendMessage
- Do NOT send structured JSON status messages — use plain text
- Use TaskUpdate to mark tasks completed (not messages)
```

---

## Team Discovery

```
Teammates can read the team config to discover other members:
~/.claude/teams/{team-name}/config.json

Config contains a members array with:
- name: Human-readable name (ALWAYS use for messaging and assignment)
- agentId: Unique identifier (reference only — do not use for communication)
- agentType: Role/type
```

---

## Team Cleanup

### TeamDelete
```
Remove team and task directories when work is complete.

IMPORTANT: TeamDelete will fail if the team still has active members.
Gracefully terminate teammates first, then call TeamDelete after all
have shut down.
```

---

## SendMessage Protocol

### Addressing
```
| "researcher"  | Teammate by name                                    |
| "*"           | Broadcast (expensive — linear in team size)         |
```

### Shutdown Protocol
```json
{"to": "team-lead", "message": {"type": "shutdown_response", "request_id": "...", "approve": true}}
```
"Approving shutdown terminates your process."

### Plan Approval Protocol
```json
{"to": "researcher", "message": {"type": "plan_approval_response", "request_id": "...", "approve": false, "feedback": "add error handling"}}
```
"Rejecting plan sends the teammate back to revise."

---

## Key Takeaways for Agent Builders

### Explicit Visibility Model
The most important multi-agent lesson: text output is NOT communication. The prompt hammers this point repeatedly because it's the most common failure mode in multi-agent systems.

### Idle is Normal
Agents go idle between turns. The prompt explicitly trains the model to understand this is expected behavior, not an error. Without this, the coordinator wastes tokens checking on idle agents.

### Name-Based Addressing
Agents use human-readable names for all communication, not UUIDs. This makes the conversation readable and debuggable.

### Task-Driven Coordination
Communication happens through the task system (shared state) rather than ad-hoc messages. This provides observability and prevents information loss.

### Shutdown as Protocol
Graceful shutdown is a first-class protocol operation (shutdown_request → shutdown_response), not an implicit "just stop." This prevents orphaned agents and ensures cleanup.

### Team = Task List
The 1:1 correspondence between teams and task lists simplifies the mental model. Creating a team automatically creates its task infrastructure.
