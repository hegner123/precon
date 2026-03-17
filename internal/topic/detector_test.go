package topic

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hegner123/precon/internal/api"
)

// mockMessagesResponse builds a complete Messages API JSON response with
// the given text content embedded in a single text content block.
func mockMessagesResponse(text string) []byte {
	resp := api.Response{
		ID:         "msg_test",
		Type:       "message",
		Role:       api.RoleAssistant,
		Model:      "claude-haiku-4-5",
		StopReason: api.StopReasonEndTurn,
		Content: []api.ContentBlock{
			{Type: "text", Text: text},
		},
		Usage: api.Usage{InputTokens: 10, OutputTokens: 20},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic("mockMessagesResponse marshal: " + err.Error())
	}
	return b
}

// newMockServer returns an httptest.Server that always responds with the
// given body bytes and HTTP 200 on /v1/messages.
func newMockServer(body []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
}

// newTestDetector creates a Detector backed by a mock HTTP server that
// returns the provided text as the assistant's response.
func newTestDetector(t *testing.T, responseText string) (*Detector, *httptest.Server) {
	t.Helper()
	server := newMockServer(mockMessagesResponse(responseText))
	client := api.NewClient("test-key", api.WithBaseURL(server.URL))
	log := slog.Default()
	det := NewDetector(log, client, api.ModelHaiku45Latest)
	return det, server
}

func TestDetector_IdentifyTopic(t *testing.T) {
	responseJSON := `{"topic_name":"Go testing","keywords":["go","test"]}`
	det, server := newTestDetector(t, responseJSON)
	defer server.Close()

	msgs := []Message{
		{Role: "user", Content: "How do I write tests in Go?"},
	}

	topic, err := det.IdentifyTopic(context.Background(), msgs)
	if err != nil {
		t.Fatal("IdentifyTopic returned unexpected error:", err)
	}

	if topic.Name != "Go testing" {
		t.Errorf("topic.Name = %q, want %q", topic.Name, "Go testing")
	}
	if len(topic.Keywords) != 2 {
		t.Errorf("len(topic.Keywords) = %d, want 2", len(topic.Keywords))
	} else {
		if topic.Keywords[0] != "go" {
			t.Errorf("topic.Keywords[0] = %q, want %q", topic.Keywords[0], "go")
		}
		if topic.Keywords[1] != "test" {
			t.Errorf("topic.Keywords[1] = %q, want %q", topic.Keywords[1], "test")
		}
	}
}

func TestDetector_IdentifyTopic_MalformedJSON(t *testing.T) {
	det, server := newTestDetector(t, "this is not json at all")
	defer server.Close()

	msgs := []Message{
		{Role: "user", Content: "Tell me about something"},
	}

	topic, err := det.IdentifyTopic(context.Background(), msgs)
	if err != nil {
		t.Fatal("IdentifyTopic should not return error on malformed JSON, got:", err)
	}

	// Should fall back to generic topic
	if topic.Name != "General Discussion" {
		t.Errorf("fallback topic.Name = %q, want %q", topic.Name, "General Discussion")
	}
	if len(topic.Keywords) != 1 || topic.Keywords[0] != "conversation" {
		t.Errorf("fallback topic.Keywords = %v, want [\"conversation\"]", topic.Keywords)
	}
}

func TestDetector_DetectShift(t *testing.T) {
	responseJSON := `{"topic_shifted":true,"new_topic_name":"New Topic","keywords":["new"],"confidence":0.9,"reason":"changed subject"}`
	det, server := newTestDetector(t, responseJSON)
	defer server.Close()

	currentTopic := &Topic{Name: "Old Topic", Keywords: []string{"old"}}
	msgs := []Message{
		{Role: "user", Content: "Let's talk about something completely different"},
	}

	shift, err := det.DetectShift(context.Background(), msgs, currentTopic)
	if err != nil {
		t.Fatal("DetectShift returned unexpected error:", err)
	}

	if shift.Detected != true {
		t.Error("shift.Detected = false, want true")
	}
	if shift.NewTopic != "New Topic" {
		t.Errorf("shift.NewTopic = %q, want %q", shift.NewTopic, "New Topic")
	}
	if len(shift.Keywords) != 1 || shift.Keywords[0] != "new" {
		t.Errorf("shift.Keywords = %v, want [\"new\"]", shift.Keywords)
	}
	if shift.Confidence != 0.9 {
		t.Errorf("shift.Confidence = %f, want 0.9", shift.Confidence)
	}
	if shift.Reason != "changed subject" {
		t.Errorf("shift.Reason = %q, want %q", shift.Reason, "changed subject")
	}
}

func TestDetector_DetectShift_NoShift(t *testing.T) {
	responseJSON := `{"topic_shifted":false,"new_topic_name":"","keywords":[],"confidence":0.8,"reason":"same topic"}`
	det, server := newTestDetector(t, responseJSON)
	defer server.Close()

	currentTopic := &Topic{Name: "Current Topic", Keywords: []string{"current"}}
	msgs := []Message{
		{Role: "user", Content: "Tell me more about the same thing"},
	}

	shift, err := det.DetectShift(context.Background(), msgs, currentTopic)
	if err != nil {
		t.Fatal("DetectShift returned unexpected error:", err)
	}

	if shift.Detected != false {
		t.Error("shift.Detected = true, want false")
	}
}

func TestDetector_DetectShift_MalformedJSON(t *testing.T) {
	det, server := newTestDetector(t, "not valid json here either!!!")
	defer server.Close()

	currentTopic := &Topic{Name: "Current Topic", Keywords: []string{"current"}}
	msgs := []Message{
		{Role: "user", Content: "Something new maybe"},
	}

	shift, err := det.DetectShift(context.Background(), msgs, currentTopic)
	if err != nil {
		t.Fatal("DetectShift should not return error on malformed JSON, got:", err)
	}

	if shift.Detected != false {
		t.Error("shift.Detected = true on malformed JSON, want false (graceful fallback)")
	}
}
