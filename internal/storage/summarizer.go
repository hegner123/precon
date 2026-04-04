package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hegner123/precon/internal/tier"
)

// SummarizerLLM is the interface for the LLM used to generate summaries.
type SummarizerLLM interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// Summarizer generates L3 summaries during L2→L4 eviction.
type Summarizer struct {
	llm SummarizerLLM
	log *slog.Logger
}

// NewSummarizer creates a Summarizer with the given LLM and logger.
func NewSummarizer(llm SummarizerLLM, log *slog.Logger) *Summarizer {
	return &Summarizer{
		llm: llm,
		log: log,
	}
}

// summarizerSystemPrompt is the system prompt sent to Haiku for summary generation.
// Output format: line 1 = summary, line 2 = comma-separated keywords. Parsed by parseSummaryResponse.
const summarizerSystemPrompt = `Generate a summary and keywords for this memory that is being archived from hot storage.

Output exactly two lines, nothing else:
Line 1: A 2-3 sentence summary preserving key facts (names, decisions, file paths, error messages). Write it so someone searching for this content later can determine relevance from the summary alone.
Line 2: 3-5 comma-separated keywords for full-text search. Use specific terms (function names, package names, error codes) over generic ones (code, bug, fix).

Do not include labels like "Summary:" or "Keywords:" -- just the raw content on each line. Do not output anything before line 1 or after line 2.`

// fallbackSummaryLen is the max characters taken from content when LLM summarization fails.
const fallbackSummaryLen = 200

// Summarize generates a summary and keywords for a Memory being evicted from L2.
// On LLM failure, falls back to truncating the first 200 chars as summary and
// using the memory's existing keywords.
func (s *Summarizer) Summarize(ctx context.Context, mem tier.Memory) (string, []string, error) {
	resp, err := s.llm.Complete(ctx, summarizerSystemPrompt, mem.Content)
	if err != nil {
		s.log.Warn("LLM summarization failed, using fallback",
			"id", mem.ID,
			"error", err,
		)
		return fallbackSummary(mem), mem.Keywords, nil
	}

	summary, keywords, parseErr := parseSummaryResponse(resp)
	if parseErr != nil {
		s.log.Warn("failed to parse LLM summary response, using fallback",
			"id", mem.ID,
			"error", parseErr,
		)
		return fallbackSummary(mem), mem.Keywords, nil
	}

	s.log.Debug("generated summary",
		"id", mem.ID,
		"summary_len", len(summary),
		"keywords", len(keywords),
	)

	return summary, keywords, nil
}

// parseSummaryResponse extracts summary text and keywords from the LLM response.
// Expected format: first line is the summary, second line is comma-separated keywords.
func parseSummaryResponse(resp string) (string, []string, error) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return "", nil, fmt.Errorf("empty LLM response")
	}

	lines := strings.SplitN(resp, "\n", 2)
	summary := strings.TrimSpace(lines[0])
	if summary == "" {
		return "", nil, fmt.Errorf("empty summary line in LLM response")
	}

	if len(lines) < 2 {
		return summary, nil, nil
	}

	rawKeywords := strings.TrimSpace(lines[1])
	if rawKeywords == "" {
		return summary, nil, nil
	}

	parts := strings.Split(rawKeywords, ",")
	keywords := make([]string, 0, len(parts))
	for _, p := range parts {
		kw := strings.TrimSpace(p)
		if kw != "" {
			keywords = append(keywords, kw)
		}
	}

	return summary, keywords, nil
}

// fallbackSummary truncates the memory content to fallbackSummaryLen characters.
func fallbackSummary(mem tier.Memory) string {
	content := mem.Content
	if len(content) > fallbackSummaryLen {
		content = content[:fallbackSummaryLen]
	}
	return content
}
