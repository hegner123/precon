package tier

import (
	"strings"
	"testing"
)

func TestFormatToolContext_Empty(t *testing.T) {
	got := FormatToolContext(nil)
	if got != "" {
		t.Errorf("FormatToolContext(nil) = %q, want empty", got)
	}

	got = FormatToolContext([]RetrievalResult{})
	if got != "" {
		t.Errorf("FormatToolContext([]) = %q, want empty", got)
	}
}

func TestFormatToolContext_SingleResult(t *testing.T) {
	results := []RetrievalResult{
		{
			Memory: Memory{
				Content:  "Eviction uses relevance × recency_decay",
				Keywords: []string{"eviction", "scoring"},
			},
			Score:      0.85,
			SourceTier: L2,
		},
	}

	got := FormatToolContext(results)

	if !strings.Contains(got, "Prior context (from memory)") {
		t.Error("missing header")
	}
	if !strings.Contains(got, "eviction, scoring") {
		t.Error("missing keywords")
	}
	if !strings.Contains(got, "Eviction uses relevance") {
		t.Error("missing content")
	}
}

func TestFormatToolContext_TruncatesLongContent(t *testing.T) {
	// Build content longer than 600 chars
	long := strings.Repeat("x", 700)
	results := []RetrievalResult{
		{
			Memory: Memory{Content: long},
			Score:  0.5,
		},
	}

	got := FormatToolContext(results)

	// Should be truncated — the full 700-char string should not appear
	if strings.Contains(got, long) {
		t.Error("content not truncated — full 700-char string found")
	}
	if !strings.Contains(got, "...") {
		t.Error("truncation marker missing")
	}
}

func TestFormatToolContext_RespectsMaxBytes(t *testing.T) {
	// Create multiple results that together exceed MaxToolContextBytes
	results := make([]RetrievalResult, 10)
	for i := range results {
		results[i] = RetrievalResult{
			Memory: Memory{
				Content:  strings.Repeat("a", 500),
				Keywords: []string{"key"},
			},
			Score: 0.5,
		}
	}

	got := FormatToolContext(results)

	if len(got) > MaxToolContextBytes+100 {
		// Allow small overhead for the header
		t.Errorf("FormatToolContext output too large: %d bytes (max %d)", len(got), MaxToolContextBytes)
	}
}

func TestFormatToolContext_MultipleResults(t *testing.T) {
	results := []RetrievalResult{
		{
			Memory: Memory{
				Content:  "First memory",
				Keywords: []string{"first"},
			},
			Score: 0.9,
		},
		{
			Memory: Memory{
				Content:  "Second memory",
				Keywords: []string{"second"},
			},
			Score: 0.7,
		},
	}

	got := FormatToolContext(results)

	if !strings.Contains(got, "First memory") {
		t.Error("missing first result")
	}
	if !strings.Contains(got, "Second memory") {
		t.Error("missing second result")
	}
}
