package api

import (
	"encoding/json"
	"testing"
)

func TestBlockAssembler_Empty(t *testing.T) {
	asm := newBlockAssembler()
	_, err := asm.Response()
	if err == nil {
		t.Fatal("expected error from empty assembler, got nil")
	}
	want := "no message_start event received"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestBlockAssembler_SimpleText(t *testing.T) {
	asm := newBlockAssembler()

	// message_start
	asm.Process(&StreamEvent{
		Type: StreamEventMessageStart,
		Message: &Response{
			ID:    "msg_test",
			Type:  "message",
			Role:  RoleAssistant,
			Model: "claude-test",
			Usage: Usage{InputTokens: 10},
		},
	})

	// content_block_start (text)
	idx0 := 0
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx0,
		ContentBlock: &ContentBlock{
			Type: "text",
			Text: "",
		},
	})

	// content_block_delta x3
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "text_delta", Text: "Hello"},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "text_delta", Text: ", "},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "text_delta", Text: "world!"},
	})

	// content_block_stop
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx0,
	})

	// message_delta
	asm.Process(&StreamEvent{
		Type: StreamEventMessageDelta,
		Delta: &StreamDelta{
			Type:       "message_delta",
			StopReason: StopReasonEndTurn,
		},
		Usage: &Usage{OutputTokens: 5},
	})

	// message_stop
	asm.Process(&StreamEvent{Type: StreamEventMessageStop})

	resp, err := asm.Response()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "msg_test" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg_test")
	}
	if resp.Model != "claude-test" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-test")
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, StopReasonEndTurn)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage.OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Text != "Hello, world!" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello, world!")
	}
	if resp.GetText() != "Hello, world!" {
		t.Errorf("GetText() = %q, want %q", resp.GetText(), "Hello, world!")
	}
}

func TestBlockAssembler_Thinking(t *testing.T) {
	asm := newBlockAssembler()

	asm.Process(&StreamEvent{
		Type: StreamEventMessageStart,
		Message: &Response{
			ID:    "msg_think",
			Type:  "message",
			Role:  RoleAssistant,
			Model: "claude-test",
		},
	})

	// Thinking block
	idx0 := 0
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx0,
		ContentBlock: &ContentBlock{
			Type: "thinking",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "thinking_delta", Thinking: "Let me think"},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "thinking_delta", Thinking: " about this."},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "signature_delta", Signature: "sig_abc123"},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx0,
	})

	// Text block
	idx1 := 1
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx1,
		ContentBlock: &ContentBlock{
			Type: "text",
			Text: "",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx1,
		Delta: &StreamDelta{Type: "text_delta", Text: "Here is the answer."},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx1,
	})

	asm.Process(&StreamEvent{
		Type: StreamEventMessageDelta,
		Delta: &StreamDelta{
			Type:       "message_delta",
			StopReason: StopReasonEndTurn,
		},
	})
	asm.Process(&StreamEvent{Type: StreamEventMessageStop})

	resp, err := asm.Response()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}

	// Thinking block
	think := resp.Content[0]
	if think.Type != "thinking" {
		t.Errorf("Content[0].Type = %q, want %q", think.Type, "thinking")
	}
	if think.Thinking != "Let me think about this." {
		t.Errorf("Content[0].Thinking = %q, want %q", think.Thinking, "Let me think about this.")
	}
	if think.Signature != "sig_abc123" {
		t.Errorf("Content[0].Signature = %q, want %q", think.Signature, "sig_abc123")
	}
	if !think.IsThinking() {
		t.Error("Content[0].IsThinking() = false, want true")
	}

	// Text block
	text := resp.Content[1]
	if text.Type != "text" {
		t.Errorf("Content[1].Type = %q, want %q", text.Type, "text")
	}
	if text.Text != "Here is the answer." {
		t.Errorf("Content[1].Text = %q, want %q", text.Text, "Here is the answer.")
	}
}

func TestBlockAssembler_ToolUse(t *testing.T) {
	asm := newBlockAssembler()

	asm.Process(&StreamEvent{
		Type: StreamEventMessageStart,
		Message: &Response{
			ID:    "msg_tool",
			Type:  "message",
			Role:  RoleAssistant,
			Model: "claude-test",
		},
	})

	// Text block
	idx0 := 0
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx0,
		ContentBlock: &ContentBlock{
			Type: "text",
			Text: "",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "text_delta", Text: "I'll use a tool."},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx0,
	})

	// Tool use block
	idx1 := 1
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx1,
		ContentBlock: &ContentBlock{
			Type: "tool_use",
			ID:   "toolu_01",
			Name: "get_weather",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx1,
		Delta: &StreamDelta{Type: "input_json_delta", PartialJSON: `{"loc`},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx1,
		Delta: &StreamDelta{Type: "input_json_delta", PartialJSON: `ation": "SF`},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx1,
		Delta: &StreamDelta{Type: "input_json_delta", PartialJSON: `"}`},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx1,
	})

	asm.Process(&StreamEvent{
		Type: StreamEventMessageDelta,
		Delta: &StreamDelta{
			Type:       "message_delta",
			StopReason: StopReasonToolUse,
		},
	})
	asm.Process(&StreamEvent{Type: StreamEventMessageStop})

	resp, err := asm.Response()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}

	// Text block
	if resp.Content[0].Text != "I'll use a tool." {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "I'll use a tool.")
	}

	// Tool use block
	tool := resp.Content[1]
	if tool.Type != "tool_use" {
		t.Errorf("Content[1].Type = %q, want %q", tool.Type, "tool_use")
	}
	if tool.ID != "toolu_01" {
		t.Errorf("Content[1].ID = %q, want %q", tool.ID, "toolu_01")
	}
	if tool.Name != "get_weather" {
		t.Errorf("Content[1].Name = %q, want %q", tool.Name, "get_weather")
	}
	if !tool.IsToolUse() {
		t.Error("Content[1].IsToolUse() = false, want true")
	}

	// Verify assembled JSON input
	var input map[string]string
	if err := json.Unmarshal(tool.Input, &input); err != nil {
		t.Fatalf("failed to unmarshal tool input: %v", err)
	}
	if input["location"] != "SF" {
		t.Errorf("tool input location = %q, want %q", input["location"], "SF")
	}

	// Verify stop reason
	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, StopReasonToolUse)
	}
}

func TestBlockAssembler_MultipleTextBlocks(t *testing.T) {
	asm := newBlockAssembler()

	asm.Process(&StreamEvent{
		Type: StreamEventMessageStart,
		Message: &Response{
			ID:    "msg_multi",
			Type:  "message",
			Role:  RoleAssistant,
			Model: "claude-test",
		},
	})

	// First text block
	idx0 := 0
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx0,
		ContentBlock: &ContentBlock{
			Type: "text",
			Text: "",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx0,
		Delta: &StreamDelta{Type: "text_delta", Text: "First block."},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx0,
	})

	// Second text block
	idx1 := 1
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStart,
		Index: &idx1,
		ContentBlock: &ContentBlock{
			Type: "text",
			Text: "",
		},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockDelta,
		Index: &idx1,
		Delta: &StreamDelta{Type: "text_delta", Text: "Second block."},
	})
	asm.Process(&StreamEvent{
		Type:  StreamEventContentBlockStop,
		Index: &idx1,
	})

	asm.Process(&StreamEvent{
		Type: StreamEventMessageDelta,
		Delta: &StreamDelta{
			Type:       "message_delta",
			StopReason: StopReasonEndTurn,
		},
	})
	asm.Process(&StreamEvent{Type: StreamEventMessageStop})

	resp, err := asm.Response()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Text != "First block." {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "First block.")
	}
	if resp.Content[1].Text != "Second block." {
		t.Errorf("Content[1].Text = %q, want %q", resp.Content[1].Text, "Second block.")
	}
	if resp.GetText() != "First block.Second block." {
		t.Errorf("GetText() = %q, want %q", resp.GetText(), "First block.Second block.")
	}
}
