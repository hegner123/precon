package storage

import (
	"testing"
	"time"

	"github.com/hegner123/precon/internal/tier"
)

func TestEvictionScore_HighRelevanceRecent(t *testing.T) {
	now := time.Now()
	mem := tier.Memory{
		Relevance:      1.0,
		LastAccessedAt: now,
	}
	score := evictionScore(mem, now)
	// Recent + high relevance = high score (don't evict)
	if score < 0.9 {
		t.Errorf("expected high score for recent high-relevance, got %f", score)
	}
}

func TestEvictionScore_LowRelevanceOld(t *testing.T) {
	now := time.Now()
	mem := tier.Memory{
		Relevance:      0.2,
		LastAccessedAt: now.Add(-72 * time.Hour), // 3 days old
	}
	score := evictionScore(mem, now)
	// Old + low relevance = low score (evict first)
	if score > 0.1 {
		t.Errorf("expected low score for old low-relevance, got %f", score)
	}
}

func TestEvictionScore_Ordering(t *testing.T) {
	now := time.Now()

	recent := tier.Memory{
		Relevance:      0.8,
		LastAccessedAt: now.Add(-1 * time.Hour),
	}
	old := tier.Memory{
		Relevance:      0.8,
		LastAccessedAt: now.Add(-48 * time.Hour),
	}
	lowRel := tier.Memory{
		Relevance:      0.1,
		LastAccessedAt: now.Add(-1 * time.Hour),
	}

	recentScore := evictionScore(recent, now)
	oldScore := evictionScore(old, now)
	lowRelScore := evictionScore(lowRel, now)

	// Recent should score higher than old (same relevance)
	if recentScore <= oldScore {
		t.Errorf("recent (%f) should score higher than old (%f)", recentScore, oldScore)
	}

	// High relevance should score higher than low relevance (same age)
	if recentScore <= lowRelScore {
		t.Errorf("high relevance (%f) should score higher than low relevance (%f)", recentScore, lowRelScore)
	}

	// Old should still beat low-relevance if relevance difference is large enough
	if oldScore <= lowRelScore {
		t.Errorf("old high-rel (%f) should score higher than recent low-rel (%f)", oldScore, lowRelScore)
	}
}
