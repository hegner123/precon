package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hegner123/precon/internal/tier"
)

// newTestStore creates a SQLiteStore backed by a temp DB for testing.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%s): %v", dbPath, err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// testMemory returns a populated Memory suitable for testing.
func testMemory(id, convID, content string, keywords []string) *tier.Memory {
	now := time.Now().Truncate(time.Second)
	return &tier.Memory{
		ID:             id,
		ConversationID: convID,
		TopicID:        "topic-1",
		Content:        content,
		IsSummary:      false,
		FullContentRef: "",
		TokenCount:     len(content),
		Relevance:      0.9,
		Tier:           tier.L2,
		Keywords:       keywords,
		CreatedAt:      now,
		LastAccessedAt: now,
		PromotedFrom:   tier.L1,
	}
}

func TestNewSQLiteStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestStore_StoreAndRetrieve(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := testMemory("mem-1", "conv-1", "The quick brown fox jumps over the lazy dog", nil)
	mem.IsSummary = true
	mem.FullContentRef = "ref-abc"
	mem.Relevance = 0.75

	if err := store.Store(ctx, mem); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := store.Retrieve(ctx, "mem-1")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got == nil {
		t.Fatal("Retrieve returned nil")
	}

	if got.ID != mem.ID {
		t.Errorf("ID: got %q, want %q", got.ID, mem.ID)
	}
	if got.ConversationID != mem.ConversationID {
		t.Errorf("ConversationID: got %q, want %q", got.ConversationID, mem.ConversationID)
	}
	if got.TopicID != mem.TopicID {
		t.Errorf("TopicID: got %q, want %q", got.TopicID, mem.TopicID)
	}
	if got.Content != mem.Content {
		t.Errorf("Content: got %q, want %q", got.Content, mem.Content)
	}
	if got.IsSummary != mem.IsSummary {
		t.Errorf("IsSummary: got %v, want %v", got.IsSummary, mem.IsSummary)
	}
	if got.FullContentRef != mem.FullContentRef {
		t.Errorf("FullContentRef: got %q, want %q", got.FullContentRef, mem.FullContentRef)
	}
	if got.TokenCount != mem.TokenCount {
		t.Errorf("TokenCount: got %d, want %d", got.TokenCount, mem.TokenCount)
	}
	if got.Relevance != mem.Relevance {
		t.Errorf("Relevance: got %f, want %f", got.Relevance, mem.Relevance)
	}
	if got.Tier != tier.L2 {
		t.Errorf("Tier: got %v, want %v", got.Tier, tier.L2)
	}
	if got.PromotedFrom != mem.PromotedFrom {
		t.Errorf("PromotedFrom: got %v, want %v", got.PromotedFrom, mem.PromotedFrom)
	}
	if !got.CreatedAt.Equal(mem.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, mem.CreatedAt)
	}
	if !got.LastAccessedAt.Equal(mem.LastAccessedAt) {
		t.Errorf("LastAccessedAt: got %v, want %v", got.LastAccessedAt, mem.LastAccessedAt)
	}
}

func TestStore_StoreAndRetrieve_Keywords(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	keywords := []string{"golang", "testing", "sqlite"}
	mem := testMemory("mem-kw", "conv-1", "Keywords round-trip test", keywords)

	if err := store.Store(ctx, mem); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := store.Retrieve(ctx, "mem-kw")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(got.Keywords) != len(keywords) {
		t.Fatalf("Keywords length: got %d, want %d", len(got.Keywords), len(keywords))
	}
	for i, kw := range keywords {
		if got.Keywords[i] != kw {
			t.Errorf("Keywords[%d]: got %q, want %q", i, got.Keywords[i], kw)
		}
	}
}

func TestStore_Query_FTS5(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mems := []*tier.Memory{
		testMemory("m1", "conv-1", "Photosynthesis converts sunlight into chemical energy in plants", nil),
		testMemory("m2", "conv-1", "The golang compiler produces statically linked binaries", nil),
		testMemory("m3", "conv-1", "Quantum entanglement connects particles across vast distances", nil),
	}
	for _, m := range mems {
		if err := store.Store(ctx, m); err != nil {
			t.Fatalf("Store(%s): %v", m.ID, err)
		}
	}

	results, err := store.Query(ctx, "photosynthesis", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Query results: got %d, want 1", len(results))
	}
	if results[0].Memory.ID != "m1" {
		t.Errorf("Query result ID: got %q, want %q", results[0].Memory.ID, "m1")
	}
	if results[0].Score <= 0 {
		t.Errorf("Query result score: got %f, want > 0", results[0].Score)
	}
	if results[0].SourceTier != tier.L2 {
		t.Errorf("Query result SourceTier: got %v, want %v", results[0].SourceTier, tier.L2)
	}
}

func TestStore_Query_NoResults(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := testMemory("m1", "conv-1", "The golang compiler produces statically linked binaries", nil)
	if err := store.Store(ctx, mem); err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := store.Query(ctx, "xylophone", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query results: got %d, want 0", len(results))
	}
}

func TestStore_Query_MultipleMatches(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mems := []*tier.Memory{
		testMemory("m1", "conv-1", "golang testing patterns and best practices", nil),
		testMemory("m2", "conv-1", "golang benchmarks measure performance overhead", nil),
		testMemory("m3", "conv-1", "rust ownership model prevents data races", nil),
	}
	for _, m := range mems {
		if err := store.Store(ctx, m); err != nil {
			t.Fatalf("Store(%s): %v", m.ID, err)
		}
	}

	results, err := store.Query(ctx, "golang", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Query results: got %d, want 2", len(results))
	}

	ids := map[string]bool{}
	for _, r := range results {
		ids[r.Memory.ID] = true
	}
	if !ids["m1"] {
		t.Error("expected m1 in results")
	}
	if !ids["m2"] {
		t.Error("expected m2 in results")
	}
}

func TestStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := testMemory("mem-del", "conv-1", "This memory will be deleted shortly", nil)
	if err := store.Store(ctx, mem); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify it exists before deletion.
	got, err := store.Retrieve(ctx, "mem-del")
	if err != nil {
		t.Fatalf("Retrieve before delete: %v", err)
	}
	if got == nil {
		t.Fatal("memory should exist before delete")
	}

	if err := store.Delete(ctx, "mem-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Retrieve should fail (sql.ErrNoRows wrapped).
	got, err = store.Retrieve(ctx, "mem-del")
	if err == nil {
		t.Fatal("Retrieve after delete: expected error, got nil")
	}

	// FTS5 query should also return nothing.
	results, err := store.Query(ctx, "deleted shortly", 10)
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query after delete: got %d results, want 0", len(results))
	}
}

func TestStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mems := []*tier.Memory{
		testMemory("m1", "abc", "First memory for conversation abc", nil),
		testMemory("m2", "abc", "Second memory for conversation abc", nil),
		testMemory("m3", "xyz", "First memory for conversation xyz", nil),
	}
	for _, m := range mems {
		if err := store.Store(ctx, m); err != nil {
			t.Fatalf("Store(%s): %v", m.ID, err)
		}
	}

	t.Run("filter by conversation", func(t *testing.T) {
		list, err := store.List(ctx, "abc")
		if err != nil {
			t.Fatalf("List(abc): %v", err)
		}
		if len(list) != 2 {
			t.Errorf("List(abc): got %d, want 2", len(list))
		}
	})

	t.Run("all conversations", func(t *testing.T) {
		list, err := store.List(ctx, "")
		if err != nil {
			t.Fatalf("List(''): %v", err)
		}
		if len(list) != 3 {
			t.Errorf("List(''): got %d, want 3", len(list))
		}
	})
}

func TestStore_Level(t *testing.T) {
	store := newTestStore(t)
	if got := store.Level(); got != tier.L2 {
		t.Errorf("Level: got %v, want %v", got, tier.L2)
	}
}

func TestStore_CreateAndGetTopic(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	topic := &Topic{
		ID:             "topic-1",
		ConversationID: "conv-1",
		Name:           "Memory Architecture",
		Keywords:       []string{"tiered", "cache", "sqlite"},
		Relevance:      0.85,
		IsCurrent:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := store.CreateTopic(ctx, topic); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	got, err := store.GetTopic(ctx, "topic-1")
	if err != nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if got == nil {
		t.Fatal("GetTopic returned nil")
	}

	if got.ID != topic.ID {
		t.Errorf("ID: got %q, want %q", got.ID, topic.ID)
	}
	if got.ConversationID != topic.ConversationID {
		t.Errorf("ConversationID: got %q, want %q", got.ConversationID, topic.ConversationID)
	}
	if got.Name != topic.Name {
		t.Errorf("Name: got %q, want %q", got.Name, topic.Name)
	}
	if got.Relevance != topic.Relevance {
		t.Errorf("Relevance: got %f, want %f", got.Relevance, topic.Relevance)
	}
	if got.IsCurrent != topic.IsCurrent {
		t.Errorf("IsCurrent: got %v, want %v", got.IsCurrent, topic.IsCurrent)
	}
	if !got.CreatedAt.Equal(topic.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, topic.CreatedAt)
	}
	if !got.UpdatedAt.Equal(topic.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, topic.UpdatedAt)
	}

	if len(got.Keywords) != len(topic.Keywords) {
		t.Fatalf("Keywords length: got %d, want %d", len(got.Keywords), len(topic.Keywords))
	}
	for i, kw := range topic.Keywords {
		if got.Keywords[i] != kw {
			t.Errorf("Keywords[%d]: got %q, want %q", i, got.Keywords[i], kw)
		}
	}
}

func TestStore_ListTopics(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	topics := []*Topic{
		{
			ID:             "t1",
			ConversationID: "conv-A",
			Name:           "Topic One",
			Keywords:       []string{"one"},
			Relevance:      0.9,
			IsCurrent:      true,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:             "t2",
			ConversationID: "conv-A",
			Name:           "Topic Two",
			Keywords:       []string{"two"},
			Relevance:      0.8,
			IsCurrent:      false,
			CreatedAt:      now,
			UpdatedAt:      now.Add(-time.Minute), // older update
		},
		{
			ID:             "t3",
			ConversationID: "conv-B",
			Name:           "Topic Three",
			Keywords:       []string{"three"},
			Relevance:      0.7,
			IsCurrent:      true,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	for _, tp := range topics {
		if err := store.CreateTopic(ctx, tp); err != nil {
			t.Fatalf("CreateTopic(%s): %v", tp.ID, err)
		}
	}

	t.Run("conv-A has 2 topics", func(t *testing.T) {
		list, err := store.ListTopics(ctx, "conv-A")
		if err != nil {
			t.Fatalf("ListTopics(conv-A): %v", err)
		}
		if len(list) != 2 {
			t.Errorf("ListTopics(conv-A): got %d, want 2", len(list))
		}
	})

	t.Run("conv-B has 1 topic", func(t *testing.T) {
		list, err := store.ListTopics(ctx, "conv-B")
		if err != nil {
			t.Fatalf("ListTopics(conv-B): %v", err)
		}
		if len(list) != 1 {
			t.Errorf("ListTopics(conv-B): got %d, want 1", len(list))
		}
	})

	t.Run("nonexistent conversation", func(t *testing.T) {
		list, err := store.ListTopics(ctx, "conv-Z")
		if err != nil {
			t.Fatalf("ListTopics(conv-Z): %v", err)
		}
		if len(list) != 0 {
			t.Errorf("ListTopics(conv-Z): got %d, want 0", len(list))
		}
	})
}

func TestStore_UpdateRelevance(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := testMemory("mem-rel", "conv-1", "Memory for relevance update test", nil)
	mem.Relevance = 0.5
	// Set LastAccessedAt to a known past time so the update is detectable
	mem.LastAccessedAt = time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := store.Store(ctx, mem); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Update relevance
	if err := store.UpdateRelevance(ctx, "mem-rel", 0.9); err != nil {
		t.Fatalf("UpdateRelevance: %v", err)
	}

	// Verify updated
	got, err := store.Retrieve(ctx, "mem-rel")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Relevance != 0.9 {
		t.Errorf("Relevance: got %f, want 0.9", got.Relevance)
	}

	// LastAccessedAt should have been updated too
	if got.LastAccessedAt.Before(mem.LastAccessedAt) || got.LastAccessedAt.Equal(mem.LastAccessedAt) {
		t.Errorf("LastAccessedAt should be updated after UpdateRelevance")
	}
}

func TestStore_UpdateRelevance_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.UpdateRelevance(ctx, "nonexistent", 0.5)
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}
