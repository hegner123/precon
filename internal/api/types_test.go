package api

import (
	"encoding/json"
	"testing"
)

func TestResponse_GetText(t *testing.T) {
	t.Run("MultipleBlocks", func(t *testing.T) {
		resp := &Response{
			Content: []ContentBlock{
				{Type: "text", Text: "Hello "},
				{Type: "tool_use", Name: "get_weather"},
				{Type: "text", Text: "world!"},
			},
		}
		got := resp.GetText()
		want := "Hello world!"
		if got != want {
			t.Errorf("GetText() = %q, want %q", got, want)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		resp := &Response{
			Content: []ContentBlock{},
		}
		got := resp.GetText()
		if got != "" {
			t.Errorf("GetText() = %q, want empty string", got)
		}
	})

	t.Run("NilContent", func(t *testing.T) {
		resp := &Response{}
		got := resp.GetText()
		if got != "" {
			t.Errorf("GetText() = %q, want empty string", got)
		}
	})

	t.Run("OnlyToolUse", func(t *testing.T) {
		resp := &Response{
			Content: []ContentBlock{
				{Type: "tool_use", Name: "search", ID: "toolu_1"},
			},
		}
		got := resp.GetText()
		if got != "" {
			t.Errorf("GetText() = %q, want empty string (no text blocks)", got)
		}
	})
}

func TestResponse_HasToolUse(t *testing.T) {
	t.Run("WithToolUse", func(t *testing.T) {
		resp := &Response{
			StopReason: StopReasonToolUse,
			Content: []ContentBlock{
				{Type: "text", Text: "Using a tool"},
				{Type: "tool_use", Name: "search"},
			},
		}
		if !resp.HasToolUse() {
			t.Error("HasToolUse() = false, want true")
		}
	})

	t.Run("WithoutToolUse", func(t *testing.T) {
		resp := &Response{
			StopReason: StopReasonEndTurn,
			Content: []ContentBlock{
				{Type: "text", Text: "Just text"},
			},
		}
		if resp.HasToolUse() {
			t.Error("HasToolUse() = true, want false")
		}
	})
}

func TestResponse_GetToolUses(t *testing.T) {
	resp := &Response{
		Content: []ContentBlock{
			{Type: "text", Text: "Using tools"},
			{Type: "tool_use", Name: "search", ID: "toolu_1"},
			{Type: "text", Text: "More text"},
			{Type: "tool_use", Name: "read_file", ID: "toolu_2"},
		},
	}
	tools := resp.GetToolUses()
	if len(tools) != 2 {
		t.Fatalf("len(GetToolUses()) = %d, want 2", len(tools))
	}
	if tools[0].Name != "search" {
		t.Errorf("tools[0].Name = %q, want %q", tools[0].Name, "search")
	}
	if tools[1].Name != "read_file" {
		t.Errorf("tools[1].Name = %q, want %q", tools[1].Name, "read_file")
	}
}

func TestContentBlock_IsText(t *testing.T) {
	tests := []struct {
		name string
		cb   ContentBlock
		want bool
	}{
		{"Text", ContentBlock{Type: "text"}, true},
		{"ToolUse", ContentBlock{Type: "tool_use"}, false},
		{"Thinking", ContentBlock{Type: "thinking"}, false},
		{"Empty", ContentBlock{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cb.IsText(); got != tt.want {
				t.Errorf("IsText() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentBlock_IsToolUse(t *testing.T) {
	tests := []struct {
		name string
		cb   ContentBlock
		want bool
	}{
		{"ToolUse", ContentBlock{Type: "tool_use"}, true},
		{"Text", ContentBlock{Type: "text"}, false},
		{"Thinking", ContentBlock{Type: "thinking"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cb.IsToolUse(); got != tt.want {
				t.Errorf("IsToolUse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentBlock_IsThinking(t *testing.T) {
	tests := []struct {
		name string
		cb   ContentBlock
		want bool
	}{
		{"Thinking", ContentBlock{Type: "thinking"}, true},
		{"Text", ContentBlock{Type: "text"}, false},
		{"ToolUse", ContentBlock{Type: "tool_use"}, false},
		{"RedactedThinking", ContentBlock{Type: "redacted_thinking"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cb.IsThinking(); got != tt.want {
				t.Errorf("IsThinking() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentBlock_GetToolInput(t *testing.T) {
	t.Run("ValidJSON", func(t *testing.T) {
		cb := ContentBlock{
			Type:  "tool_use",
			Input: json.RawMessage(`{"query":"test","limit":10}`),
		}
		var target map[string]any
		if err := cb.GetToolInput(&target); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target["query"] != "test" {
			t.Errorf("query = %v, want %q", target["query"], "test")
		}
		limit, ok := target["limit"].(float64)
		if !ok || limit != 10 {
			t.Errorf("limit = %v, want 10", target["limit"])
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		cb := ContentBlock{
			Type: "tool_use",
		}
		var target map[string]any
		if err := cb.GetToolInput(&target); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// target remains nil/empty
	})
}

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("Hello, Claude!")
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	text, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("Content type = %T, want string", msg.Content)
	}
	if text != "Hello, Claude!" {
		t.Errorf("Content = %q, want %q", text, "Hello, Claude!")
	}
}

func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage("I can help!")
	if msg.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", msg.Role, RoleAssistant)
	}
	text, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("Content type = %T, want string", msg.Content)
	}
	if text != "I can help!" {
		t.Errorf("Content = %q, want %q", text, "I can help!")
	}
}

func TestNewUserMessageBlocks(t *testing.T) {
	textBlock := NewTextBlock("What's in this image?")
	imgBlock := NewBase64ImageBlock(ImageMediaTypePNG, "iVBOR...")
	msg := NewUserMessageBlocks(textBlock, imgBlock)

	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("Content type = %T, want []any", msg.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
}

func TestSystemParam_MarshalJSON(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		sys := NewSystemString("You are a helpful assistant.")
		data, err := json.Marshal(sys)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `"You are a helpful assistant."`
		if string(data) != want {
			t.Errorf("MarshalJSON() = %s, want %s", string(data), want)
		}
	})

	t.Run("Blocks", func(t *testing.T) {
		sys := NewSystemBlocks(
			NewTextBlock("You are a helpful assistant."),
			NewTextBlockWithCache("Remember this context.", WithEphemeralCache()),
		)
		data, err := json.Marshal(sys)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify it marshals as an array
		var arr []json.RawMessage
		if unmarshalErr := json.Unmarshal(data, &arr); unmarshalErr != nil {
			t.Fatalf("expected array, got error: %v", unmarshalErr)
		}
		if len(arr) != 2 {
			t.Errorf("len(blocks) = %d, want 2", len(arr))
		}

		// Verify first block
		var block TextBlockParam
		if unmarshalErr := json.Unmarshal(arr[0], &block); unmarshalErr != nil {
			t.Fatalf("failed to unmarshal block: %v", unmarshalErr)
		}
		if block.Text != "You are a helpful assistant." {
			t.Errorf("block.Text = %q, want %q", block.Text, "You are a helpful assistant.")
		}
		if block.CacheControl != nil {
			t.Errorf("first block should not have cache control")
		}

		// Verify second block has cache control
		var block2 TextBlockParam
		if unmarshalErr := json.Unmarshal(arr[1], &block2); unmarshalErr != nil {
			t.Fatalf("failed to unmarshal block2: %v", unmarshalErr)
		}
		if block2.CacheControl == nil {
			t.Fatal("second block should have cache control")
		}
		if block2.CacheControl.Type != "ephemeral" {
			t.Errorf("cache_control.type = %q, want %q", block2.CacheControl.Type, "ephemeral")
		}
	})
}

func TestSystemParam_UnmarshalJSON(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		var sys SystemParam
		data := []byte(`"You are helpful."`)
		if err := json.Unmarshal(data, &sys); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Re-marshal to verify roundtrip
		out, err := json.Marshal(sys)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}
		if string(out) != `"You are helpful."` {
			t.Errorf("roundtrip = %s, want %s", string(out), `"You are helpful."`)
		}
	})

	t.Run("Blocks", func(t *testing.T) {
		var sys SystemParam
		data := []byte(`[{"type":"text","text":"Hello"}]`)
		if err := json.Unmarshal(data, &sys); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, err := json.Marshal(sys)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}
		// Should roundtrip as array
		var arr []json.RawMessage
		if unmarshalErr := json.Unmarshal(out, &arr); unmarshalErr != nil {
			t.Fatalf("expected array after roundtrip: %v", unmarshalErr)
		}
		if len(arr) != 1 {
			t.Errorf("len(arr) = %d, want 1", len(arr))
		}
	})
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Type: "error",
		ErrorDetails: ErrorDetail{
			Type:    ErrorTypeRateLimit,
			Message: "too many requests",
		},
		StatusCode: 429,
	}
	want := "rate_limit_error: too many requests"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestAPIError_Methods(t *testing.T) {
	t.Run("IsRateLimited", func(t *testing.T) {
		err := &APIError{ErrorDetails: ErrorDetail{Type: ErrorTypeRateLimit}}
		if !err.IsRateLimited() {
			t.Error("IsRateLimited() = false, want true")
		}
		err2 := &APIError{ErrorDetails: ErrorDetail{Type: ErrorTypeOverloaded}}
		if err2.IsRateLimited() {
			t.Error("IsRateLimited() = true, want false")
		}
	})

	t.Run("IsOverloaded", func(t *testing.T) {
		err := &APIError{ErrorDetails: ErrorDetail{Type: ErrorTypeOverloaded}}
		if !err.IsOverloaded() {
			t.Error("IsOverloaded() = false, want true")
		}
	})

	t.Run("IsRetryable", func(t *testing.T) {
		tests := []struct {
			errType ErrorType
			want    bool
		}{
			{ErrorTypeRateLimit, true},
			{ErrorTypeOverloaded, true},
			{ErrorTypeAPI, true},
			{ErrorTypeAuthentication, false},
			{ErrorTypeInvalidRequest, false},
			{ErrorTypePermission, false},
			{ErrorTypeNotFound, false},
		}
		for _, tt := range tests {
			err := &APIError{ErrorDetails: ErrorDetail{Type: tt.errType}}
			if got := err.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() for %q = %v, want %v", tt.errType, got, tt.want)
			}
		}
	})
}

func TestUsage_TotalInputTokens(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		u := Usage{InputTokens: 100}
		if got := u.TotalInputTokens(); got != 100 {
			t.Errorf("TotalInputTokens() = %d, want 100", got)
		}
	})

	t.Run("WithCacheTokens", func(t *testing.T) {
		cacheCreate := 50
		cacheRead := 30
		u := Usage{
			InputTokens:              100,
			CacheCreationInputTokens: &cacheCreate,
			CacheReadInputTokens:     &cacheRead,
		}
		want := 180
		if got := u.TotalInputTokens(); got != want {
			t.Errorf("TotalInputTokens() = %d, want %d", got, want)
		}
	})

	t.Run("NilCacheTokens", func(t *testing.T) {
		u := Usage{InputTokens: 100}
		if got := u.TotalInputTokens(); got != 100 {
			t.Errorf("TotalInputTokens() = %d, want 100", got)
		}
	})
}

func TestNewTextBlock(t *testing.T) {
	block := NewTextBlock("hello")
	if block.Type != ContentBlockTypeText {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeText)
	}
	if block.Text != "hello" {
		t.Errorf("Text = %q, want %q", block.Text, "hello")
	}
	if block.CacheControl != nil {
		t.Error("CacheControl should be nil")
	}
}

func TestNewTextBlockWithCache(t *testing.T) {
	cache := WithEphemeralCache()
	block := NewTextBlockWithCache("cached text", cache)
	if block.Type != ContentBlockTypeText {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeText)
	}
	if block.Text != "cached text" {
		t.Errorf("Text = %q, want %q", block.Text, "cached text")
	}
	if block.CacheControl == nil {
		t.Fatal("CacheControl should not be nil")
	}
	if block.CacheControl.Type != "ephemeral" {
		t.Errorf("CacheControl.Type = %q, want %q", block.CacheControl.Type, "ephemeral")
	}
}

func TestTextBlockParam_SetCacheControl(t *testing.T) {
	block := NewTextBlock("test")
	cached := block.SetCacheControl(WithLongCache())

	// Original should be unchanged
	if block.CacheControl != nil {
		t.Error("original block should not have cache control")
	}
	// New copy should have cache
	if cached.CacheControl == nil {
		t.Fatal("cached block should have cache control")
	}
	ttl := CacheTTL1Hour
	if cached.CacheControl.TTL == nil || *cached.CacheControl.TTL != ttl {
		t.Errorf("TTL = %v, want %v", cached.CacheControl.TTL, ttl)
	}
}

func TestCacheControl_Helpers(t *testing.T) {
	t.Run("WithCache", func(t *testing.T) {
		cc := WithCache(CacheTTL5Min)
		if cc.Type != "ephemeral" {
			t.Errorf("Type = %q, want %q", cc.Type, "ephemeral")
		}
		if cc.TTL == nil || *cc.TTL != CacheTTL5Min {
			t.Errorf("TTL = %v, want %v", cc.TTL, CacheTTL5Min)
		}
	})

	t.Run("WithEphemeralCache", func(t *testing.T) {
		cc := WithEphemeralCache()
		if cc.TTL == nil || *cc.TTL != CacheTTL5Min {
			t.Errorf("TTL = %v, want %v", cc.TTL, CacheTTL5Min)
		}
	})

	t.Run("WithLongCache", func(t *testing.T) {
		cc := WithLongCache()
		if cc.TTL == nil || *cc.TTL != CacheTTL1Hour {
			t.Errorf("TTL = %v, want %v", cc.TTL, CacheTTL1Hour)
		}
	})

	t.Run("NewCacheControl", func(t *testing.T) {
		ttl := CacheTTL1Hour
		cc := NewCacheControl(&ttl)
		if cc.Type != "ephemeral" {
			t.Errorf("Type = %q, want %q", cc.Type, "ephemeral")
		}
		if cc.TTL == nil || *cc.TTL != CacheTTL1Hour {
			t.Errorf("TTL = %v, want %v", cc.TTL, CacheTTL1Hour)
		}
	})

	t.Run("NewCacheControl_NilTTL", func(t *testing.T) {
		cc := NewCacheControl(nil)
		if cc.Type != "ephemeral" {
			t.Errorf("Type = %q, want %q", cc.Type, "ephemeral")
		}
		if cc.TTL != nil {
			t.Errorf("TTL = %v, want nil", cc.TTL)
		}
	})
}

func TestToolChoice(t *testing.T) {
	t.Run("Auto", func(t *testing.T) {
		if ToolChoiceAuto.Type != "auto" {
			t.Errorf("Type = %q, want %q", ToolChoiceAuto.Type, "auto")
		}
	})
	t.Run("Any", func(t *testing.T) {
		if ToolChoiceAny.Type != "any" {
			t.Errorf("Type = %q, want %q", ToolChoiceAny.Type, "any")
		}
	})
	t.Run("None", func(t *testing.T) {
		if ToolChoiceNone.Type != "none" {
			t.Errorf("Type = %q, want %q", ToolChoiceNone.Type, "none")
		}
	})
	t.Run("SpecificTool", func(t *testing.T) {
		tc := ToolChoiceTool("get_weather")
		if tc.Type != "tool" {
			t.Errorf("Type = %q, want %q", tc.Type, "tool")
		}
		if tc.Name != "get_weather" {
			t.Errorf("Name = %q, want %q", tc.Name, "get_weather")
		}
	})
}

func TestNewToolResultBlock(t *testing.T) {
	block := NewToolResultBlock("toolu_01", "sunny in SF")
	if block.Type != ContentBlockTypeToolResult {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeToolResult)
	}
	if block.ToolUseID != "toolu_01" {
		t.Errorf("ToolUseID = %q, want %q", block.ToolUseID, "toolu_01")
	}
	content, ok := block.Content.(string)
	if !ok {
		t.Fatalf("Content type = %T, want string", block.Content)
	}
	if content != "sunny in SF" {
		t.Errorf("Content = %q, want %q", content, "sunny in SF")
	}
	if block.IsError != nil {
		t.Error("IsError should be nil for success result")
	}
}

func TestNewToolResultBlockError(t *testing.T) {
	block := NewToolResultBlockError("toolu_02", "tool failed")
	if block.Type != ContentBlockTypeToolResult {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeToolResult)
	}
	if block.ToolUseID != "toolu_02" {
		t.Errorf("ToolUseID = %q, want %q", block.ToolUseID, "toolu_02")
	}
	if block.IsError == nil || !*block.IsError {
		t.Error("IsError should be true")
	}
}

func TestNewBase64ImageBlock(t *testing.T) {
	block := NewBase64ImageBlock(ImageMediaTypePNG, "iVBORw0KGgo=")
	if block.Type != ContentBlockTypeImage {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeImage)
	}
	if block.Source.Type != "base64" {
		t.Errorf("Source.Type = %q, want %q", block.Source.Type, "base64")
	}
	if block.Source.MediaType != ImageMediaTypePNG {
		t.Errorf("Source.MediaType = %q, want %q", block.Source.MediaType, ImageMediaTypePNG)
	}
	if block.Source.Data != "iVBORw0KGgo=" {
		t.Errorf("Source.Data = %q, want %q", block.Source.Data, "iVBORw0KGgo=")
	}
}

func TestNewURLImageBlock(t *testing.T) {
	block := NewURLImageBlock("https://example.com/img.png")
	if block.Type != ContentBlockTypeImage {
		t.Errorf("Type = %q, want %q", block.Type, ContentBlockTypeImage)
	}
	if block.Source.Type != "url" {
		t.Errorf("Source.Type = %q, want %q", block.Source.Type, "url")
	}
	if block.Source.URL != "https://example.com/img.png" {
		t.Errorf("Source.URL = %q, want %q", block.Source.URL, "https://example.com/img.png")
	}
}

func TestRequest_MarshalJSON(t *testing.T) {
	req := &Request{
		Model:     "claude-test",
		Messages:  []MessageParam{NewUserMessage("Hi")},
		MaxTokens: 1024,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
		t.Fatalf("not valid JSON: %v", unmarshalErr)
	}
	if parsed["model"] != "claude-test" {
		t.Errorf("model = %v, want %q", parsed["model"], "claude-test")
	}
	maxTokens, ok := parsed["max_tokens"].(float64)
	if !ok || maxTokens != 1024 {
		t.Errorf("max_tokens = %v, want 1024", parsed["max_tokens"])
	}
}
