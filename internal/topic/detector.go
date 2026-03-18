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

	sys := api.NewSystemString("You are a topic analysis assistant. You respond with ONLY valid JSON, no markdown fences, no preamble, no explanation.")
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

	sys := api.NewSystemString("You are a topic analysis assistant. You respond with ONLY valid JSON, no markdown fences, no preamble, no explanation.")
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

	return fmt.Sprintf(`You are a topic analysis assistant. Identify the main topic of this conversation.

Messages:
%s

Task: Identify the main topic being discussed.

Respond with ONLY valid JSON in this exact format:
{
  "topic_name": "Short descriptive name (3-5 words)",
  "keywords": ["keyword1", "keyword2", "keyword3", "keyword4", "keyword5"]
}

The topic name should be concise but descriptive.
Keywords should be 3-5 relevant terms that capture the essence of the discussion.`, messagesText)
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
		currentTopicContext = fmt.Sprintf(`
Current Topic:
- Name: %s
- Keywords: %v
`, currentTopic.Name, currentTopic.Keywords)
	} else {
		currentTopicContext = "No current topic established."
	}

	return fmt.Sprintf(`You are a topic analysis assistant. Analyze the following conversation messages to determine if the topic has shifted.

%s

Recent Messages:
%s

Task: Determine if the conversation has shifted to a new topic.

A topic shift occurs when:
- The user explicitly introduces a new subject
- The conversation direction changes significantly
- A new domain or area of discussion begins

A topic shift does NOT occur when:
- The conversation continues on the same general subject
- The user asks follow-up questions on the same topic
- There are minor tangents that relate to the main topic

Respond with ONLY valid JSON in this exact format:
{
  "topic_shifted": true/false,
  "new_topic_name": "Short descriptive name (3-5 words)",
  "keywords": ["keyword1", "keyword2", "keyword3", "keyword4", "keyword5"],
  "confidence": 0.0-1.0,
  "reason": "Brief explanation"
}

If topic_shifted is false, leave new_topic_name empty and keywords as empty array.
Confidence should reflect how certain you are about the shift (or lack thereof).`, currentTopicContext, messagesText)
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
