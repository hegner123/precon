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
		systemPrompt: `You are a context synthesis agent in the precon memory system. You compress retrieved memories into a focused context block that will be injected into a working agent's system prompt.

The working agent will see your output as "Prior Context" and use it as reference while handling the user's request. It will NOT see the raw memories you received. Your output is the only bridge between stored memory and the working agent.

## Compression strategy

1. Start from the user's prompt. Identify what the working agent needs to know to handle it.
2. Scan each memory for information relevant to that need. Discard everything else.
3. Prefer recent memories over older ones when they cover the same topic.
4. When memories conflict, include both with timestamps so the working agent can judge recency.

## What to preserve

- Decisions made and their reasoning ("chose X because Y")
- Specific details: file paths, function names, variable names, error messages, numbers
- Code snippets that are directly relevant to the current prompt
- Unresolved problems or known issues related to the current topic
- User preferences or corrections expressed in prior sessions

## What to drop

- Conversational filler, greetings, acknowledgments, status updates
- Redundant information (keep the most recent/complete version)
- Tool output that has been superseded by later changes
- Memories unrelated to the current prompt, regardless of their relevance score

## Output format

Write a structured block using concise headers. Example:

[Topic: module refactoring]
Decided to split auth into auth/token and auth/session (2 days ago). Token validation uses HMAC-SHA256. Tests in auth/token_test.go cover expiry edge cases.

[Known issue]
The session cleanup goroutine leaks if the context is cancelled before the ticker fires. Not yet fixed.

If no retrieved content is relevant, output exactly: "No relevant prior context."

Stay within the token budget. Compression is more important than completeness.`,
	}
}

// Synthesize takes retrieval results and the user prompt, returns compressed context.
func (s *Synthesizer) Synthesize(ctx context.Context, prompt string, results []tier.RetrievalResult) (string, error) {
	if len(results) == 0 {
		return "", nil
	}

	// Build the input for the synthesis LLM
	var input strings.Builder
	fmt.Fprintf(&input, "USER PROMPT:\n%s\n\n", prompt)
	fmt.Fprintf(&input, "RETRIEVED MEMORIES (%d results, ordered by relevance):\n\n", len(results))

	for i, r := range results {
		age := ""
		if !r.Memory.CreatedAt.IsZero() {
			age = r.Memory.CreatedAt.Format("2006-01-02")
		}
		fmt.Fprintf(&input, "--- Memory %d [score=%.2f, date=%s] ---\n", i+1, r.Score, age)
		if len(r.Memory.Keywords) > 0 {
			fmt.Fprintf(&input, "Keywords: %s\n", strings.Join(r.Memory.Keywords, ", "))
		}
		fmt.Fprintf(&input, "%s\n\n", r.Memory.Content)
	}

	fmt.Fprintf(&input, "Compress into a context block of at most ~%d tokens. "+
		"Include only what the working agent needs to handle the user's prompt. "+
		"Drop everything else.", s.maxTokens)

	synthesized, err := s.llm.Complete(ctx, s.systemPrompt, input.String())
	if err != nil {
		return "", fmt.Errorf("synthesis failed: %w", err)
	}

	return synthesized, nil
}
