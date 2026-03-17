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
