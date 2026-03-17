package embedding

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// fakeEmbedding returns a deterministic 768-dim embedding for testing.
func fakeEmbedding(seed float32) []float32 {
	v := make([]float32, Dimensions)
	for i := range v {
		v[i] = seed + float32(i)*0.001
	}
	return v
}

func TestEmbed_Single(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", auth)
		}

		// Verify content type
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}

		// Parse request
		var req runPodRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if len(req.Input.Texts) != 1 {
			t.Fatalf("expected 1 text, got %d", len(req.Input.Texts))
		}

		if req.Input.Texts[0] != "hello world" {
			t.Errorf("expected 'hello world', got %q", req.Input.Texts[0])
		}

		// Respond
		resp := runPodResponse{
			Output: runPodOutput{
				Embeddings: [][]float32{fakeEmbedding(0.1)},
				Model:      "nomic-embed-text",
				Count:      1,
				Dimensions: Dimensions,
			},
			Status: "COMPLETED",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client pointing at test server — override the URL by using endpoint trick
	client := NewClient("test-endpoint", "test-key", testLogger())
	// Override the HTTP client's transport to redirect to our test server
	client.httpClient = server.Client()

	// We need to override the URL. Since the client builds the URL from endpointID,
	// we'll use a custom approach: replace the httpClient with one that redirects.
	client.httpClient.Transport = &rewriteTransport{
		target: server.URL,
		inner:  http.DefaultTransport,
	}

	emb, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(emb) != Dimensions {
		t.Fatalf("expected %d dimensions, got %d", Dimensions, len(emb))
	}

	// Verify first value
	if emb[0] != 0.1 {
		t.Errorf("expected emb[0]=0.1, got %f", emb[0])
	}
}

func TestEmbedBatch_MultipleBatches(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var req runPodRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		embeddings := make([][]float32, len(req.Input.Texts))
		for i := range req.Input.Texts {
			embeddings[i] = fakeEmbedding(float32(callCount) + float32(i)*0.01)
		}

		resp := runPodResponse{
			Output: runPodOutput{
				Embeddings: embeddings,
				Model:      "nomic-embed-text",
				Count:      len(embeddings),
				Dimensions: Dimensions,
			},
			Status: "COMPLETED",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-endpoint", "test-key", testLogger())
	client.httpClient.Transport = &rewriteTransport{
		target: server.URL,
		inner:  http.DefaultTransport,
	}

	// Create 3 texts — should be 1 batch
	texts := []string{"text1", "text2", "text3"}
	embeddings, err := client.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(embeddings) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(embeddings))
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

func TestEmbedBatch_Empty(t *testing.T) {
	client := NewClient("test-endpoint", "test-key", testLogger())

	embeddings, err := client.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil) failed: %v", err)
	}
	if embeddings != nil {
		t.Errorf("expected nil, got %v", embeddings)
	}
}

func TestEmbed_RetryOnFailure(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			// First request fails
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
			return
		}

		// Second request succeeds
		var req runPodRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := runPodResponse{
			Output: runPodOutput{
				Embeddings: [][]float32{fakeEmbedding(0.5)},
				Model:      "nomic-embed-text",
				Count:      1,
				Dimensions: Dimensions,
			},
			Status: "COMPLETED",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-endpoint", "test-key", testLogger())
	client.httpClient.Transport = &rewriteTransport{
		target: server.URL,
		inner:  http.DefaultTransport,
	}

	emb, err := client.Embed(context.Background(), "retry test")
	if err != nil {
		t.Fatalf("Embed failed after retry: %v", err)
	}

	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}

	if len(emb) != Dimensions {
		t.Fatalf("expected %d dimensions, got %d", Dimensions, len(emb))
	}
}

func TestEmbed_NotCompleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := runPodResponse{
			Status: "FAILED",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-endpoint", "test-key", testLogger())
	client.httpClient.Transport = &rewriteTransport{
		target: server.URL,
		inner:  http.DefaultTransport,
	}

	_, err := client.Embed(context.Background(), "fail test")
	if err == nil {
		t.Fatal("expected error for FAILED status")
	}

	if !strings.Contains(err.Error(), "FAILED") {
		t.Errorf("error should mention FAILED status, got: %s", err.Error())
	}
}

func TestEmbed_CountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 2 embeddings for 1 text
		resp := runPodResponse{
			Output: runPodOutput{
				Embeddings: [][]float32{fakeEmbedding(0.1), fakeEmbedding(0.2)},
				Model:      "nomic-embed-text",
				Count:      2,
				Dimensions: Dimensions,
			},
			Status: "COMPLETED",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-endpoint", "test-key", testLogger())
	client.httpClient.Transport = &rewriteTransport{
		target: server.URL,
		inner:  http.DefaultTransport,
	}

	_, err := client.Embed(context.Background(), "mismatch test")
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}

	if !strings.Contains(err.Error(), "2 embeddings for 1 texts") {
		t.Errorf("error should mention count mismatch, got: %s", err.Error())
	}
}

// rewriteTransport redirects all requests to a test server URL.
type rewriteTransport struct {
	target string
	inner  http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the URL with the test server, preserving the path
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.target, "http://")
	return t.inner.RoundTrip(req)
}
