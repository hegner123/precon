package retriever

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/hegner123/precon/internal/tier"
)

// mockStore implements tier.Store for testing.
type mockStore struct {
	level      tier.Level
	results    []tier.RetrievalResult
	err        error
	lastLimit  int
	lastPrompt string
}

func (m *mockStore) Query(ctx context.Context, prompt string, limit int) ([]tier.RetrievalResult, error) {
	m.lastPrompt = prompt
	m.lastLimit = limit
	return m.results, m.err
}

func (m *mockStore) Store(ctx context.Context, mem *tier.Memory) error { return nil }
func (m *mockStore) Retrieve(ctx context.Context, id string) (*tier.Memory, error) {
	return nil, nil
}
func (m *mockStore) Delete(ctx context.Context, id string) error { return nil }
func (m *mockStore) List(ctx context.Context, conversationID string) ([]tier.Memory, error) {
	return nil, nil
}
func (m *mockStore) Level() tier.Level { return m.level }

func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRetriever_NoTiers(t *testing.T) {
	r := New(nopLogger(), 10)

	results, err := r.Retrieve(context.Background(), "hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestRetriever_SingleTier(t *testing.T) {
	store := &mockStore{
		level: tier.L2,
		results: []tier.RetrievalResult{
			{Memory: tier.Memory{ID: "a", Content: "alpha"}, Score: 0.9, SourceTier: tier.L2},
			{Memory: tier.Memory{ID: "b", Content: "beta"}, Score: 0.7, SourceTier: tier.L2},
			{Memory: tier.Memory{ID: "c", Content: "gamma"}, Score: 0.5, SourceTier: tier.L2},
		},
	}

	r := New(nopLogger(), 10, store)
	results, err := r.Retrieve(context.Background(), "test query")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, res := range results {
		if res.SourceTier != tier.L2 {
			t.Errorf("result[%d]: expected source tier L2, got %v", i, res.SourceTier)
		}
	}
}

func TestRetriever_MultipleTiers(t *testing.T) {
	l2Store := &mockStore{
		level: tier.L2,
		results: []tier.RetrievalResult{
			{Memory: tier.Memory{ID: "l2-a"}, Score: 0.9, SourceTier: tier.L2},
			{Memory: tier.Memory{ID: "l2-b"}, Score: 0.8, SourceTier: tier.L2},
		},
	}
	l4Store := &mockStore{
		level: tier.L4,
		results: []tier.RetrievalResult{
			{Memory: tier.Memory{ID: "l4-a"}, Score: 0.7, SourceTier: tier.L4},
			{Memory: tier.Memory{ID: "l4-b"}, Score: 0.6, SourceTier: tier.L4},
			{Memory: tier.Memory{ID: "l4-c"}, Score: 0.5, SourceTier: tier.L4},
		},
	}

	r := New(nopLogger(), 10, l2Store, l4Store)
	results, err := r.Retrieve(context.Background(), "multi-tier query")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// First 2 should be from L2, next 3 from L4 (order follows tier order).
	for i := range 2 {
		if results[i].SourceTier != tier.L2 {
			t.Errorf("result[%d]: expected L2, got %v", i, results[i].SourceTier)
		}
	}
	for i := 2; i < 5; i++ {
		if results[i].SourceTier != tier.L4 {
			t.Errorf("result[%d]: expected L4, got %v", i, results[i].SourceTier)
		}
	}
}

func TestRetriever_TierError_Continues(t *testing.T) {
	failingStore := &mockStore{
		level: tier.L2,
		err:   errors.New("database connection lost"),
	}
	workingStore := &mockStore{
		level: tier.L4,
		results: []tier.RetrievalResult{
			{Memory: tier.Memory{ID: "l4-ok"}, Score: 0.8, SourceTier: tier.L4},
			{Memory: tier.Memory{ID: "l4-ok2"}, Score: 0.6, SourceTier: tier.L4},
		},
	}

	r := New(nopLogger(), 10, failingStore, workingStore)
	results, err := r.Retrieve(context.Background(), "fault tolerance test")
	if err != nil {
		t.Fatalf("expected no error (fault-tolerant), got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results from working tier, got %d", len(results))
	}
	for _, res := range results {
		if res.SourceTier != tier.L4 {
			t.Errorf("expected all results from L4, got %v", res.SourceTier)
		}
	}
}

func TestRetriever_AllTiersError(t *testing.T) {
	fail1 := &mockStore{level: tier.L2, err: errors.New("fail-l2")}
	fail2 := &mockStore{level: tier.L3, err: errors.New("fail-l3")}
	fail3 := &mockStore{level: tier.L4, err: errors.New("fail-l4")}

	r := New(nopLogger(), 10, fail1, fail2, fail3)
	results, err := r.Retrieve(context.Background(), "all broken")
	if err != nil {
		t.Fatalf("expected no error (fault-tolerant), got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestRetriever_LimitPassedThrough(t *testing.T) {
	store1 := &mockStore{level: tier.L2}
	store2 := &mockStore{level: tier.L4}

	const wantLimit = 25
	r := New(nopLogger(), wantLimit, store1, store2)

	_, err := r.Retrieve(context.Background(), "limit check")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store1.lastLimit != wantLimit {
		t.Errorf("store1: expected limit %d, got %d", wantLimit, store1.lastLimit)
	}
	if store2.lastLimit != wantLimit {
		t.Errorf("store2: expected limit %d, got %d", wantLimit, store2.lastLimit)
	}
}

func TestRetriever_EmptyPrompt(t *testing.T) {
	store := &mockStore{
		level:   tier.L2,
		results: nil, // empty results for empty prompt
	}

	r := New(nopLogger(), 10, store)
	results, err := r.Retrieve(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty prompt, got %d", len(results))
	}
	if store.lastPrompt != "" {
		t.Errorf("expected empty prompt passed through, got %q", store.lastPrompt)
	}
}

func TestRetriever_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// A store that checks context and returns its error.
	ctxStore := &mockStore{
		level: tier.L2,
		err:   ctx.Err(),
	}

	r := New(nopLogger(), 10, ctxStore)
	results, err := r.Retrieve(ctx, "cancelled")
	if err != nil {
		t.Fatalf("expected no error (fault-tolerant), got %v", err)
	}
	// The cancelled store's results are skipped, so we get 0.
	if len(results) != 0 {
		t.Fatalf("expected 0 results from cancelled store, got %d", len(results))
	}
}
