package api

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

// sseResponse joins SSE event strings with double newlines.
func sseResponse(events ...string) string {
    var b strings.Builder
    for _, e := range events {
        b.WriteString(e)
        b.WriteString("\n\n")
    }
    return b.String()
}

// simpleTextSSE returns a full SSE stream for a simple text response.
func simpleTextSSE(id, model, text string) string {
    return sseResponse(
        fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","content":[],"model":%q,"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`, id, model),
        `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
        fmt.Sprintf(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text),
        `event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
        `event: message_delta
data: {"type":"message_delta","delta":{"type":"message_delta","stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
        `event: message_stop
data: {"type":"message_stop"}`,
    )
}

func TestClient_Send_Success(t *testing.T) {
    respJSON := `{
        "id": "msg_01",
        "type": "message",
        "role": "assistant",
        "content": [{"type": "text", "text": "Hello!"}],
        "model": "claude-test",
        "stop_reason": "end_turn",
        "usage": {"input_tokens": 10, "output_tokens": 3}
    }`

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify headers
        if r.Header.Get("x-api-key") != "test-key" {
            t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "test-key")
        }
        if r.Header.Get("anthropic-version") != AnthropicVersion {
            t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), AnthropicVersion)
        }
        if r.Header.Get("Content-Type") != "application/json" {
            t.Errorf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), "application/json")
        }
        if r.Method != http.MethodPost {
            t.Errorf("method = %q, want POST", r.Method)
        }
        if r.URL.Path != "/v1/messages" {
            t.Errorf("path = %q, want /v1/messages", r.URL.Path)
        }

        // Verify request body is valid JSON
        body, readErr := io.ReadAll(r.Body)
        if readErr != nil {
            t.Fatalf("failed to read request body: %v", readErr)
        }
        var req Request
        if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
            t.Fatalf("failed to unmarshal request: %v", unmarshalErr)
        }
        if req.Model != "claude-test" {
            t.Errorf("request model = %q, want %q", req.Model, "claude-test")
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, respJSON)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    resp, err := client.Send(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.ID != "msg_01" {
        t.Errorf("ID = %q, want %q", resp.ID, "msg_01")
    }
    if resp.GetText() != "Hello!" {
        t.Errorf("GetText() = %q, want %q", resp.GetText(), "Hello!")
    }
    if resp.StopReason != StopReasonEndTurn {
        t.Errorf("StopReason = %q, want %q", resp.StopReason, StopReasonEndTurn)
    }
    if resp.Usage.InputTokens != 10 {
        t.Errorf("Usage.InputTokens = %d, want 10", resp.Usage.InputTokens)
    }
    if resp.Usage.OutputTokens != 3 {
        t.Errorf("Usage.OutputTokens = %d, want 3", resp.Usage.OutputTokens)
    }
}

func TestClient_Send_Error(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is required"}}`)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    _, err := client.Send(context.Background(), &Request{
        Model:    "claude-test",
        Messages: []MessageParam{NewUserMessage("Hi")},
    })
    if err == nil {
        t.Fatal("expected error, got nil")
    }

    var apiErr *APIError
    if !errors.As(err, &apiErr) {
        t.Fatalf("expected *APIError, got %T: %v", err, err)
    }
    if apiErr.StatusCode != 400 {
        t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
    }
    if apiErr.ErrorDetails.Type != ErrorTypeInvalidRequest {
        t.Errorf("ErrorDetails.Type = %q, want %q", apiErr.ErrorDetails.Type, ErrorTypeInvalidRequest)
    }
    if apiErr.ErrorDetails.Message != "max_tokens is required" {
        t.Errorf("ErrorDetails.Message = %q, want %q", apiErr.ErrorDetails.Message, "max_tokens is required")
    }
}

func TestClient_Send_RateLimit(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Retry-After", "30")
        w.WriteHeader(http.StatusTooManyRequests)
        fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"too many requests"}}`)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    _, err := client.Send(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err == nil {
        t.Fatal("expected error, got nil")
    }

    var apiErr *APIError
    if !errors.As(err, &apiErr) {
        t.Fatalf("expected *APIError, got %T: %v", err, err)
    }
    if apiErr.StatusCode != 429 {
        t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
    }
    if apiErr.ErrorDetails.Type != ErrorTypeRateLimit {
        t.Errorf("ErrorDetails.Type = %q, want %q", apiErr.ErrorDetails.Type, ErrorTypeRateLimit)
    }
    if apiErr.RetryAfter != 30*1_000_000_000 { // 30 seconds in nanoseconds
        t.Errorf("RetryAfter = %v, want 30s", apiErr.RetryAfter)
    }
}

func TestClient_Send_StreamError(t *testing.T) {
    client := NewClient("test-key")
    streamTrue := true
    _, err := client.Send(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
        Stream:    &streamTrue,
    })
    if err == nil {
        t.Fatal("expected error for stream=true with Send, got nil")
    }
    if !strings.Contains(err.Error(), "use SendStream") {
        t.Errorf("error = %q, want to contain 'use SendStream'", err.Error())
    }
}

func TestClient_CountTokens(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/messages/count_tokens" {
            t.Errorf("path = %q, want /v1/messages/count_tokens", r.URL.Path)
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, `{"input_tokens": 42}`)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    resp, err := client.CountTokens(context.Background(), &TokenCountRequest{
        Model:    "claude-test",
        Messages: []MessageParam{NewUserMessage("Hello")},
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.InputTokens != 42 {
        t.Errorf("InputTokens = %d, want 42", resp.InputTokens)
    }
}

func TestClient_Stream_SimpleText(t *testing.T) {
    body := simpleTextSSE("msg_stream", "claude-test", "Hello from stream!")

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, body)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    stream, err := client.Stream(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    resp, err := stream.Collect()
    if err != nil {
        t.Fatalf("Collect error: %v", err)
    }
    if resp.ID != "msg_stream" {
        t.Errorf("ID = %q, want %q", resp.ID, "msg_stream")
    }
    if resp.GetText() != "Hello from stream!" {
        t.Errorf("GetText() = %q, want %q", resp.GetText(), "Hello from stream!")
    }
    if resp.StopReason != StopReasonEndTurn {
        t.Errorf("StopReason = %q, want %q", resp.StopReason, StopReasonEndTurn)
    }
}

func TestClient_StreamWithCallback(t *testing.T) {
    body := simpleTextSSE("msg_cb", "claude-test", "Callback text")

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, body)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))

    var eventTypes []StreamEventType
    resp, err := client.StreamWithCallback(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    }, func(event *StreamEvent) error {
        eventTypes = append(eventTypes, event.Type)
        return nil
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // Verify callback received events
    if len(eventTypes) == 0 {
        t.Fatal("callback received zero events")
    }

    // Should start with message_start
    if eventTypes[0] != StreamEventMessageStart {
        t.Errorf("first event = %q, want %q", eventTypes[0], StreamEventMessageStart)
    }

    // Last event should be message_stop
    if eventTypes[len(eventTypes)-1] != StreamEventMessageStop {
        t.Errorf("last event = %q, want %q", eventTypes[len(eventTypes)-1], StreamEventMessageStop)
    }

    // Verify assembled response
    if resp.ID != "msg_cb" {
        t.Errorf("ID = %q, want %q", resp.ID, "msg_cb")
    }
    if resp.GetText() != "Callback text" {
        t.Errorf("GetText() = %q, want %q", resp.GetText(), "Callback text")
    }
}

func TestClient_StreamWithCallback_ErrorStopsStream(t *testing.T) {
    body := simpleTextSSE("msg_stop", "claude-test", "Will stop")

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, body)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))

    stopErr := errors.New("callback stop")
    _, err := client.StreamWithCallback(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    }, func(event *StreamEvent) error {
        // Stop after first event
        return stopErr
    })
    if !errors.Is(err, stopErr) {
        t.Errorf("error = %v, want %v", err, stopErr)
    }
}

func TestClient_Stream_HTTPError(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusUnauthorized)
        fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid key"}}`)
    }))
    defer srv.Close()

    client := NewClient("bad-key", WithBaseURL(srv.URL))
    _, err := client.Stream(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err == nil {
        t.Fatal("expected error, got nil")
    }

    var apiErr *APIError
    if !errors.As(err, &apiErr) {
        t.Fatalf("expected *APIError, got %T: %v", err, err)
    }
    if apiErr.StatusCode != 401 {
        t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
    }
}

func TestClient_Stream_WrongContentType(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, `{"type":"message"}`)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    _, err := client.Stream(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err == nil {
        t.Fatal("expected error for wrong content type, got nil")
    }
    if !strings.Contains(err.Error(), "unexpected content type") {
        t.Errorf("error = %q, want to contain 'unexpected content type'", err.Error())
    }
}

func TestClient_ListModels(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            t.Errorf("method = %q, want GET", r.Method)
        }
        if !strings.HasPrefix(r.URL.Path, "/v1/models") {
            t.Errorf("path = %q, want /v1/models...", r.URL.Path)
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, `{
            "data": [
                {"id": "claude-sonnet-4-5", "display_name": "Claude Sonnet 4.5", "type": "model", "created_at": "2025-01-01"},
                {"id": "claude-haiku-4-5", "display_name": "Claude Haiku 4.5", "type": "model", "created_at": "2025-01-01"}
            ],
            "has_more": false,
            "first_id": "claude-sonnet-4-5",
            "last_id": "claude-haiku-4-5"
        }`)
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    models, err := client.ListModels(context.Background())
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(models) != 2 {
        t.Fatalf("len(models) = %d, want 2", len(models))
    }
    if models[0].ID != "claude-sonnet-4-5" {
        t.Errorf("models[0].ID = %q, want %q", models[0].ID, "claude-sonnet-4-5")
    }
    if models[1].ID != "claude-haiku-4-5" {
        t.Errorf("models[1].ID = %q, want %q", models[1].ID, "claude-haiku-4-5")
    }
}

func TestClient_Options(t *testing.T) {
    t.Run("BaseURL", func(t *testing.T) {
        c := NewClient("key", WithBaseURL("https://custom.api.com"))
        if c.BaseURL() != "https://custom.api.com" {
            t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), "https://custom.api.com")
        }
    })

    t.Run("APIKey", func(t *testing.T) {
        c := NewClient("my-key")
        if c.APIKey() != "my-key" {
            t.Errorf("APIKey() = %q, want %q", c.APIKey(), "my-key")
        }
    })

    t.Run("DefaultBaseURL", func(t *testing.T) {
        c := NewClient("key")
        if c.BaseURL() != DefaultBaseURL {
            t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), DefaultBaseURL)
        }
    })
}

func TestClient_Send_WithRetry(t *testing.T) {
    calls := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if calls < 3 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusTooManyRequests)
            fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, `{"id":"msg_retry","type":"message","role":"assistant","content":[{"type":"text","text":"recovered"}],"model":"claude-test","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`)
    }))
    defer srv.Close()

    client := NewClient("test-key",
        WithBaseURL(srv.URL),
        WithRetryConfig(RetryConfig{
            MaxRetries:     3,
            InitialBackoff: time.Millisecond,
            MaxBackoff:     10 * time.Millisecond,
            Multiplier:     2.0,
            Jitter:         0,
        }),
    )

    resp, err := client.Send(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.GetText() != "recovered" {
        t.Errorf("GetText() = %q, want %q", resp.GetText(), "recovered")
    }
    if calls != 3 {
        t.Errorf("calls = %d, want 3", calls)
    }
}

func TestClient_parseErrorWithHeaders_UnparsableBody(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusBadGateway)
        fmt.Fprint(w, "bad gateway - not json")
    }))
    defer srv.Close()

    client := NewClient("test-key", WithBaseURL(srv.URL))
    _, err := client.Send(context.Background(), &Request{
        Model:     "claude-test",
        Messages:  []MessageParam{NewUserMessage("Hi")},
        MaxTokens: 100,
    })
    if err == nil {
        t.Fatal("expected error, got nil")
    }

    var apiErr *APIError
    if !errors.As(err, &apiErr) {
        t.Fatalf("expected *APIError, got %T: %v", err, err)
    }
    if apiErr.StatusCode != 502 {
        t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
    }
    // When body is not JSON, error type defaults to api_error
    if apiErr.ErrorDetails.Type != ErrorTypeAPI {
        t.Errorf("ErrorDetails.Type = %q, want %q", apiErr.ErrorDetails.Type, ErrorTypeAPI)
    }
}
