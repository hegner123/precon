// Package topic provides topic detection and tracking for conversation context management.
package topic

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hegner123/precon/internal/api"
)

// DefaultDetectionModel is the default model for topic detection (fast and cheap).
const DefaultDetectionModel = api.ModelHaiku45Latest

// Message is a simplified message for topic analysis.
type Message struct {
	Role    string
	Content string
}

// Topic represents a detected conversation topic.
type Topic struct {
	Name     string
	Keywords []string
}

// Shift represents a detected shift in conversation topic.
type Shift struct {
	Detected   bool
	NewTopic   string
	Keywords   []string
	Confidence float64
	Reason     string
}

// topicIdentificationResponse is the expected JSON response from Claude for topic identification.
type topicIdentificationResponse struct {
	TopicName string   `json:"topic_name"`
	Keywords  []string `json:"keywords"`
}

// topicDetectionResponse is the expected JSON response from Claude for topic shift detection.
type topicDetectionResponse struct {
	TopicShifted bool     `json:"topic_shifted"`
	NewTopicName string   `json:"new_topic_name"`
	Keywords     []string `json:"keywords"`
	Confidence   float64  `json:"confidence"`
	Reason       string   `json:"reason"`
}

// Detector uses Claude to detect and identify conversation topics.
type Detector struct {
	log    *slog.Logger
	client *api.Client
	model  string
}

// NewDetector creates a new topic detector.
// If model is empty, uses claude-haiku-4-5 for fast, cheap detection.
func NewDetector(log *slog.Logger, client *api.Client, model string) *Detector {
	if model == "" {
		model = DefaultDetectionModel
	}
	return &Detector{
		log:    log,
		client: client,
		model:  model,
	}
}

// IdentifyTopic identifies the main topic from a set of messages.
func (d *Detector) IdentifyTopic(ctx context.Context, messages []Message) (*Topic, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to analyze")
	}

	prompt := buildTopicIdentificationPrompt(messages)

	sys := api.NewSystemString("Respond with ONLY a raw JSON object. No markdown fences, no preamble, no explanation, no text after the JSON.")
	req := &api.Request{
		Model:     d.model,
		MaxTokens: 300,
		System:    &sys,
		Messages: []api.MessageParam{
			api.NewUserMessage(prompt),
		},
	}

	d.log.Debug("sending topic identification request", "model", d.model, "message_count", len(messages))

	resp, err := d.client.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send topic identification request: %w", err)
	}

	text := resp.GetText()
	topic, err := parseTopicIdentificationResponse(text)
	if err != nil {
		d.log.Warn("failed to parse topic identification response, using fallback", "error", err)
		return &Topic{
			Name:     "General Discussion",
			Keywords: []string{"conversation"},
		}, nil
	}

	d.log.Info("identified topic", "name", topic.Name, "keywords", topic.Keywords)
	return topic, nil
}

// DetectShift analyzes recent messages to detect if the conversation topic has shifted.
func (d *Detector) DetectShift(ctx context.Context, messages []Message, currentTopic *Topic) (*Shift, error) {
	if len(messages) == 0 {
		return &Shift{Detected: false}, nil
	}

	prompt := buildTopicShiftPrompt(messages, currentTopic)

	sys := api.NewSystemString("Respond with ONLY a raw JSON object. No markdown fences, no preamble, no explanation, no text after the JSON.")
	req := &api.Request{
		Model:     d.model,
		MaxTokens: 500,
		System:    &sys,
		Messages: []api.MessageParam{
			api.NewUserMessage(prompt),
		},
	}

	d.log.Debug("sending topic shift detection request", "model", d.model, "message_count", len(messages))

	resp, err := d.client.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send topic shift detection request: %w", err)
	}

	text := resp.GetText()
	shift, err := parseTopicShiftResponse(text)
	if err != nil {
		d.log.Warn("failed to parse topic shift response, assuming no shift", "error", err)
		return &Shift{Detected: false}, nil
	}

	d.log.Info("topic shift detection complete",
		"detected", shift.Detected,
		"new_topic", shift.NewTopic,
		"confidence", shift.Confidence,
	)
	return shift, nil
}

// buildTopicIdentificationPrompt constructs the prompt for initial topic identification.
func buildTopicIdentificationPrompt(messages []Message) string {
	var b strings.Builder
	for i, msg := range messages {
		fmt.Fprintf(&b, "%d. [%s]: %s\n", i+1, msg.Role, truncateText(msg.Content, 500))
	}
	messagesText := b.String()

	return fmt.Sprintf(`Identify the main topic of this conversation.

Messages:
%s

Respond with ONLY a JSON object (no markdown fences, no text before or after):
{"topic_name": "Descriptive name, 3-5 words", "keywords": ["term1", "term2", "term3"]}

Topic name: be specific. "SQLite FTS5 index design" not "database work". Use the most precise description the messages support.
Keywords: 3-5 terms. Prefer specific nouns (file names, package names, technology names) over generic verbs (fix, add, change).`, messagesText)
}

// buildTopicShiftPrompt constructs the prompt for topic shift detection.
func buildTopicShiftPrompt(messages []Message, currentTopic *Topic) string {
	var b strings.Builder
	for i, msg := range messages {
		fmt.Fprintf(&b, "%d. [%s]: %s\n", i+1, msg.Role, truncateText(msg.Content, 500))
	}
	messagesText := b.String()

	var currentTopicContext string
	if currentTopic != nil {
		currentTopicContext = fmt.Sprintf("Current topic: %s [%s]",
			currentTopic.Name, strings.Join(currentTopic.Keywords, ", "))
	} else {
		currentTopicContext = "No current topic established."
	}

	return fmt.Sprintf(`Determine if the conversation topic has shifted.

%s

Recent messages:
%s

A shift IS: the user introduces an unrelated subject, the domain changes entirely, or work moves to a different part of the codebase with different concerns.

A shift is NOT: follow-up questions on the same topic, drilling deeper into the same problem, minor tangents that return to the main thread, or switching between related subtasks within the same project area.

Respond with ONLY a JSON object (no markdown fences, no text before or after):
{"topic_shifted": false, "new_topic_name": "", "keywords": [], "confidence": 0.9, "reason": "Still discussing X"}

Or if shifted:
{"topic_shifted": true, "new_topic_name": "Specific 3-5 word name", "keywords": ["term1", "term2", "term3"], "confidence": 0.8, "reason": "Moved from X to Y"}

Confidence: 0.9+ for clear cases, 0.5-0.8 for ambiguous, below 0.5 means you are guessing (prefer false when unsure).`, currentTopicContext, messagesText)
}

// parseTopicIdentificationResponse parses Claude's response for topic identification.
func parseTopicIdentificationResponse(text string) (*Topic, error) {
	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON found in response: %s", truncateText(text, 200))
	}

	var resp topicIdentificationResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse topic identification response: %w (json: %s)", err, jsonStr)
	}

	return &Topic{
		Name:     resp.TopicName,
		Keywords: resp.Keywords,
	}, nil
}

// parseTopicShiftResponse parses Claude's response for topic shift detection.
func parseTopicShiftResponse(text string) (*Shift, error) {
	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON found in response: %s", truncateText(text, 200))
	}

	var resp topicDetectionResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse topic detection response: %w (json: %s)", err, jsonStr)
	}

	return &Shift{
		Detected:   resp.TopicShifted,
		NewTopic:   resp.NewTopicName,
		Keywords:   resp.Keywords,
		Confidence: resp.Confidence,
		Reason:     resp.Reason,
	}, nil
}

// extractJSON attempts to extract a JSON object from text.
// It handles cases where the model may include extra text before/after the JSON.
func extractJSON(text string) string {
	start := -1
	end := -1
	depth := 0

	for i, ch := range text {
		if ch == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 && start != -1 {
				end = i + 1
				break
			}
		}
	}

	if start == -1 || end == -1 || end <= start {
		return ""
	}

	return text[start:end]
}

// truncateText truncates text to a maximum length, adding ellipsis if needed.
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

// Model returns the model being used for topic detection.
func (d *Detector) Model() string {
	return d.model
}

// SetModel updates the model used for topic detection.
func (d *Detector) SetModel(model string) {
	d.model = model
}
