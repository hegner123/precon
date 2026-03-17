// Package tier defines the memory tier abstraction for the Precon memory multiplex.
//
// Memory is organized into 5 tiers (L1-L5) like a CPU cache hierarchy.
// Each tier has different access speed, capacity, and query characteristics.
// Content flows between tiers based on relevance and recency.
package tier

import (
	"context"
	"time"
)

// Level identifies a memory tier.
type Level int

const (
	L1 Level = iota + 1 // Active context (in the prompt)
	L2                   // Hot storage (SQLite, queried every prompt)
	L3                   // Warm storage (summaries with pointers)
	L4                   // Semantic storage (pgvector, embedding search)
	L5                   // Cold storage (files, cloud, archive)
)

// String returns the tier name.
func (l Level) String() string {
	switch l {
	case L1:
		return "L1:Active"
	case L2:
		return "L2:Hot"
	case L3:
		return "L3:Warm"
	case L4:
		return "L4:Semantic"
	case L5:
		return "L5:Cold"
	default:
		return "Unknown"
	}
}

// Memory represents a unit of stored knowledge at any tier.
type Memory struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	TopicID        string    `json:"topic_id"`
	Content        string    `json:"content"`          // Full content (L2/L4/L5) or summary (L3)
	IsSummary      bool      `json:"is_summary"`       // True for L3 summary entries
	FullContentRef string    `json:"full_content_ref"` // L4 ID when this is an L3 summary
	TokenCount     int       `json:"token_count"`
	Relevance      float64   `json:"relevance"`        // 0.0-1.0, current relevance score
	Tier           Level     `json:"tier"`              // Which tier this memory lives in
	Keywords       []string  `json:"keywords"`
	CreatedAt      time.Time `json:"created_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"` // For recency-based eviction
	PromotedFrom   Level     `json:"promoted_from"`    // Tier it was promoted from (0 if native)
}

// RetrievalResult wraps a Memory with search metadata.
type RetrievalResult struct {
	Memory     Memory  `json:"memory"`
	Score      float64 `json:"score"`      // Search relevance (0.0-1.0)
	SourceTier Level   `json:"source_tier"`
}

// Store defines the interface for a single tier's storage backend.
// Each tier implements this interface with its own storage technology.
type Store interface {
	// Store saves a memory at this tier.
	Store(ctx context.Context, mem *Memory) error

	// Retrieve gets a memory by ID.
	Retrieve(ctx context.Context, id string) (*Memory, error)

	// Query searches this tier for memories relevant to the given prompt.
	// Returns results ordered by relevance score descending.
	Query(ctx context.Context, prompt string, limit int) ([]RetrievalResult, error)

	// Delete removes a memory from this tier (after promotion/eviction).
	Delete(ctx context.Context, id string) error

	// List returns all memories for a conversation at this tier.
	List(ctx context.Context, conversationID string) ([]Memory, error)

	// Level returns which tier this store represents.
	Level() Level
}

// EvictionPolicy decides when and what to move between tiers.
type EvictionPolicy interface {
	// ShouldEvict returns memories that should be moved to a lower tier.
	ShouldEvict(ctx context.Context, memories []Memory) []Memory

	// ShouldPromote returns memories that should be moved to a higher tier
	// based on retrieval during the current task.
	ShouldPromote(ctx context.Context, retrieved []RetrievalResult) []Memory
}
