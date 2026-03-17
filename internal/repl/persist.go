package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/tier"
)

// toolRecord captures a single tool call for persistence.
type toolRecord struct {
	Name   string
	Input  map[string]any
	Output string
	Error  string
}

// turnContext holds everything from a single turn for L2 persistence.
type turnContext struct {
	UserMessage string
	Thinking    string
	Tools       []toolRecord
	Response    string
	Usage       api.Usage
}

// persistTurn saves the full turn context to L2 as categorized memories.
// Each category (user message, thinking, tool interactions, response) is stored
// as a separate memory so the retriever can surface the most relevant slice.
// This is the raw/immediate persistence — the background persister adds smart analysis.
func (r *REPL) persistTurn(ctx context.Context, tc turnContext) {
	now := time.Now()
	convID := r.config.ConversationID
	var stored int

	// 1. User message
	if tc.UserMessage != "" {
		mem := &tier.Memory{
			ID:             uuid.New().String(),
			ConversationID: convID,
			Content:        tc.UserMessage,
			TokenCount:     len(tc.UserMessage) / 4,
			Relevance:      1.0,
			Tier:           tier.L2,
			Keywords:       []string{"user", "prompt"},
			CreatedAt:      now,
			LastAccessedAt: now,
		}
		if err := r.l2.Store(ctx, mem); err != nil {
			r.log.Warn("L2 persist failed", "category", "user", "error", err)
		} else {
			stored++
		}
	}

	// 2. Thinking — the agent's reasoning process
	if tc.Thinking != "" {
		mem := &tier.Memory{
			ID:             uuid.New().String(),
			ConversationID: convID,
			Content:        tc.Thinking,
			TokenCount:     len(tc.Thinking) / 4,
			Relevance:      0.8,
			Tier:           tier.L2,
			Keywords:       []string{"thinking", "reasoning"},
			CreatedAt:      now,
			LastAccessedAt: now,
		}
		if err := r.l2.Store(ctx, mem); err != nil {
			r.log.Warn("L2 persist failed", "category", "thinking", "error", err)
		} else {
			stored++
		}
	}

	// 3. Tool interactions — each tool call + result as a combined memory
	for _, t := range tc.Tools {
		var content strings.Builder
		fmt.Fprintf(&content, "Tool: %s\n", t.Name)
		if len(t.Input) > 0 {
			// Best-effort JSON for storage — empty string on failure
			inputJSON, _ := json.Marshal(t.Input)
			fmt.Fprintf(&content, "Input: %s\n", string(inputJSON))
		}
		if t.Error != "" {
			fmt.Fprintf(&content, "Error: %s\n", t.Error)
		} else {
			// Truncate large tool outputs for storage
			output := t.Output
			if len(output) > 4000 {
				output = output[:4000] + "\n... (truncated for storage)"
			}
			fmt.Fprintf(&content, "Output: %s\n", output)
		}

		mem := &tier.Memory{
			ID:             uuid.New().String(),
			ConversationID: convID,
			Content:        content.String(),
			TokenCount:     content.Len() / 4,
			Relevance:      0.9,
			Tier:           tier.L2,
			Keywords:       []string{"tool", t.Name},
			CreatedAt:      now,
			LastAccessedAt: now,
		}
		if err := r.l2.Store(ctx, mem); err != nil {
			r.log.Warn("L2 persist failed", "category", "tool", "tool", t.Name, "error", err)
		} else {
			stored++
		}
	}

	// 4. Final response
	if tc.Response != "" {
		mem := &tier.Memory{
			ID:             uuid.New().String(),
			ConversationID: convID,
			Content:        tc.Response,
			TokenCount:     tc.Usage.OutputTokens,
			Relevance:      1.0,
			Tier:           tier.L2,
			Keywords:       []string{"assistant", "response"},
			CreatedAt:      now,
			LastAccessedAt: now,
		}
		if err := r.l2.Store(ctx, mem); err != nil {
			r.log.Warn("L2 persist failed", "category", "response", "error", err)
		} else {
			stored++
		}
	}

	r.log.Debug("persisted turn to L2",
		"conversation", convID,
		"memories", stored,
		"thinking", tc.Thinking != "",
		"tools", len(tc.Tools),
	)
}

// runBackgroundPersister runs the Haiku-based persister on the engine context.
// This runs in a goroutine and survives request cancellation.
func (r *REPL) runBackgroundPersister(tc turnContext) {
	start := time.Now()

	// Collect tool result summaries for the persister
	var toolSummaries []string
	for _, t := range tc.Tools {
		var summary strings.Builder
		fmt.Fprintf(&summary, "Tool: %s", t.Name)
		if t.Error != "" {
			fmt.Fprintf(&summary, " (error: %s)", truncate(t.Error, 200))
		} else if t.Output != "" {
			fmt.Fprintf(&summary, " → %s", truncate(t.Output, 500))
		}
		toolSummaries = append(toolSummaries, summary.String())
	}

	decision, err := r.persister.Review(r.engineCtx, tc.UserMessage, tc.Response, toolSummaries)
	if err != nil {
		r.log.Error("background persister review failed", "error", err, "duration", time.Since(start))
		return
	}

	// Apply decision (writes to L2, updates scores)
	if err := r.persister.Apply(r.engineCtx, decision, r.config.ConversationID); err != nil {
		r.log.Error("background persister apply failed", "error", err, "duration", time.Since(start))
		return
	}

	r.log.Debug("background persister complete", "duration", time.Since(start))
}
