# 09 — Decision-Making Systems

> Source: `src/utils/permissions/yoloClassifier.ts`, `src/utils/permissions/permissionExplainer.ts`, `src/constants/prompts.ts` (actions section)

---

## Auto-Mode Security Classifier

The "yolo classifier" determines whether tool calls should be automatically approved in auto/unattended mode. It's a **two-stage XML classifier** with a fallback tool-use path.

### Architecture

```
Tool call input
    │
    ▼
┌──────────────────┐
│ Stage 1: Fast    │  64 max tokens, no thinking
│ XML classifier   │  Returns: <result>ALLOW</result> or <result>DENY: reason</result>
└──────────────────┘
    │
    │ If DENY:
    ▼
┌──────────────────┐
│ Stage 2: Thinking│  4096 max tokens, chain-of-thought
│ XML classifier   │  More careful analysis with reasoning
└──────────────────┘
    │
    │ If Stage 2 fails:
    ▼
┌──────────────────┐
│ Tool-use fallback│  Forced tool choice: classify_result
│                  │  Structured {decision, reason} output
└──────────────────┘
```

### Transcript Building
The classifier receives a compact transcript of recent conversation context. Messages are serialized with role markers, and tool calls are compressed via `toAutoClassifierInput()` — each tool defines its own compact representation.

### CLAUDE.md Integration
The classifier receives cached CLAUDE.md content as a prefix message, so user-defined project rules influence classification decisions.

### User Rules
The system prompt includes user-configured allow/deny rules:
- Allow rules: patterns the user has pre-approved
- Deny rules: patterns the user has explicitly blocked
- Environment information: CWD, platform, shell

### PowerShell Deny Guidance
Additional deny rules for PowerShell-specific dangerous patterns:
- Download-and-execute chains
- Destructive operations
- Persistence mechanisms
- Privilege escalation

---

## Permission Explainer

Uses a side-query with **forced tool choice** to generate human-readable risk assessments for shell commands.

### Approach
```
Side query → Model with forced tool choice (explain_command)
           → Returns: {risk_level: LOW|MEDIUM|HIGH, explanation: string}
```

### Context
The explainer extracts recent assistant messages for context, validates responses with Zod, and logs analytics.

---

## Actions Section (Reversibility Framework)

The system prompt's "Executing actions with care" section is a decision-making framework for the model:

### Decision Criteria
```
Consider:
- Reversibility: Can this be undone?
- Blast radius: How many systems does this affect?
- Visibility: Will others see this?
- Risk level: What's the worst case?
```

### Default Behavior
```
By default, transparently communicate the action and ask for confirmation.
This default can be changed by user instructions — if explicitly asked to
operate more autonomously, proceed without confirmation.
```

### Authorization Scoping
```
A user approving an action (like a git push) once does NOT mean they approve
it in all contexts. Unless authorized in advance in durable instructions
like CLAUDE.md files, always confirm first. Authorization stands for the
scope specified, not beyond. Match the scope of your actions to what was
actually requested.
```

### Anti-Shortcut Rule
```
When you encounter an obstacle, do not use destructive actions as a shortcut.
For instance, try to identify root causes rather than bypassing safety checks.
If you discover unexpected state like unfamiliar files, branches, or configuration,
investigate before deleting or overwriting — it may represent the user's
in-progress work.
```

---

## Tool Permission Model

Every tool implements a two-phase permission check:

### Phase 1: validateInput(input, context)
Structural/mode guard. Returns `{result: true}` or `{result: false, message, errorCode}`.

### Phase 2: checkPermissions(input, context)
Security check. Returns one of:
- `allow` — proceed with optional `updatedInput`
- `ask` — prompt user with message and suggestions
- `deny` — block with message

### Permission Context
```typescript
{
  mode: 'default' | ...,
  alwaysAllowRules: Map<source, Rule[]>,
  alwaysDenyRules: Map<source, Rule[]>,
  alwaysAskRules: Map<source, Rule[]>,
  shouldAvoidPermissionPrompts: boolean  // for background agents
}
```

### Rule Sources
Rules come from multiple sources with priority:
- User settings (global, project)
- CLAUDE.md files
- Policy settings (managed by org)
- Auto-mode classifier decisions

---

## Verification Agent

For non-trivial implementations, a verification agent provides adversarial review:

```
When non-trivial implementation happens on your turn, independent adversarial
verification must happen before you report completion.

Non-trivial means: 3+ file edits, backend/API changes, or infrastructure changes.

Spawn Agent with subagent_type="verification". Pass the original user request,
all files changed, the approach, and the plan file path.

Your own checks and a fork's self-checks do NOT substitute — only the verifier
assigns a verdict.

On FAIL: fix, resume verifier with findings plus your fix, repeat until PASS.
On PASS: spot-check — re-run 2-3 commands from its report, confirm output matches.
On PARTIAL: report what passed and what could not be verified.
```

---

## Key Takeaways for Agent Builders

### Two-Stage Classification
The fast-then-careful pattern (64 tokens → 4096 tokens with thinking) minimizes latency for obvious cases while maintaining accuracy for ambiguous ones. The fallback to structured tool output provides a safety net.

### Forced Tool Choice for Structured Output
Both the permission explainer and the classifier fallback use forced tool choice to guarantee structured responses. This is more reliable than asking for JSON in free-form text.

### Authorization is Scoped
"Approval once ≠ approval always" is a critical security principle. Each approval is scoped to the specific context — not a blanket permission.

### Adversarial Verification
The verification agent pattern prevents the common failure of the model declaring success without actually verifying. The explicit "your own checks do NOT substitute" rule prevents self-validation.

### Investigate Before Destroying
The anti-shortcut rule ("investigate before deleting") prevents the common agent failure of clearing obstacles through destruction rather than understanding.
