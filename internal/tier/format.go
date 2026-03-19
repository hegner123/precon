package tier

import (
	"fmt"
	"strings"
)

// FormatDirectInjection formats retrieval results for direct system prompt injection.
// Top-N results are rendered as structured context for the working agent.
func FormatDirectInjection(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Prior Context (retrieved from memory)\n\n")
	for i, r := range results {
		fmt.Fprintf(&b, "### Memory %d [%s, relevance=%.2f]\n", i+1, r.SourceTier, r.Score)
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&b, "Keywords: %s\n", strings.Join(r.Memory.Keywords, ", "))
		}
		content := r.Memory.Content
		if len(content) > 2000 {
			content = content[:2000] + "\n... (truncated)"
		}
		fmt.Fprintf(&b, "%s\n\n", content)
	}
	return b.String()
}

// MaxToolContextBytes caps the total size of context injected into a tool response.
// ~500 tokens ≈ 2000 bytes. Keeps tool results from bloating L1.
const MaxToolContextBytes = 2000

// FormatToolContext formats retrieval results for injection into tool responses.
// Compact format: one block per memory, hard-capped at MaxToolContextBytes.
// Returns empty string if no results.
func FormatToolContext(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n---\nPrior context (from memory):\n")

	for _, r := range results {
		// Preview what this entry would look like
		var entry strings.Builder
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&entry, "[%s] ", strings.Join(r.Memory.Keywords, ", "))
		}
		content := r.Memory.Content
		// Per-entry cap: leave room for multiple entries within budget
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		entry.WriteString(content)
		entry.WriteString("\n")

		// Check if adding this entry would exceed budget
		if b.Len()+entry.Len() > MaxToolContextBytes {
			break
		}
		b.WriteString(entry.String())
	}

	return b.String()
}
