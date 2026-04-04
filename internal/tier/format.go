package tier

import (
	"fmt"
	"strings"
)

// FormatDirectInjection formats retrieval results for direct system prompt injection.
// Compact format: the working agent sees this as "Prior Context" in its system prompt.
// Avoids markdown headers (token-heavy) and tier implementation details (noise to the agent).
func FormatDirectInjection(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Prior Context\n\nRetrieved from previous sessions. May be outdated. Trust the user's current message over anything here.\n\n")
	for _, r := range results {
		// Date prefix for temporal awareness
		date := ""
		if !r.Memory.CreatedAt.IsZero() {
			date = r.Memory.CreatedAt.Format("Jan 02") + ": "
		}
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&b, "**[%s]** ", strings.Join(r.Memory.Keywords, ", "))
		}
		content := r.Memory.Content
		if len(content) > 2000 {
			content = content[:2000] + "... (truncated)"
		}
		fmt.Fprintf(&b, "%s%s\n\n", date, content)
	}
	return b.String()
}

// MaxToolContextBytes caps the total size of context injected into a tool response.
// ~500 tokens ≈ 2000 bytes. Keeps tool results from bloating L1.
const MaxToolContextBytes = 2000

// FormatToolContext formats retrieval results for injection into tool responses.
// Compact format: one line per memory, hard-capped at MaxToolContextBytes.
// Returns empty string if no results.
func FormatToolContext(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n---\nPrior context:\n")

	for _, r := range results {
		var entry strings.Builder
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&entry, "[%s] ", strings.Join(r.Memory.Keywords, ", "))
		}
		content := r.Memory.Content
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		entry.WriteString(content)
		entry.WriteString("\n")

		if b.Len()+entry.Len() > MaxToolContextBytes {
			break
		}
		b.WriteString(entry.String())
	}

	return b.String()
}
