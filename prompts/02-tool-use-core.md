# 02 — Core Tool Prompts

> Source: `src/tools/{BashTool,FileReadTool,FileEditTool,FileWriteTool,GlobTool,GrepTool}/prompt.ts`

These are the six foundational tools. Their prompts are sent to the model as tool descriptions and define how the LLM should use each capability.

---

## Bash Tool

The most complex tool prompt (~300 lines when fully assembled). Dynamically composed at runtime based on git settings, sandbox config, feature flags, and user type.

### Core Description
```
Executes a given bash command and returns its output.

The working directory persists between commands, but shell state does not.
The shell environment is initialized from the user's profile (bash or zsh).
```

### Tool Preference Redirect
The prompt explicitly steers the model AWAY from Bash for common operations:
```
IMPORTANT: Avoid using this tool to run find, grep, cat, head, tail, sed, awk,
or echo commands, unless explicitly instructed. Instead, use the appropriate
dedicated tool:
- File search: Use Glob (NOT find or ls)
- Content search: Use Grep (NOT grep or rg)
- Read files: Use Read (NOT cat/head/tail)
- Edit files: Use Edit (NOT sed/awk)
- Write files: Use Write (NOT echo >/cat <<EOF)
- Communication: Output text directly (NOT echo/printf)
```

### Instruction Items
- Verify parent directories before creating files: "first use this tool to run `ls`"
- Quote file paths with spaces
- Maintain CWD using absolute paths, avoid `cd`
- Configurable timeout (default and max exposed to model)
- Background execution option explained

### Multiple Commands Pattern
```
- If independent and can run in parallel, make multiple Bash tool calls in a single message
- If dependent and must run sequentially, use '&&' to chain
- Use ';' only when sequential but don't care if earlier commands fail
- DO NOT use newlines to separate commands
```

### Git Safety Protocol
An extensive sub-section for git operations:
- **NEVER** update git config
- **NEVER** run destructive git commands unless explicitly requested
- **NEVER** skip hooks (--no-verify, --no-gpg-sign)
- **NEVER** force push to main/master
- **CRITICAL**: Always create NEW commits rather than amending (amend after hook failure modifies the PREVIOUS commit)
- Prefer adding specific files over `git add -A`
- **NEVER** commit unless explicitly asked

### Commit Message Protocol
Detailed multi-step workflow:
1. Parallel: `git status`, `git diff`, `git log` (recent style)
2. Analyze changes, draft commit message (focus on "why" not "what")
3. Parallel: stage files, create commit with HEREDOC, verify with `git status`
4. If pre-commit hook fails: fix and create NEW commit

### PR Creation Protocol
Similar multi-step workflow with `gh pr create` using HEREDOC format.

### Sandbox Section (Dynamic)
When sandboxing is enabled, a full sandbox configuration is appended:
- Filesystem restrictions (read deny-list, write allow-list)
- Network restrictions (allowed/denied hosts)
- `$TMPDIR` usage requirement
- When/how to bypass sandbox (`dangerouslyDisableSandbox`)

### Sleep Avoidance
```
- Do not sleep between commands that can run immediately
- If your command is long running, use run_in_background
- Do not retry failing commands in a sleep loop — diagnose the root cause
- If waiting for a background task, you will be notified when it completes
```

---

## File Read Tool

### Core Description
```
Reads a file from the local filesystem. You can access any file directly.
Assume this tool is able to read all files on the machine.
```

### Key Instructions
- File path must be absolute, not relative
- Default reads up to 2000 lines from the beginning
- Optional line offset and limit for long files
- Reads images (PNG, JPG, etc.) — multimodal support
- Reads PDFs (with `pages` parameter for large documents, max 20 pages)
- Reads Jupyter notebooks (.ipynb) — returns all cells with outputs
- Cannot read directories — use `ls` via Bash for that
- Empty files produce a system reminder warning

### Design Pattern: Gating Other Tools
The Read tool serves as a prerequisite for Edit and Write:
```
You must use your Read tool at least once in the conversation before editing.
This tool will error if you attempt an edit without reading the file.
```

---

## File Edit Tool

### Core Description
```
Performs exact string replacements in files.
```

### Key Instructions
- Must read the file first (enforced — errors if not read)
- Preserve exact indentation from Read output (tabs/spaces after line number prefix)
- Never include line number prefixes in `old_string` or `new_string`
- **Prefer editing existing files. NEVER write new files unless explicitly required.**
- The edit FAILS if `old_string` is not unique — provide more context to disambiguate
- `replace_all` for renaming variables across a file
- Minimal uniqueness hint (internal users): "Use the smallest old_string that's clearly unique — usually 2-4 adjacent lines."

---

## File Write Tool

### Core Description
```
Writes a file to the local filesystem. Overwrites the existing file if one exists.
```

### Key Instructions
- Must read existing files first (enforced)
- "Prefer the Edit tool for modifying existing files — it only sends the diff."
- "NEVER create documentation files (*.md) or README files unless explicitly requested."
- No emojis unless explicitly requested

---

## Glob Tool

Minimal prompt — the tool is simple:
```
- Fast file pattern matching tool that works with any codebase size
- Supports glob patterns like "**/*.js" or "src/**/*.ts"
- Returns matching file paths sorted by modification time
- When doing an open ended search that may require multiple rounds of
  globbing and grepping, use the Agent tool instead
```

---

## Grep Tool

```
A powerful search tool built on ripgrep

Usage:
- ALWAYS use Grep for search tasks. NEVER invoke grep or rg as a Bash command.
- Supports full regex syntax (e.g., "log.*Error", "function\\s+\\w+")
- Filter files with glob parameter or type parameter
- Output modes: "content", "files_with_matches" (default), "count"
- Use Agent tool for open-ended searches requiring multiple rounds
- Pattern syntax: Uses ripgrep — literal braces need escaping
- Multiline matching: use multiline: true for cross-line patterns
```

---

## Key Takeaways for Agent Builders

### Tool Hierarchy Pattern
Establish a clear preference order: specialized tools > general-purpose shell. Explicitly redirect the model away from shell equivalents.

### Prerequisite Enforcement
Use "must read before edit/write" gates to prevent blind modifications. This is enforced both in the prompt AND in the tool implementation.

### Git Safety as First-Class Concern
Git operations get their own detailed sub-protocol because they are high-risk, hard-to-reverse, and externally visible. The prompt addresses specific failure modes (amend after hook failure, accidental force push, committing secrets).

### Dynamic Composition
The Bash prompt is not a static string — it's assembled from ~10 sub-sections based on runtime state (sandbox config, git settings, feature flags, user type). This allows the prompt to stay relevant without bloating when features are disabled.

### Explicit Anti-Patterns
Each tool prompt doesn't just say what to do — it says what NOT to do. "Don't use cat, don't use sed, don't use find." This is essential because LLMs default to familiar shell commands.
