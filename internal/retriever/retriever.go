// Package retriever implements the pre-prompt retrieval agent.
//
// The retriever is an unconscious (autonomic) agent that fires on every user prompt.
// It queries L2 (hot storage), L3 (summaries), and L4 (semantic storage) for content
// relevant to the incoming prompt. It does not make judgment calls about relevance
// weighting — that's the synthesizer's job. The retriever casts a wide net.
//
// Design: pure DB query agent. Fast, deterministic, exhaustive.
package retriever

import (
	"context"
	"log/slog"

	"github.com/hegner123/precon/internal/tier"
)

// Retriever queries memory tiers for content relevant to a user prompt.
type Retriever struct {
	log   *slog.Logger
	tiers []tier.Store // L2, L3, L4 stores to query
	limit int          // Max results per tier
}

// New creates a Retriever that queries the given tier stores.
func New(log *slog.Logger, limit int, tiers ...tier.Store) *Retriever {
	return &Retriever{
		log:   log,
		tiers: tiers,
		limit: limit,
	}
}

// Retrieve queries all configured tiers for content relevant to the prompt.
// Returns results from all tiers, ordered by score within each tier.
// The synthesizer is responsible for merging and compressing these results.
func (r *Retriever) Retrieve(ctx context.Context, prompt string) ([]tier.RetrievalResult, error) {
	var all []tier.RetrievalResult

	for _, store := range r.tiers {
		results, err := store.Query(ctx, prompt, r.limit)
		if err != nil {
			r.log.Warn("tier query failed", "tier", store.Level(), "error", err)
			continue
		}
		all = append(all, results...)
	}

	return all, nil
}
