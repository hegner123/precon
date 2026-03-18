// Package persister implements the background persistence agent.
//
// The persister is an unconscious (autonomic) agent that runs after each working
// agent turn. It reviews what the working agent produced and decides:
// - What new knowledge to save to L2 (hot storage)
// - What existing topics to update with new relevance scores
// - When to trigger tier eviction (L2 overflow → lower tiers)
//
// Uses Haiku for speed and cost efficiency via tool_use with a JSON schema
// for structured output. The working agent never knows this is happening —
// it just gets a well-curated context window next time.
package persister

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/tier"
)

// LLM abstracts the language model used for persistence decisions.
// Phase 5 expanded: supports both simple completion and tool_use for structured output.
type LLM interface {
	// Complete sends a prompt and returns the response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// CompleteWithTools sends a prompt with tool definitions and returns the full response.
	// The persister uses this to get structured JSON output via tool_use.
	CompleteWithTools(ctx context.Context, req *api.Request) (*api.Response, error)
}

// Decision represents what the persister decided about a turn.
type Decision struct {
	// NewTopics are topics to create in L2 from this turn's content.
	NewTopics []TopicEntry `json:"new_topics"`

	// UpdatedScores are relevance score updates for existing topics.
	UpdatedScores []ScoreUpdate `json:"updated_scores"`

	// ShouldEvict is true if L2 has grown too large and needs eviction.
	ShouldEvict bool `json:"should_evict"`
}

// TopicEntry is a new topic to persist.
type TopicEntry struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
	Content  string   `json:"content"`
}

// ScoreUpdate is a relevance score change for an existing topic.
type ScoreUpdate struct {
	TopicID  string  `json:"topic_id"`
	NewScore float64 `json:"new_score"`
	Reason   string  `json:"reason"`
}

// L2Writer is the subset of L2 storage the persister needs to write.
// Narrower than tier.Store — only write operations, not reads.
type L2Writer interface {
	tier.Store
	// UpdateRelevance updates the relevance score for a memory by ID.
	UpdateRelevance(ctx context.Context, id string, score float64) error
}

// maxRetries is the number of times to retry on tool_use parse failure.
const maxRetries = 2

// persistToolName is the name of the tool the LLM must call.
const persistToolName = "persist_decision"

// Persister reviews working agent output and manages persistence.
type Persister struct {
	log          *slog.Logger
	llm          LLM
	l2           L2Writer
	model        string
	systemPrompt string
}

// New creates a Persister with the given LLM and L2 store.
func New(log *slog.Logger, llm LLM, l2 L2Writer, model string) *Persister {
	return &Persister{
		log:   log,
		llm:   llm,
		l2:    l2,
		model: model,
		systemPrompt: `You are a persistence agent. After each working agent turn, you review the conversation and decide what knowledge to save.

You MUST call the persist_decision tool with your analysis. Do not respond with text.

Rules:
- Identify new topics or knowledge worth persisting.
- Score existing topics for continued relevance (0.0 = irrelevant, 1.0 = critical).
- Be conservative: not every turn produces new knowledge.
- Focus on: decisions made, problems solved, patterns identified, code changes.
- Ignore: greetings, acknowledgments, status updates, repetitive tool output.
- If nothing worth persisting, call the tool with empty arrays and should_evict=false.`,
	}
}

// persistTool returns the API tool definition for structured persistence output.
func persistTool() api.Tool {
	return api.Tool{
		Name:        persistToolName,
		Description: "Record persistence decisions about the current turn. Must be called exactly once.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"new_topics": map[string]any{
					"type":        "array",
					"description": "New topics to persist from this turn. Empty array if nothing new.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{
								"type":        "string",
								"description": "Short descriptive topic name",
							},
							"keywords": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Keywords for FTS5 search",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "The knowledge to persist — concise summary of what was learned/decided",
							},
						},
						"required": []string{"name", "keywords", "content"},
					},
				},
				"updated_scores": map[string]any{
					"type":        "array",
					"description": "Relevance score updates for existing topics. Empty array if no changes.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"topic_id": map[string]any{
								"type":        "string",
								"description": "ID of the existing topic to update",
							},
							"new_score": map[string]any{
								"type":        "number",
								"description": "New relevance score from 0.0 (irrelevant) to 1.0 (critical)",
							},
							"reason": map[string]any{
								"type":        "string",
								"description": "Brief reason for the score change",
							},
						},
						"required": []string{"topic_id", "new_score", "reason"},
					},
				},
				"should_evict": map[string]any{
					"type":        "boolean",
					"description": "True if L2 storage seems overloaded and old content should be evicted",
				},
			},
			"required": []string{"new_topics", "updated_scores", "should_evict"},
		},
	}
}

// Review analyzes a working agent turn and returns persistence decisions.
// Uses tool_use for structured output with retry on parse failure.
// Falls back to empty decision after maxRetries failures.
func (p *Persister) Review(ctx context.Context, userPrompt, agentResponse string, toolResults []string) (*Decision, error) {
	start := time.Now()

	input := fmt.Sprintf("USER PROMPT:\n%s\n\nAGENT RESPONSE:\n%s", userPrompt, agentResponse)
	if len(toolResults) > 0 {
		input += "\n\nTOOL RESULTS:\n"
		for i, r := range toolResults {
			// Truncate individual tool results to keep persister input manageable
			result := r
			if len(result) > 2000 {
				result = result[:2000] + "\n... (truncated)"
			}
			input += fmt.Sprintf("--- Tool %d ---\n%s\n", i+1, result)
		}
	}

	// Build request with tool_use
	sys := api.NewSystemString(p.systemPrompt)
	req := &api.Request{
		Model:    p.model,
		Messages: []api.MessageParam{api.NewUserMessage(input)},
		System:   &sys,
		Tools:    []api.Tool{persistTool()},
		// Force the model to call our specific tool
		ToolChoice: api.ToolChoiceTool(persistToolName),
		MaxTokens:  2048,
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.log.Warn("retrying persistence review",
				"attempt", attempt,
				"error", lastErr,
			)
		}

		resp, err := p.llm.CompleteWithTools(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("persistence LLM call failed: %w", err)
			continue
		}

		decision, err := parseToolResponse(resp)
		if err != nil {
			lastErr = fmt.Errorf("parse tool response: %w", err)
			continue
		}

		p.log.Info("persistence review complete",
			"duration", time.Since(start),
			"new_topics", len(decision.NewTopics),
			"score_updates", len(decision.UpdatedScores),
			"should_evict", decision.ShouldEvict,
		)
		return decision, nil
	}

	// All retries exhausted — skip persistence for this turn
	p.log.Error("persistence review failed after retries, skipping",
		"attempts", maxRetries+1,
		"duration", time.Since(start),
		"error", lastErr,
	)
	return &Decision{}, nil
}

// parseToolResponse extracts the Decision from a tool_use response.
func parseToolResponse(resp *api.Response) (*Decision, error) {
	toolUses := resp.GetToolUses()
	if len(toolUses) == 0 {
		return nil, fmt.Errorf("no tool_use blocks in response (stop_reason=%s)", resp.StopReason)
	}

	// Find our persist_decision tool call
	for _, block := range toolUses {
		if block.Name != persistToolName {
			continue
		}

		var decision Decision
		if err := json.Unmarshal(block.Input, &decision); err != nil {
			return nil, fmt.Errorf("unmarshal tool input: %w (raw: %s)", err, string(block.Input))
		}
		return &decision, nil
	}

	return nil, fmt.Errorf("persist_decision tool not called (found %d other tool calls)", len(toolUses))
}

// Apply executes a persistence decision by writing to L2.
// Creates new topic memories and updates relevance scores.
// Runs on engine context so it survives request cancellation.
func (p *Persister) Apply(ctx context.Context, decision *Decision, conversationID string) error {
	start := time.Now()
	var stored, updated int

	// 1. Store new topics
	for _, topic := range decision.NewTopics {
		mem := &tier.Memory{
			ID:             generateID(),
			ConversationID: conversationID,
			Content:        fmt.Sprintf("[%s]\n%s", topic.Name, topic.Content),
			TokenCount:     len(topic.Content) / 4,
			Relevance:      1.0,
			Tier:           tier.L2,
			Keywords:       topic.Keywords,
			CreatedAt:      time.Now(),
			LastAccessedAt: time.Now(),
		}

		if err := p.l2.Store(ctx, mem); err != nil {
			p.log.Warn("failed to store new topic",
				"topic", topic.Name,
				"error", err,
			)
			continue
		}
		stored++
	}

	// 2. Apply score updates
	for _, update := range decision.UpdatedScores {
		if err := p.l2.UpdateRelevance(ctx, update.TopicID, update.NewScore); err != nil {
			p.log.Warn("failed to update relevance score",
				"topic_id", update.TopicID,
				"new_score", update.NewScore,
				"error", err,
			)
			continue
		}
		updated++
	}

	// 3. ShouldEvict is noted but not acted on here — the Evictor runs
	// independently via REPL.evictor.CheckAndEvict and handles the full
	// L2→L4 migration pipeline (embed, store in L4, delete from L2, generate L3 summary).
	// Deleting from L2 here without L4 migration would cause data loss.
	if decision.ShouldEvict {
		p.log.Info("persister flagged eviction needed (handled by Evictor)")
	}

	p.log.Info("persistence applied",
		"duration", time.Since(start),
		"stored", stored,
		"updated", updated,
		"conversation", conversationID,
	)

	return nil
}

// generateID creates a unique ID for a new memory.
// Uses timestamp + atomic counter for ordering without importing uuid.
var idCounter int64

func generateID() string {
	n := atomic.AddInt64(&idCounter, 1)
	return fmt.Sprintf("persist-%d-%d", time.Now().UnixNano(), n)
}
