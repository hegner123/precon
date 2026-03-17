package persister

import (
	"context"
	"testing"

	"github.com/hegner123/precon/internal/api"
)

func TestClientAdapter_ImplementsLLM(t *testing.T) {
	// Compile-time check that ClientAdapter implements LLM
	var _ LLM = (*ClientAdapter)(nil)
}

func TestNewClientAdapter(t *testing.T) {
	// Just verifies construction — no real API calls
	adapter := NewClientAdapter(nil, "test-model")
	if adapter == nil {
		t.Fatal("NewClientAdapter returned nil")
	}
	if adapter.model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", adapter.model)
	}
}

func TestClientAdapter_Complete_NilClient(t *testing.T) {
	// This tests that the adapter propagates errors from a nil client
	adapter := NewClientAdapter(nil, "test-model")

	// Should panic or error due to nil client — we just verify it doesn't silently succeed
	defer func() {
		if r := recover(); r == nil {
			t.Log("Complete with nil client did not panic (might return error instead)")
		}
	}()

	_, err := adapter.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Error("expected error from nil client")
	}
}

func TestClientAdapter_CompleteWithTools_NilClient(t *testing.T) {
	adapter := NewClientAdapter(nil, "test-model")

	defer func() {
		if r := recover(); r == nil {
			t.Log("CompleteWithTools with nil client did not panic")
		}
	}()

	_, err := adapter.CompleteWithTools(context.Background(), &api.Request{})
	if err == nil {
		t.Error("expected error from nil client")
	}
}
