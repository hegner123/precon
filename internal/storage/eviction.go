package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/hegner123/precon/internal/embedding"
	"github.com/hegner123/precon/internal/tier"
)

// L2MaxTokens is the default threshold at which L2 eviction triggers.
const L2MaxTokens = 500000

// L2TargetPercent is the target occupancy after eviction (80% of threshold).
const L2TargetPercent = 0.80

// Evictor manages the L2→L4 eviction pipeline.
// When L2 total tokens exceed the threshold, the lowest-scoring memories are
// embedded and moved to L4, then deleted from L2. If an L3 store and summarizer
// are configured, generates summaries during eviction.
type Evictor struct {
	l2         *SQLiteStore
	l4         *PgvectorStore
	l3         *L3Store    // optional — summaries generated during eviction
	summarizer *Summarizer // optional — generates L3 summaries
	embedC     *embedding.Client
	log        *slog.Logger
	threshold  int64 // L2 token budget
}

// NewEvictor creates an evictor for the L2→L4 pipeline.
func NewEvictor(l2 *SQLiteStore, l4 *PgvectorStore, embedClient *embedding.Client, log *slog.Logger) *Evictor {
	return &Evictor{
		l2:        l2,
		l4:        l4,
		embedC:    embedClient,
		log:       log,
		threshold: L2MaxTokens,
	}
}

// SetL3 enables summary generation during eviction.
func (e *Evictor) SetL3(l3 *L3Store, summarizer *Summarizer) {
	e.l3 = l3
	e.summarizer = summarizer
}

// SetThreshold overrides the default L2 token threshold.
func (e *Evictor) SetThreshold(tokens int64) {
	e.threshold = tokens
}

// CheckAndEvict checks if L2 exceeds its token budget and evicts if needed.
// Returns the number of memories evicted.
func (e *Evictor) CheckAndEvict(ctx context.Context, conversationID string) (int, error) {
	start := time.Now()

	// Get total L2 tokens
	total, err := e.l2TotalTokens(ctx)
	if err != nil {
		return 0, fmt.Errorf("check L2 tokens: %w", err)
	}

	if total <= e.threshold {
		e.log.Debug("L2 within budget", "tokens", total, "threshold", e.threshold)
		return 0, nil
	}

	e.log.Info("L2 eviction triggered",
		"tokens", total,
		"threshold", e.threshold,
	)

	// Target: 80% of threshold
	target := int64(float64(e.threshold) * L2TargetPercent)
	tokensToEvict := total - target

	// Get all L2 memories, sorted by eviction priority (lowest score first)
	candidates, err := e.l2.List(ctx, conversationID)
	if err != nil {
		return 0, fmt.Errorf("list L2 memories for eviction: %w", err)
	}

	// Sort by eviction score: relevance * recency_decay (lowest first = evict first)
	now := time.Now()
	sort.Slice(candidates, func(i, j int) bool {
		return evictionScore(candidates[i], now) < evictionScore(candidates[j], now)
	})

	// Collect memories to evict until we've freed enough tokens
	var toEvict []tier.Memory
	var freedTokens int64
	for _, mem := range candidates {
		if freedTokens >= tokensToEvict {
			break
		}
		toEvict = append(toEvict, mem)
		freedTokens += int64(mem.TokenCount)
	}

	if len(toEvict) == 0 {
		return 0, nil
	}

	// Batch embed all content
	texts := make([]string, len(toEvict))
	for i, mem := range toEvict {
		texts[i] = mem.Content
	}

	embeddings, err := e.embedC.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("batch embed for eviction: %w", err)
	}

	// Move each memory: store in L4, delete from L2
	evicted := 0
	for i, mem := range toEvict {
		mem.Tier = tier.L4

		if err := e.l4.StoreWithEmbedding(ctx, &mem, embeddings[i]); err != nil {
			e.log.Error("L4 store during eviction failed", "id", mem.ID, "error", err)
			continue
		}

		if err := e.l2.Delete(ctx, mem.ID); err != nil {
			e.log.Error("L2 delete during eviction failed — memory exists in both L2 and L4",
				"id", mem.ID, "error", err)
			continue
		}

		// Generate L3 summary if configured
		if e.l3 != nil && e.summarizer != nil {
			summaryText, keywords, summaryErr := e.summarizer.Summarize(ctx, mem)
			if summaryErr != nil {
				e.log.Warn("L3 summary generation failed", "id", mem.ID, "error", summaryErr)
			} else {
				summaryMem := &tier.Memory{
					ID:             "summary-" + mem.ID,
					ConversationID: mem.ConversationID,
					Content:        summaryText,
					IsSummary:      true,
					FullContentRef: mem.ID, // points to L4 entry
					TokenCount:     len(summaryText) / 4,
					Relevance:      mem.Relevance,
					Tier:           tier.L3,
					Keywords:       keywords,
					CreatedAt:      mem.CreatedAt,
					LastAccessedAt: time.Now(),
				}
				if storeErr := e.l3.Store(ctx, summaryMem); storeErr != nil {
					e.log.Warn("L3 summary store failed", "id", mem.ID, "error", storeErr)
				}
			}
		}

		evicted++
	}

	e.log.Info("L2 eviction complete",
		"evicted", evicted,
		"freed_tokens", freedTokens,
		"elapsed", time.Since(start),
	)

	return evicted, nil
}

// evictionScore computes a score for eviction priority.
// Lower score = evict first. Combines relevance with temporal decay.
func evictionScore(mem tier.Memory, now time.Time) float64 {
	age := now.Sub(mem.LastAccessedAt).Hours()
	// Decay factor: halves every 24 hours
	decay := 1.0 / (1.0 + age/24.0)
	return mem.Relevance * decay
}

// l2TotalTokens returns the sum of token_count across L2 memories only.
func (e *Evictor) l2TotalTokens(ctx context.Context) (int64, error) {
	var total int64
	err := e.l2.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(token_count), 0) FROM memories WHERE tier = 2").Scan(&total)
	return total, err
}
