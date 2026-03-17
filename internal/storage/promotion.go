package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hegner123/precon/internal/tier"
)

// Promoter manages the L4→L2 promotion and L2→L4 demotion lifecycle.
// Promotion copies content from L4 to L2 for fast access; demotion removes
// the promoted L2 copy while the L4 original remains intact.
type Promoter struct {
	l2  *SQLiteStore
	l4  *PgvectorStore
	log *slog.Logger
}

// NewPromoter creates a Promoter for managing content lifecycle across L2 and L4.
func NewPromoter(l2 *SQLiteStore, l4 *PgvectorStore, log *slog.Logger) *Promoter {
	return &Promoter{
		l2:  l2,
		l4:  l4,
		log: log,
	}
}

// Promote copies a memory from L4 to L2, marking it as promoted.
// The new L2 memory has PromotedFrom=L4 and FullContentRef set to the L4 ID
// so it can be traced back to its origin.
func (p *Promoter) Promote(ctx context.Context, l4ID string) (*tier.Memory, error) {
	l4Mem, err := p.l4.Retrieve(ctx, l4ID)
	if err != nil {
		return nil, fmt.Errorf("retrieve L4 memory for promotion: %w", err)
	}

	now := time.Now()
	promoted := &tier.Memory{
		ID:             fmt.Sprintf("promo-%d", now.UnixNano()),
		ConversationID: l4Mem.ConversationID,
		TopicID:        l4Mem.TopicID,
		Content:        l4Mem.Content,
		IsSummary:      l4Mem.IsSummary,
		FullContentRef: l4ID,
		TokenCount:     l4Mem.TokenCount,
		Relevance:      l4Mem.Relevance,
		Tier:           tier.L2,
		Keywords:       l4Mem.Keywords,
		CreatedAt:      now,
		LastAccessedAt: now,
		PromotedFrom:   tier.L4,
	}

	if err := p.l2.Store(ctx, promoted); err != nil {
		return nil, fmt.Errorf("store promoted memory in L2: %w", err)
	}

	p.log.Info("promoted L4→L2",
		"l4_id", l4ID,
		"l2_id", promoted.ID,
		"tokens", promoted.TokenCount,
	)

	return promoted, nil
}

// Demote removes a promoted L2 memory. The L4 original remains intact.
// Returns an error if the memory was not promoted (PromotedFrom == 0).
func (p *Promoter) Demote(ctx context.Context, l2ID string) error {
	mem, err := p.l2.Retrieve(ctx, l2ID)
	if err != nil {
		return fmt.Errorf("retrieve L2 memory for demotion: %w", err)
	}

	if !IsPromoted(*mem) {
		return fmt.Errorf("memory %s is not promoted (PromotedFrom=%s)", l2ID, mem.PromotedFrom)
	}

	if err := p.l2.Delete(ctx, l2ID); err != nil {
		return fmt.Errorf("delete promoted L2 memory: %w", err)
	}

	p.log.Info("demoted L2 (L4 original retained)",
		"l2_id", l2ID,
		"original_tier", mem.PromotedFrom,
		"full_content_ref", mem.FullContentRef,
	)

	return nil
}

// IsPromoted returns true if the memory was promoted from another tier.
func IsPromoted(mem tier.Memory) bool {
	return mem.PromotedFrom != 0
}
