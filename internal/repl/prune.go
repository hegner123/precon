package repl

import (
	"encoding/json"

	"github.com/hegner123/precon/internal/api"
)

// pruneL1 removes low-relevance messages from L1 when the estimated token count
// exceeds the budget. Preserves: (a) the last 6 message pairs (12 messages),
// (b) any message containing tool_use or tool_result blocks.
// Pruned messages are already in L2 from persistTurn.
func (r *REPL) pruneL1() {
	budget := r.config.MaxContextTokens - r.config.ResponseReserve
	estimated := r.estimateL1Tokens()

	if estimated <= budget {
		return
	}

	r.log.Info("L1 pruning triggered",
		"estimated_tokens", estimated,
		"budget", budget,
		"messages", len(r.messages),
	)

	// Protect the last 12 messages (6 pairs) — these are the most recent context
	protectedTail := min(12, len(r.messages))

	// Walk from oldest to newest (excluding protected tail), removing messages
	// that don't contain tool_use or tool_result blocks
	pruneEnd := len(r.messages) - protectedTail
	var kept []api.MessageParam
	pruned := 0

	for i, msg := range r.messages {
		if i >= pruneEnd {
			// In protected tail — always keep
			kept = append(kept, msg)
			continue
		}

		if hasToolContent(msg) {
			// Contains tool_use or tool_result — keep
			kept = append(kept, msg)
			continue
		}

		// This message is prunable
		pruned++
		// Re-check if we're back under budget
		estimated -= estimateMessageTokens(msg)
		if estimated <= budget {
			// Under budget — keep the rest
			kept = append(kept, r.messages[i+1:]...)
			break
		}
	}

	if pruned > 0 {
		r.messages = kept
		r.log.Info("L1 pruning complete",
			"pruned", pruned,
			"remaining", len(r.messages),
			"estimated_tokens", r.estimateL1Tokens(),
		)
	}
}

// estimateL1Tokens estimates the total token count of L1 messages.
// Uses character-based estimation (4 chars per token) for per-message decisions.
func (r *REPL) estimateL1Tokens() int {
	var total int
	for _, msg := range r.messages {
		total += estimateMessageTokens(msg)
	}
	return total
}

// estimateMessageTokens estimates tokens for a single message.
func estimateMessageTokens(msg api.MessageParam) int {
	switch content := msg.Content.(type) {
	case string:
		return len(content) / 4
	case []any:
		total := 0
		for _, block := range content {
			switch b := block.(type) {
			case api.TextBlockParam:
				total += len(b.Text) / 4
			case api.ToolResultBlockParam:
				if s, ok := b.Content.(string); ok {
					total += len(s) / 4
				}
			case api.ToolUseBlockParam:
				// Marshal of map[string]any from API response — failure means zero token estimate for this block
				inputJSON, _ := json.Marshal(b.Input)
				total += len(string(inputJSON)) / 4
			}
		}
		return total
	default:
		return 0
	}
}

// hasToolContent returns true if the message contains tool_use or tool_result blocks.
func hasToolContent(msg api.MessageParam) bool {
	blocks, ok := msg.Content.([]any)
	if !ok {
		return false
	}
	for _, block := range blocks {
		switch block.(type) {
		case api.ToolUseBlockParam:
			return true
		case api.ToolResultBlockParam:
			return true
		}
	}
	return false
}
