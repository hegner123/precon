package repl

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/hegner123/precon/internal/api"
)

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_Short(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestTruncate_Exact(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestTruncate_Long(t *testing.T) {
	got := truncate("hello world", 5)
	want := "hello..."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// estimateMessageTokens
// ---------------------------------------------------------------------------

func TestEstimateMessageTokens_String(t *testing.T) {
	msg := api.MessageParam{
		Role:    api.RoleUser,
		Content: "abcdefghijklmnop", // 16 chars => 4 tokens
	}
	got := estimateMessageTokens(msg)
	if got != 4 {
		t.Fatalf("expected 4, got %d", got)
	}
}

func TestEstimateMessageTokens_Blocks(t *testing.T) {
	blocks := []any{
		api.TextBlockParam{
			Type: api.ContentBlockTypeText,
			Text: strings.Repeat("a", 40), // 40/4 = 10
		},
		api.ToolUseBlockParam{
			Type:  api.ContentBlockTypeToolUse,
			ID:    "tu_1",
			Name:  "bash",
			Input: map[string]any{"command": "ls"}, // {"command":"ls"} = 16 chars => 4 tokens
		},
	}
	msg := api.MessageParam{
		Role:    api.RoleAssistant,
		Content: blocks,
	}
	got := estimateMessageTokens(msg)
	// Text block: 10, ToolUse block: marshaled JSON of {"command":"ls"} = 16 chars => 4
	// Total: 14
	if got < 10 {
		t.Fatalf("expected at least 10, got %d", got)
	}
}

func TestEstimateMessageTokens_Empty(t *testing.T) {
	msg := api.MessageParam{
		Role:    api.RoleUser,
		Content: nil,
	}
	got := estimateMessageTokens(msg)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// hasToolContent
// ---------------------------------------------------------------------------

func TestHasToolContent_NoTools(t *testing.T) {
	msg := api.MessageParam{
		Role:    api.RoleUser,
		Content: "just text",
	}
	if hasToolContent(msg) {
		t.Fatal("expected false for string content")
	}
}

func TestHasToolContent_WithToolUse(t *testing.T) {
	msg := api.MessageParam{
		Role: api.RoleAssistant,
		Content: []any{
			api.ToolUseBlockParam{
				Type:  api.ContentBlockTypeToolUse,
				ID:    "tu_1",
				Name:  "bash",
				Input: map[string]any{"command": "ls"},
			},
		},
	}
	if !hasToolContent(msg) {
		t.Fatal("expected true for ToolUseBlockParam")
	}
}

func TestHasToolContent_WithToolResult(t *testing.T) {
	msg := api.MessageParam{
		Role: api.RoleUser,
		Content: []any{
			api.ToolResultBlockParam{
				Type:      api.ContentBlockTypeToolResult,
				ToolUseID: "tu_1",
				Content:   "result text",
			},
		},
	}
	if !hasToolContent(msg) {
		t.Fatal("expected true for ToolResultBlockParam")
	}
}

func TestHasToolContent_TextOnly(t *testing.T) {
	msg := api.MessageParam{
		Role: api.RoleAssistant,
		Content: []any{
			api.TextBlockParam{
				Type: api.ContentBlockTypeText,
				Text: "just text blocks",
			},
		},
	}
	if hasToolContent(msg) {
		t.Fatal("expected false for text-only blocks")
	}
}

// ---------------------------------------------------------------------------
// assistantFromResponse
// ---------------------------------------------------------------------------

func TestAssistantFromResponse_TextOnly(t *testing.T) {
	resp := &api.Response{
		Role: api.RoleAssistant,
		Content: []api.ContentBlock{
			{Type: string(api.ContentBlockTypeText), Text: "hello world"},
		},
	}

	msg := assistantFromResponse(resp)

	if msg.Role != api.RoleAssistant {
		t.Fatalf("expected role %q, got %q", api.RoleAssistant, msg.Role)
	}

	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	tb, ok := blocks[0].(api.TextBlockParam)
	if !ok {
		t.Fatalf("expected TextBlockParam, got %T", blocks[0])
	}
	if tb.Text != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", tb.Text)
	}
}

func TestAssistantFromResponse_WithToolUse(t *testing.T) {
	resp := &api.Response{
		Role: api.RoleAssistant,
		Content: []api.ContentBlock{
			{Type: string(api.ContentBlockTypeText), Text: "let me check"},
			{
				Type:  string(api.ContentBlockTypeToolUse),
				ID:    "tu_abc",
				Name:  "bash",
				Input: json.RawMessage(`{"command":"ls"}`),
			},
		},
	}

	msg := assistantFromResponse(resp)
	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	// First block: text
	if _, ok := blocks[0].(api.TextBlockParam); !ok {
		t.Fatalf("expected TextBlockParam at index 0, got %T", blocks[0])
	}

	// Second block: tool use
	tu, ok := blocks[1].(api.ToolUseBlockParam)
	if !ok {
		t.Fatalf("expected ToolUseBlockParam at index 1, got %T", blocks[1])
	}
	if tu.ID != "tu_abc" {
		t.Fatalf("expected tool use ID %q, got %q", "tu_abc", tu.ID)
	}
	if tu.Name != "bash" {
		t.Fatalf("expected tool name %q, got %q", "bash", tu.Name)
	}
}

func TestAssistantFromResponse_WithThinking(t *testing.T) {
	resp := &api.Response{
		Role: api.RoleAssistant,
		Content: []api.ContentBlock{
			{
				Type:      string(api.ContentBlockTypeThinking),
				Thinking:  "let me reason about this",
				Signature: "sig_abc123",
			},
			{Type: string(api.ContentBlockTypeText), Text: "here is the answer"},
		},
	}

	msg := assistantFromResponse(resp)
	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	// First block: thinking
	tb, ok := blocks[0].(api.ThinkingBlock)
	if !ok {
		t.Fatalf("expected ThinkingBlock at index 0, got %T", blocks[0])
	}
	if tb.Thinking != "let me reason about this" {
		t.Fatalf("expected thinking text preserved, got %q", tb.Thinking)
	}
	if tb.Signature != "sig_abc123" {
		t.Fatalf("expected signature %q, got %q", "sig_abc123", tb.Signature)
	}

	// Second block: text
	if _, ok := blocks[1].(api.TextBlockParam); !ok {
		t.Fatalf("expected TextBlockParam at index 1, got %T", blocks[1])
	}
}

// ---------------------------------------------------------------------------
// prettyOutput (the user spec says "formatToolOutput" but the function is prettyOutput)
// ---------------------------------------------------------------------------

func TestPrettyOutput_ReadFile(t *testing.T) {
	output := `{"content":"file content here","file":"/path/to/file"}`
	got := prettyOutput(output, 500)
	// This is a generic JSON object — falls through to pretty-printed JSON fallback
	// since there's no specific "content"+"file" handler. Verify it doesn't panic
	// and returns something non-empty.
	if got == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestPrettyOutput_Bash(t *testing.T) {
	output := `{"stdout":"hello","stderr":"","exit_code":0}`
	got := prettyOutput(output, 500)
	// Generic JSON — status handler checks for "status" key. Falls to pretty JSON.
	if got == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestPrettyOutput_BashError(t *testing.T) {
	output := `{"stdout":"","stderr":"error msg","exit_code":1}`
	got := prettyOutput(output, 500)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestPrettyOutput_Write(t *testing.T) {
	output := `{"file":"/path","bytes_written":42,"status":"ok"}`
	got := prettyOutput(output, 500)
	// Has "status" and "file" keys — triggers the status handler
	if got != "/path: ok" {
		t.Fatalf("expected %q, got %q", "/path: ok", got)
	}
}

func TestPrettyOutput_Stump(t *testing.T) {
	output := `{"root":"/dir","stats":{"dirs":5,"files":10},"tree":[]}`
	got := prettyOutput(output, 500)
	want := "/dir: 5 dirs, 10 files"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Checkfor(t *testing.T) {
	output := `{"directories":[{"dir":"/x","matches_found":3}]}`
	got := prettyOutput(output, 500)
	want := "3 matches"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_NotJSON(t *testing.T) {
	output := "this is plain text that is not JSON at all"
	got := prettyOutput(output, 20)
	want := truncate(output, 20)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_UnknownJSON(t *testing.T) {
	output := `{"foo":"bar"}`
	got := prettyOutput(output, 500)
	// Falls through all known shapes to pretty-printed JSON
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	// Should contain the key from the JSON
	if !strings.Contains(got, "foo") {
		t.Fatalf("expected pretty-printed JSON to contain %q, got %q", "foo", got)
	}
}

func TestPrettyOutput_Empty(t *testing.T) {
	got := prettyOutput("", 500)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// pruneL1
// ---------------------------------------------------------------------------

func newTestREPL(maxContext, responseReserve int, messages []api.MessageParam) *REPL {
	return &REPL{
		config: Config{
			MaxContextTokens: maxContext,
			ResponseReserve:  responseReserve,
		},
		messages: messages,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// makeTextMsg creates a user or assistant message with a string of given length.
func makeTextMsg(role api.Role, charCount int) api.MessageParam {
	return api.MessageParam{
		Role:    role,
		Content: strings.Repeat("x", charCount),
	}
}

// makeToolMsg creates a message containing a ToolUseBlockParam.
func makeToolMsg(role api.Role) api.MessageParam {
	return api.MessageParam{
		Role: role,
		Content: []any{
			api.ToolUseBlockParam{
				Type:  api.ContentBlockTypeToolUse,
				ID:    "tu_test",
				Name:  "bash",
				Input: map[string]any{"command": "echo hi"},
			},
		},
	}
}

func TestPruneL1_UnderBudget(t *testing.T) {
	msgs := make([]api.MessageParam, 5)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = makeTextMsg(api.RoleUser, 40) // 10 tokens each
		} else {
			msgs[i] = makeTextMsg(api.RoleAssistant, 40)
		}
	}
	// Total: 5 * 10 = 50 tokens, budget = 1000 - 200 = 800
	r := newTestREPL(1000, 200, msgs)
	r.pruneL1()

	if len(r.messages) != 5 {
		t.Fatalf("expected 5 messages (no pruning), got %d", len(r.messages))
	}
}

func TestPruneL1_OverBudget(t *testing.T) {
	// Budget = 10000 - 1000 = 9000 tokens
	// Each message: 400 chars => 100 tokens
	// 20 messages => 2000 tokens total
	// Protected tail = 12 messages => 1200 tokens (fits in budget)
	// Prune zone = 8 messages => 800 tokens
	// We need total > budget. Use larger messages.
	//
	// 20 messages of 2000 chars each => 500 tokens each => 10000 total
	// Budget = 10000 - 1000 = 9000
	// Protected tail = 12 * 500 = 6000 (under budget)
	// Prune zone = 8 * 500 = 4000
	// So pruning the prune zone should bring us from 10000 to ~6000
	msgs := make([]api.MessageParam, 20)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = makeTextMsg(api.RoleUser, 2000)
		} else {
			msgs[i] = makeTextMsg(api.RoleAssistant, 2000)
		}
	}

	r := newTestREPL(10000, 1000, msgs)
	before := r.estimateL1Tokens()
	if before <= 9000 {
		t.Fatalf("precondition: total tokens %d should exceed budget 9000", before)
	}

	r.pruneL1()

	// Should have pruned some messages from the prune zone
	if len(r.messages) >= 20 {
		t.Fatalf("expected pruning to occur, still have %d messages", len(r.messages))
	}

	// Protected tail (last 12 of original) should all be present
	lastOriginal := msgs[len(msgs)-1]
	lastKept := r.messages[len(r.messages)-1]
	if lastOriginal.Content != lastKept.Content {
		t.Fatal("expected last original message to be preserved in result")
	}

	// Verify we're at or under budget now
	estimated := r.estimateL1Tokens()
	budget := 9000
	if estimated > budget {
		t.Fatalf("expected tokens <= %d after pruning, got %d", budget, estimated)
	}
}

func TestPruneL1_ProtectsToolMessages(t *testing.T) {
	// Budget = 1000 - 200 = 800 tokens
	// 20 messages of 400 chars each => 2000 tokens
	// Place a tool message at index 2 (in the prune zone)
	msgs := make([]api.MessageParam, 20)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = makeTextMsg(api.RoleUser, 400)
		} else {
			msgs[i] = makeTextMsg(api.RoleAssistant, 400)
		}
	}
	// Replace message at index 2 with a tool message
	msgs[2] = makeToolMsg(api.RoleAssistant)

	r := newTestREPL(1000, 200, msgs)
	r.pruneL1()

	// The tool message at original index 2 should be preserved
	foundTool := false
	for _, msg := range r.messages {
		if hasToolContent(msg) {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatal("expected tool message to be preserved during pruning")
	}
}

// ---------------------------------------------------------------------------
// Additional edge cases
// ---------------------------------------------------------------------------

func TestAssistantFromResponse_RedactedThinking(t *testing.T) {
	resp := &api.Response{
		Role: api.RoleAssistant,
		Content: []api.ContentBlock{
			{
				Type: string(api.ContentBlockTypeRedactedThink),
				Data: "redacted_data_blob",
			},
			{Type: string(api.ContentBlockTypeText), Text: "visible answer"},
		},
	}

	msg := assistantFromResponse(resp)
	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	rtb, ok := blocks[0].(api.RedactedThinkingBlock)
	if !ok {
		t.Fatalf("expected RedactedThinkingBlock at index 0, got %T", blocks[0])
	}
	if rtb.Data != "redacted_data_blob" {
		t.Fatalf("expected data %q, got %q", "redacted_data_blob", rtb.Data)
	}
}

func TestEstimateMessageTokens_ToolResultBlock(t *testing.T) {
	blocks := []any{
		api.ToolResultBlockParam{
			Type:      api.ContentBlockTypeToolResult,
			ToolUseID: "tu_1",
			Content:   strings.Repeat("r", 80), // 80/4 = 20
		},
	}
	msg := api.MessageParam{
		Role:    api.RoleUser,
		Content: blocks,
	}
	got := estimateMessageTokens(msg)
	if got != 20 {
		t.Fatalf("expected 20, got %d", got)
	}
}

func TestPrettyOutput_CleanDiff(t *testing.T) {
	output := `{"summary":{"files_changed":3,"insertions":10,"deletions":5},"files":[]}`
	got := prettyOutput(output, 500)
	want := "3 files changed, +10 -5"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Imports(t *testing.T) {
	output := `{"summary":{"total_files":4,"total_imports":12},"files":[]}`
	got := prettyOutput(output, 500)
	want := "12 imports across 4 files"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Repfor(t *testing.T) {
	output := `{"files_modified":2,"replacements":7}`
	got := prettyOutput(output, 500)
	want := "7 replacements in 2 files"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Sig(t *testing.T) {
	output := `{"file":"main.go","functions":[{},{}],"types":[{}]}`
	got := prettyOutput(output, 500)
	want := "main.go: 2 functions, 1 types"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Errs(t *testing.T) {
	output := `{"count":5,"files":3,"format":"go"}`
	got := prettyOutput(output, 500)
	want := "5 errors in 3 files (go)"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Delete(t *testing.T) {
	output := `{"original_path":"/tmp/foo.txt","trash_path":"/Users/me/.Trash/foo.txt"}`
	got := prettyOutput(output, 500)
	want := "moved /tmp/foo.txt to Trash"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Conflicts(t *testing.T) {
	output := `{"total":2,"has_diff3":false,"files":[]}`
	got := prettyOutput(output, 500)
	want := "2 conflicts"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_Matches(t *testing.T) {
	output := `{"matches":["a","b","c"]}`
	got := prettyOutput(output, 500)
	want := "3 matches"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrettyOutput_GenericSummaryString(t *testing.T) {
	output := `{"summary":"all good"}`
	got := prettyOutput(output, 500)
	if got != "all good" {
		t.Fatalf("expected %q, got %q", "all good", got)
	}
}

func TestPrettyOutput_GenericStatus(t *testing.T) {
	output := `{"status":"completed"}`
	got := prettyOutput(output, 500)
	if got != "completed" {
		t.Fatalf("expected %q, got %q", "completed", got)
	}
}
