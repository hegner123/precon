// Package embedding provides a RunPod serverless embedding client for
// nomic-embed-text (768 dimensions). Adapted from ingest/internal/embed/runpod.go.
//
// The client calls the RunPod runsync API with batch support (up to 500 texts
// per request) and a single retry with exponential backoff on failure.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Dimensions is the embedding vector size for nomic-embed-text.
const Dimensions = 768

// maxBatchSize is the maximum number of texts per RunPod request.
const maxBatchSize = 500

// Embedding is a float32 vector from the embedding model.
type Embedding []float32

// Client calls the RunPod serverless embedding API.
type Client struct {
	endpointID string
	apiKey     string
	httpClient *http.Client
	log        *slog.Logger
}

// NewClient creates an embedding client for the given RunPod endpoint.
func NewClient(endpointID, apiKey string, log *slog.Logger) *Client {
	return &Client{
		endpointID: endpointID,
		apiKey:     apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		log: log,
	}
}

type runPodRequest struct {
	Input runPodInput `json:"input"`
}

type runPodInput struct {
	Texts []string `json:"texts"`
}

type runPodResponse struct {
	Output runPodOutput `json:"output"`
	Status string       `json:"status"`
}

type runPodOutput struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model"`
	Count      int         `json:"count"`
	Dimensions int         `json:"dimensions"`
}

// Embed generates an embedding vector for the given text.
func (c *Client) Embed(ctx context.Context, text string) (Embedding, error) {
	embeddings, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts, splitting into sub-requests
// if the batch exceeds maxBatchSize.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([]Embedding, 0, len(texts))

	for start := 0; start < len(texts); start += maxBatchSize {
		end := min(start+maxBatchSize, len(texts))
		batch := texts[start:end]

		c.log.Debug("sending RunPod batch",
			"batch_start", start,
			"batch_size", len(batch),
			"total", len(texts),
		)

		embeddings, err := c.sendBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embed batch starting at %d: %w", start, err)
		}
		result = append(result, embeddings...)
	}

	return result, nil
}

func (c *Client) sendBatch(ctx context.Context, texts []string) ([]Embedding, error) {
	body, err := json.Marshal(runPodRequest{
		Input: runPodInput{Texts: texts},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal RunPod request: %w", err)
	}

	url := fmt.Sprintf("https://api.runpod.ai/v2/%s/runsync", c.endpointID)

	embeddings, err := c.doRequest(ctx, url, body)
	if err != nil {
		// Single retry on failure with backoff.
		c.log.Warn("RunPod request failed, retrying", "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		embeddings, err = c.doRequest(ctx, url, body)
		if err != nil {
			return nil, err
		}
	}

	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("RunPod returned %d embeddings for %d texts", len(embeddings), len(texts))
	}

	return embeddings, nil
}

func (c *Client) doRequest(ctx context.Context, url string, body []byte) ([]Embedding, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create RunPod request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		c.log.Error("RunPod request failed", "elapsed", elapsed, "error", err)
		return nil, fmt.Errorf("RunPod request failed: %w", err)
	}
	defer resp.Body.Close()

	c.log.Debug("RunPod response received",
		"status", resp.StatusCode,
		"elapsed", elapsed,
	)

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			respBody = nil
		}
		c.log.Error("RunPod request returned error",
			"status", resp.StatusCode,
			"body", string(respBody),
			"elapsed", elapsed,
		)
		return nil, fmt.Errorf("RunPod request returned %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read RunPod response body: %w", err)
	}

	var result runPodResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode RunPod response: %w", err)
	}

	if result.Status != "COMPLETED" {
		c.log.Error("RunPod job not completed",
			"status", result.Status,
			"body", string(respBody),
			"elapsed", elapsed,
		)
		return nil, fmt.Errorf("RunPod job status: %s (expected COMPLETED)", result.Status)
	}

	if len(result.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("RunPod response contained no embeddings")
	}

	c.log.Debug("RunPod batch complete",
		"count", result.Output.Count,
		"dimensions", result.Output.Dimensions,
		"model", result.Output.Model,
		"elapsed", elapsed,
	)

	embeddings := make([]Embedding, len(result.Output.Embeddings))
	for i, e := range result.Output.Embeddings {
		embeddings[i] = Embedding(e)
	}

	return embeddings, nil
}
