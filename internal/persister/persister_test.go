package persister

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/tier"
)

// mockLLM is a test double for the LLM interface.
type mockLLM struct {
	completeResponse string
	completeErr      error
	toolResponse     *api.Response
	toolErr          error
	callCount        int
}

func (m *mockLLM) Complete(_ context.Context, _, _ string) (string, error) {
	m.callCount++
	return m.completeResponse, m.completeErr
}

func (m *mockLLM) CompleteWithTools(_ context.Context, _ *api.Request) (*api.Response, error) {
	m.callCount++
	return m.toolResponse, m.toolErr
}

// mockStore is a test double for L2Writer.
type mockStore struct {
	stored       []*tier.Memory
	deleted      []string
	relevanceMap map[string]float64
	listed       []tier.Memory
	storeErr     error
	updateErr    error
}

func newMockStore() *mockStore {
	return &mockStore{
		relevanceMap: make(map[string]float64),
	}
}

func (m *mockStore) Store(_ context.Context, mem *tier.Memory) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = append(m.stored, mem)
	return nil
}

func (m *mockStore) Retrieve(_ context.Context, id string) (*tier.Memory, error) {
	for _, mem := range m.stored {
		if mem.ID == id {
			return mem, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", id)
}

func (m *mockStore) Query(_ context.Context, _ string, _ int) ([]tier.RetrievalResult, error) {
	return nil, nil
}

func (m *mockStore) Delete(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}

func (m *mockStore) List(_ context.Context, _ string) ([]tier.Memory, error) {
	return m.listed, nil
}

func (m *mockStore) Level() tier.Level {
	return tier.L2
}

func (m *mockStore) UpdateRelevance(_ context.Context, id string, score float64) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.relevanceMap[id] = score
	return nil
}

// makeToolResponse builds a mock api.Response with a persist_decision tool call.
func makeToolResponse(decision Decision) *api.Response {
	input, _ := json.Marshal(decision)
	return &api.Response{
		StopReason: api.StopReasonToolUse,
		Content: []api.ContentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_test",
				Name:  persistToolName,
				Input: json.RawMessage(input),
			},
		},
	}
}

func TestReview_SuccessfulToolUse(t *testing.T) {
	decision := Decision{
		NewTopics: []TopicEntry{
			{Name: "test-topic", Keywords: []string{"go", "testing"}, Content: "Writing tests for persister"},
		},
		UpdatedScores: []ScoreUpdate{
			{TopicID: "existing-1", NewScore: 0.8, Reason: "still relevant"},
		},
		ShouldEvict: false,
	}

	llm := &mockLLM{
		toolResponse: makeToolResponse(decision),
	}

	store := newMockStore()
	p := New(testLogger(), llm, store, "test-model")

	got, err := p.Review(context.Background(), "write tests", "I'll write the tests now", nil)
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}

	if len(got.NewTopics) != 1 {
		t.Errorf("expected 1 new topic, got %d", len(got.NewTopics))
	}
	if got.NewTopics[0].Name != "test-topic" {
		t.Errorf("expected topic name 'test-topic', got %q", got.NewTopics[0].Name)
	}
	if len(got.UpdatedScores) != 1 {
		t.Errorf("expected 1 score update, got %d", len(got.UpdatedScores))
	}
	if got.ShouldEvict {
		t.Error("expected ShouldEvict=false")
	}
	if llm.callCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.callCount)
	}
}

func TestReview_EmptyDecision(t *testing.T) {
	decision := Decision{
		NewTopics:     []TopicEntry{},
		UpdatedScores: []ScoreUpdate{},
		ShouldEvict:   false,
	}

	llm := &mockLLM{
		toolResponse: makeToolResponse(decision),
	}

	store := newMockStore()
	p := New(testLogger(), llm, store, "test-model")

	got, err := p.Review(context.Background(), "hello", "hi there", nil)
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}

	if len(got.NewTopics) != 0 {
		t.Errorf("expected 0 new topics, got %d", len(got.NewTopics))
	}
	if len(got.UpdatedScores) != 0 {
		t.Errorf("expected 0 score updates, got %d", len(got.UpdatedScores))
	}
}

func TestReview_RetryOnLLMError(t *testing.T) {
	decision := Decision{
		NewTopics: []TopicEntry{
			{Name: "retry-topic", Keywords: []string{"retry"}, Content: "Succeeded on retry"},
		},
		ShouldEvict: false,
	}

	callNum := 0
	llm := &mockLLM{}
	// Override CompleteWithTools to fail on first call, succeed on second
	origResponse := makeToolResponse(decision)

	type retryLLM struct {
		calls    int
		response *api.Response
	}
	rllm := &retryLLM{response: origResponse}

	// Use a wrapper that fails first then succeeds
	p := New(testLogger(), &retryableMock{
		failCount: 1,
		response:  origResponse,
	}, newMockStore(), "test-model")

	got, err := p.Review(context.Background(), "test", "response", nil)
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}

	if len(got.NewTopics) != 1 {
		t.Errorf("expected 1 new topic after retry, got %d", len(got.NewTopics))
	}

	// Verify unused vars don't cause issues
	_ = llm
	_ = callNum
	_ = rllm
}

// retryableMock fails failCount times then succeeds.
type retryableMock struct {
	failCount int
	calls     int
	response  *api.Response
}

func (m *retryableMock) Complete(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *retryableMock) CompleteWithTools(_ context.Context, _ *api.Request) (*api.Response, error) {
	m.calls++
	if m.calls <= m.failCount {
		return nil, fmt.Errorf("transient error %d", m.calls)
	}
	return m.response, nil
}

func TestReview_AllRetriesFail(t *testing.T) {
	llm := &mockLLM{
		toolErr: fmt.Errorf("persistent failure"),
	}

	p := New(testLogger(), llm, newMockStore(), "test-model")

	got, err := p.Review(context.Background(), "test", "response", nil)
	if err != nil {
		t.Fatalf("Review() should not return error (falls back to empty decision), got: %v", err)
	}

	// Should return empty decision after exhausting retries
	if len(got.NewTopics) != 0 || len(got.UpdatedScores) != 0 || got.ShouldEvict {
		t.Errorf("expected empty decision on total failure, got %+v", got)
	}

	// Should have tried maxRetries+1 times
	if llm.callCount != maxRetries+1 {
		t.Errorf("expected %d LLM calls, got %d", maxRetries+1, llm.callCount)
	}
}

func TestReview_NoToolUseInResponse(t *testing.T) {
	// Response has text but no tool_use — should retry
	textResp := &api.Response{
		StopReason: api.StopReasonEndTurn,
		Content: []api.ContentBlock{
			{Type: "text", Text: "I should save this..."},
		},
	}

	successDecision := Decision{
		NewTopics: []TopicEntry{
			{Name: "recovered", Keywords: []string{"test"}, Content: "Recovered after bad response"},
		},
	}

	llm := &retryableMockWithResponses{
		responses: []*api.Response{
			textResp,                          // first: no tool_use
			makeToolResponse(successDecision), // second: proper tool_use
		},
	}

	p := New(testLogger(), llm, newMockStore(), "test-model")

	got, err := p.Review(context.Background(), "test", "response", nil)
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}

	if len(got.NewTopics) != 1 || got.NewTopics[0].Name != "recovered" {
		t.Errorf("expected recovered topic, got %+v", got)
	}
}

type retryableMockWithResponses struct {
	responses []*api.Response
	idx       int
}

func (m *retryableMockWithResponses) Complete(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *retryableMockWithResponses) CompleteWithTools(_ context.Context, _ *api.Request) (*api.Response, error) {
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func TestApply_StoresNewTopics(t *testing.T) {
	store := newMockStore()
	p := New(testLogger(), &mockLLM{}, store, "test-model")

	decision := &Decision{
		NewTopics: []TopicEntry{
			{Name: "topic-a", Keywords: []string{"go", "testing"}, Content: "Content A"},
			{Name: "topic-b", Keywords: []string{"design"}, Content: "Content B"},
		},
	}

	err := p.Apply(context.Background(), decision, "conv-123")
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	if len(store.stored) != 2 {
		t.Fatalf("expected 2 stored memories, got %d", len(store.stored))
	}

	// Check first stored memory
	mem := store.stored[0]
	if mem.ConversationID != "conv-123" {
		t.Errorf("expected conversation ID 'conv-123', got %q", mem.ConversationID)
	}
	if mem.Tier != tier.L2 {
		t.Errorf("expected tier L2, got %v", mem.Tier)
	}
	if mem.Relevance != 1.0 {
		t.Errorf("expected relevance 1.0, got %f", mem.Relevance)
	}
}

func TestApply_UpdatesRelevanceScores(t *testing.T) {
	store := newMockStore()
	p := New(testLogger(), &mockLLM{}, store, "test-model")

	decision := &Decision{
		UpdatedScores: []ScoreUpdate{
			{TopicID: "mem-1", NewScore: 0.5, Reason: "less relevant now"},
			{TopicID: "mem-2", NewScore: 0.9, Reason: "still important"},
		},
	}

	err := p.Apply(context.Background(), decision, "conv-123")
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	if score, ok := store.relevanceMap["mem-1"]; !ok || score != 0.5 {
		t.Errorf("expected mem-1 score 0.5, got %v (found=%v)", score, ok)
	}
	if score, ok := store.relevanceMap["mem-2"]; !ok || score != 0.9 {
		t.Errorf("expected mem-2 score 0.9, got %v (found=%v)", score, ok)
	}
}

func TestApply_HandlesStoreErrors(t *testing.T) {
	store := newMockStore()
	store.storeErr = fmt.Errorf("disk full")
	p := New(testLogger(), &mockLLM{}, store, "test-model")

	decision := &Decision{
		NewTopics: []TopicEntry{
			{Name: "will-fail", Keywords: []string{"fail"}, Content: "This will fail"},
		},
	}

	// Should not return error — just logs warnings
	err := p.Apply(context.Background(), decision, "conv-123")
	if err != nil {
		t.Fatalf("Apply() should not return error on store failure, got: %v", err)
	}

	if len(store.stored) != 0 {
		t.Errorf("expected 0 stored (all failed), got %d", len(store.stored))
	}
}

func TestApply_HandlesUpdateErrors(t *testing.T) {
	store := newMockStore()
	store.updateErr = fmt.Errorf("not found")
	p := New(testLogger(), &mockLLM{}, store, "test-model")

	decision := &Decision{
		UpdatedScores: []ScoreUpdate{
			{TopicID: "nonexistent", NewScore: 0.5, Reason: "test"},
		},
	}

	err := p.Apply(context.Background(), decision, "conv-123")
	if err != nil {
		t.Fatalf("Apply() should not return error on update failure, got: %v", err)
	}
}

func TestPersistTool_Schema(t *testing.T) {
	tool := persistTool()

	if tool.Name != "persist_decision" {
		t.Errorf("expected tool name 'persist_decision', got %q", tool.Name)
	}

	schema, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	// Check required fields exist
	for _, field := range []string{"new_topics", "updated_scores", "should_evict"} {
		if _, ok := schema[field]; !ok {
			t.Errorf("expected field %q in schema", field)
		}
	}

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatal("expected required array in schema")
	}
	if len(required) != 3 {
		t.Errorf("expected 3 required fields, got %d", len(required))
	}
}

func TestParseToolResponse_ValidInput(t *testing.T) {
	decision := Decision{
		NewTopics: []TopicEntry{
			{Name: "test", Keywords: []string{"a"}, Content: "b"},
		},
		UpdatedScores: []ScoreUpdate{},
		ShouldEvict:   true,
	}

	resp := makeToolResponse(decision)
	got, err := parseToolResponse(resp)
	if err != nil {
		t.Fatalf("parseToolResponse() error: %v", err)
	}

	if len(got.NewTopics) != 1 {
		t.Errorf("expected 1 topic, got %d", len(got.NewTopics))
	}
	if !got.ShouldEvict {
		t.Error("expected ShouldEvict=true")
	}
}

func TestParseToolResponse_NoToolUse(t *testing.T) {
	resp := &api.Response{
		StopReason: api.StopReasonEndTurn,
		Content: []api.ContentBlock{
			{Type: "text", Text: "just text"},
		},
	}

	_, err := parseToolResponse(resp)
	if err == nil {
		t.Fatal("expected error for response without tool_use")
	}
}

func TestParseToolResponse_WrongToolName(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"key": "value"})
	resp := &api.Response{
		StopReason: api.StopReasonToolUse,
		Content: []api.ContentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_wrong",
				Name:  "wrong_tool",
				Input: json.RawMessage(input),
			},
		},
	}

	_, err := parseToolResponse(resp)
	if err == nil {
		t.Fatal("expected error for wrong tool name")
	}
}

func TestReview_WithToolResults(t *testing.T) {
	decision := Decision{
		NewTopics: []TopicEntry{
			{Name: "tool-analysis", Keywords: []string{"tools"}, Content: "Analyzed tool usage"},
		},
	}

	llm := &mockLLM{
		toolResponse: makeToolResponse(decision),
	}

	p := New(testLogger(), llm, newMockStore(), "test-model")

	toolResults := []string{
		"Tool: checkfor → 5 matches",
		"Tool: repfor → 3 replacements in 2 files",
	}

	got, err := p.Review(context.Background(), "find and replace", "Done", toolResults)
	if err != nil {
		t.Fatalf("Review() error: %v", err)
	}

	if len(got.NewTopics) != 1 {
		t.Errorf("expected 1 topic, got %d", len(got.NewTopics))
	}
}

func TestReview_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	llm := &mockLLM{
		toolErr: ctx.Err(),
	}

	p := New(testLogger(), llm, newMockStore(), "test-model")

	got, err := p.Review(ctx, "test", "response", nil)
	if err != nil {
		t.Fatalf("Review() should return empty decision on context cancellation, got error: %v", err)
	}

	// Should still return empty decision
	if len(got.NewTopics) != 0 {
		t.Errorf("expected empty decision, got %+v", got)
	}
}
