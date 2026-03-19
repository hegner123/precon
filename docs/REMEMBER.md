
  Good question. A few more systematic approaches:

  Source/session filtering — Tag memories with a session ID. Never return memories from the
  current session during tool appending — those are already in the conversation window by
  definition.

  Content-type prioritization — Not all memories are equal. Rank by type: decisions > failed
  approaches > conventions/patterns > project state summaries > raw tool output. Only surface the
  high-value types during agentic loops.

  Entity affinity — When I'm working on  store.go , surface memories tagged with that file or
  package from previous sessions. "Last time you modified the store, the GetTask index math was
  off" is high value. Generic project summaries are low value.

  Semantic dedup against visible context — Before injecting, embed the candidate memory and
  compare against what's already in the conversation window. If similarity is above a threshold,
  skip it.

  Token budget awareness — Only inject when the context window has headroom. If I'm deep in a long
  agentic loop and context is getting tight, memories are competing with the actual work product.

  Frequency cap per loop — At most N memory injections per agentic work loop (say, across a chain
  of tool calls between user messages). Prevents accumulation.

  Compaction over retrieval — Instead of returning 3 related raw memories, synthesize them into
  one concise statement. "The store uses slice-based ID lookup where index = ID-1. Milestones use
  *int for nullable FK. Validation happens in model, not store."

  Implicit feedback — If a memory was surfaced and the agent never referenced it or changed
  behavior because of it, lower its retrieval score for similar future contexts.
  The highest-leverage combination is probably: session filtering + entity affinity + content-type
  prioritization. That alone would eliminate most of the noise I saw today.
