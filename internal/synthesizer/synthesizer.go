// Package synthesizer implements the pre-prompt context synthesis agent.
//
// The synthesizer is an unconscious (autonomic) agent that takes raw retrieval
// results and compresses them into a working context injection for the working agent.
// This is where LLM reasoning lives for the pre-prompt pipeline — deciding what's
// signal vs. noise in the retrieved content.
//
// Design: aggressive compression. The retriever casts a wide net; the synthesizer
// filters hard. The working agent should receive only what it needs.
package synthesizer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hegner123/precon/internal/tier"
)

// LLM abstracts the language model used for synthesis.
type LLM interface {
	// Complete sends a prompt and returns the response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// Synthesizer compresses retrieval results into working context.
type Synthesizer struct {
	log          *slog.Logger
	llm          LLM
	maxTokens    int // Target size for synthesized context
	systemPrompt string
}

// New creates a Synthesizer with the given LLM backend and token budget.
func New(log *slog.Logger, llm LLM, maxTokens int) *Synthesizer {
	return &Synthesizer{
		log:       log,
		llm:       llm,
		maxTokens: maxTokens,
		systemPrompt: `You are a context synthesis agent. Your job is to compress retrieved memories into a concise context block for a working agent.

Rules:
- Extract only information relevant to the user's current prompt.
- Preserve specific details: names, numbers, decisions, code snippets.
- Drop conversational filler, greetings, and meta-discussion.
- Output a structured context block, not a conversation summary.
- If no retrieved content is relevant, output "No relevant prior context."
- Keep output under the token budget. Be aggressive about compression.`,
	}
}

// Synthesize takes retrieval results and the user prompt, returns compressed context.
func (s *Synthesizer) Synthesize(ctx context.Context, prompt string, results []tier.RetrievalResult) (string, error) {
	if len(results) == 0 {
		return "", nil
	}

	// Build the input for the synthesis LLM
	var input strings.Builder
	fmt.Fprintf(&input, "USER PROMPT: %s\n\n", prompt)
	fmt.Fprintf(&input, "RETRIEVED MEMORIES (%d results):\n\n", len(results))

	for i, r := range results {
		fmt.Fprintf(&input, "--- Memory %d [%s, score=%.2f] ---\n", i+1, r.SourceTier, r.Score)
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&input, "Keywords: %s\n", strings.Join(r.Memory.Keywords, ", "))
		}
		fmt.Fprintf(&input, "%s\n\n", r.Memory.Content)
	}

	fmt.Fprintf(&input, "Compress the relevant memories into a context block (max ~%d tokens). "+
		"Only include what the working agent needs to handle the user's prompt.", s.maxTokens)

	synthesized, err := s.llm.Complete(ctx, s.systemPrompt, input.String())
	if err != nil {
		return "", fmt.Errorf("synthesis failed: %w", err)
	}

	return synthesized, nil
}
