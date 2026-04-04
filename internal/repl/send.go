package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	lip "charm.land/lipgloss/v2"
	"github.com/hegner123/bezel"
	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/tier"
)

// maxToolIterations is the maximum number of agentic tool loop iterations.
// Prevents unbounded API calls and tool execution if the model enters an infinite loop.
const maxToolIterations = 50

// SGR escape sequences for streaming thinking output.
// Matches thinkingStyle: italic + #C4B5FD (RGB 196, 181, 253).
const (
	thinkingSGROpen  = "\033[3;38;2;196;181;253m"
	thinkingSGRClose = "\033[0m"
)

// streamResult holds the outcome of one streaming API call.
type streamResult struct {
	resp     *api.Response // assembled response (for tool_use detection, usage, etc.)
	text     string        // accumulated text for L1/persistence
	thinking string        // accumulated thinking for persistence
	err      error
}

// streamWithBezel runs one streaming API call multiplexed with bezel events.
// Tokens are written to stdout as they arrive. Bezel events (resize, typing,
// Ctrl-C) are handled concurrently via select.
func (r *REPL) streamWithBezel(ctx context.Context, req *api.Request) streamResult {
	stream, err := r.client.Stream(ctx, req)
	if err != nil {
		return streamResult{err: err}
	}
	defer stream.Close()

	// Feed stream events into a channel from a background goroutine.
	eventCh := make(chan *api.StreamEvent, 8)
	errCh := make(chan error, 1)
	go func() {
		defer close(eventCh)
		for {
			ev, err := stream.Next()
			if err != nil {
				if err != io.EOF {
					select {
					case errCh <- err:
					default:
					}
				}
				return
			}
			select {
			case eventCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	asm := api.NewBlockAssembler()
	var text, thinking strings.Builder
	inThinking := false

	for {
		select {
		case ev, ok := <-r.bz.Events():
			if !ok {
				if inThinking {
					os.Stdout.WriteString(thinkingSGRClose + "\n")
				}
				return streamResult{err: context.Canceled}
			}
			if ev.Type == bezel.EventResize {
				r.redraw("streaming...")
				continue
			}
			action, _ := r.ed.HandleEvent(ev, r.km, &r.hist)
			if action == bezel.ActionQuit {
				if inThinking {
					os.Stdout.WriteString(thinkingSGRClose + "\n")
				}
				return streamResult{err: context.Canceled}
			}
			// Update bezel to reflect typing during streaming
			r.redraw("streaming...")

		case sev, ok := <-eventCh:
			if !ok {
				// Stream complete
				if inThinking {
					os.Stdout.WriteString(thinkingSGRClose + "\n")
				}
				resp, respErr := asm.Response()
				return streamResult{
					resp:     resp,
					text:     text.String(),
					thinking: thinking.String(),
					err:      respErr,
				}
			}
			asm.Process(sev)

			if sev.Type == api.StreamEventContentBlockDelta && sev.Delta != nil {
				switch sev.Delta.Type {
				case "thinking_delta":
					if !inThinking {
						os.Stdout.WriteString(thinkingSGROpen)
						inThinking = true
					}
					thinking.WriteString(sev.Delta.Thinking)
					os.Stdout.WriteString(sev.Delta.Thinking)
				case "text_delta":
					if inThinking {
						os.Stdout.WriteString(thinkingSGRClose + "\n")
						inThinking = false
					}
					text.WriteString(sev.Delta.Text)
					os.Stdout.WriteString(sev.Delta.Text)
				}
			}

		case err := <-errCh:
			if inThinking {
				os.Stdout.WriteString(thinkingSGRClose + "\n")
			}
			return streamResult{err: err}

		case <-ctx.Done():
			if inThinking {
				os.Stdout.WriteString(thinkingSGRClose + "\n")
			}
			return streamResult{err: ctx.Err()}
		}
	}
}

// send processes a user message through the agentic pipeline.
// When the response contains tool_use blocks, executes tools and loops.
func (r *REPL) send(ctx context.Context, message string) error {
	// Add user message to L1
	r.messages = append(r.messages, api.MessageParam{
		Role:    api.RoleUser,
		Content: message,
	})

	// L1 pruning: drop low-relevance messages when near token budget
	r.pruneL1()

	// [Unconscious] Pre-prompt retrieval — query L2 (+ L4 if available) for relevant prior context
	var retrievedContext string
	if r.retriever != nil {
		r.redraw("retrieving context...")
		results, err := r.retriever.Retrieve(ctx, message)
		if err != nil {
			r.log.Warn("retrieval failed", "error", err)
		} else if len(results) > 0 {
			totalTokens := 0
			for _, res := range results {
				totalTokens += res.Memory.TokenCount
			}
			if r.synthesizer != nil && totalTokens > 4000 {
				r.redraw("synthesizing context...")
				synthesized, synthErr := r.synthesizer.Synthesize(ctx, message, results)
				if synthErr != nil {
					r.log.Warn("synthesis failed, falling back to direct injection", "error", synthErr)
					retrievedContext = tier.FormatDirectInjection(results)
				} else {
					retrievedContext = synthesized
				}
			} else {
				retrievedContext = tier.FormatDirectInjection(results)
			}
			r.log.Info("retrieved prior context", "results", len(results), "total_tokens", totalTokens)
		}
	}

	// Build base request with extended thinking enabled
	req := &api.Request{
		Model:     r.config.Model,
		MaxTokens: r.config.MaxTokens,
		Thinking: &api.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 10000,
		},
	}

	// Build system prompt as blocks: static (cached) + dynamic (uncached).
	// The static block has a 1h cache breakpoint (BP2). Dynamic retrieved
	// context sits after the breakpoint and does not bust the cache.
	if r.config.SystemPrompt != "" {
		if retrievedContext != "" {
			sys := api.NewSystemBlocks(
				r.staticSystemBlock,               // BP2: cached with 1h TTL
				api.NewTextBlock(retrievedContext), // dynamic, after breakpoint
			)
			req.System = &sys
		} else {
			sys := api.NewSystemBlocks(r.staticSystemBlock)
			req.System = &sys
		}
	}

	// Add tools with cache breakpoint on last tool (BP1).
	// Tools come before system in cache prefix order, so this is always stable.
	if r.registry != nil {
		req.Tools = r.registry.APIToolsWithCache(api.WithLongCache())
	}

	// Agentic loop: stream → check for tool_use → execute → send results → repeat
	// Collect turn context for L2 persistence
	// NOTE: r.messages must only be mutated from this goroutine (the REPL event loop).
	// Background goroutines (persister, evictor) receive copies of data via turnContext.
	var turnThinking strings.Builder // accumulated thinking across iterations
	var turnTools []toolRecord       // tool calls + results
	var finalResp *api.Response
	messageSnapshot := len(r.messages) // snapshot for rollback on error
	for iteration := 0; iteration < maxToolIterations; iteration++ {
		req.Messages = r.messages

		r.redraw("streaming...")

		// Stream tokens to stdout as they arrive, multiplexed with bezel events.
		result := r.streamWithBezel(ctx, req)
		if result.err != nil {
			r.messages = r.messages[:messageSnapshot]
			return fmt.Errorf("API call failed (iteration %d): %w", iteration, result.err)
		}
		resp := result.resp

		// Accumulate thinking for persistence
		if result.thinking != "" {
			turnThinking.WriteString(result.thinking)
		}

		// Newline after streamed output (cursor is at end of last token)
		if result.text != "" || result.thinking != "" {
			fmt.Println()
		}

		// Blank line separates response from tool output
		if resp.HasToolUse() && (result.text != "" || result.thinking != "") {
			fmt.Println()
		}

		if !resp.HasToolUse() || r.executor == nil {
			finalResp = resp

			// Add assistant text to L1
			r.messages = append(r.messages, api.MessageParam{
				Role:    api.RoleAssistant,
				Content: result.text,
			})
			break
		}

		// Has tool_use blocks — execute tools and continue loop
		// Build assistant message with both text and tool_use blocks
		r.messages = append(r.messages, assistantFromResponse(resp))

		// Execute each tool and collect results — single-line display per tool
		var resultBlocks []any
		for _, block := range resp.GetToolUses() {
			var input map[string]any
			if unmarshalErr := json.Unmarshal(block.Input, &input); unmarshalErr != nil {
				resultBlocks = append(resultBlocks, api.NewToolResultBlockError(block.ID,
					fmt.Sprintf("failed to parse tool input: %s", unmarshalErr)))
				continue
			}

			toolStart := time.Now()
			result := r.executor.Execute(ctx, block.Name, input)
			elapsed := time.Since(toolStart)

			rec := toolRecord{Name: block.Name, Input: input}

			if result.IsError {
				display := formatToolBlock(block.Name, input, "", result.Error, elapsed, true)
				icon := lip.NewStyle().Foreground(lip.Color("#EF4444")).Render("  ●")
				fmt.Println(icon + " " + r.errorStyle.Render(display))
				resultBlocks = append(resultBlocks, api.NewToolResultBlockError(block.ID, result.Error))
				rec.Error = result.Error
			} else {
				preview := prettyOutput(result.Output, 200)
				display := formatToolBlock(block.Name, input, preview, "", elapsed, false)
				icon := lip.NewStyle().Foreground(lip.Color("#22C55E")).Render("  ●")
				fmt.Println(icon + " " + r.toolStyle.Render(display))
				resultBlocks = append(resultBlocks, api.NewToolResultBlock(block.ID, result.Output))
				rec.Output = result.Output
			}
			turnTools = append(turnTools, rec)
		}

		// Add tool results as user message
		r.messages = append(r.messages, api.MessageParam{
			Role:    api.RoleUser,
			Content: resultBlocks,
		})

		// Blank line separates tool output from next iteration's thinking
		fmt.Println()

		// Status for next iteration
		r.redraw("thinking...")
		finalResp = resp
	}

	// Guard: if we exhausted max iterations, the model is stuck in a tool loop
	if finalResp != nil && finalResp.HasToolUse() {
		fmt.Println(r.errorStyle.Render(fmt.Sprintf(
			"  warning: tool loop reached %d iterations, forcing stop", maxToolIterations)))
		// Still persist what we have — don't discard the partial work
	}

	// Build turn context
	tc := turnContext{
		UserMessage: message,
		Thinking:    turnThinking.String(),
		Tools:       turnTools,
		Response:    "",
		Usage:       api.Usage{},
	}
	if finalResp != nil {
		tc.Response = finalResp.GetText()
		tc.Usage = finalResp.Usage
	}

	// [Unconscious] Persistence — runs in background on engine context.
	// Persister runs first, then evictor, so the evictor sees up-to-date relevance scores.
	if r.l2 != nil {
		// Always do raw L2 persistence (immediate, synchronous)
		r.persistTurn(r.engineCtx, tc)

		// Background: smart analysis then eviction (sequenced to avoid logical races)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()

			// Per-turn timeout prevents goroutine accumulation when APIs are slow
			bgCtx, bgCancel := context.WithTimeout(r.engineCtx, 2*time.Minute)
			defer bgCancel()

			// Step 1: Background persister updates relevance scores
			if r.persister != nil {
				r.runBackgroundPersister(bgCtx, tc)
			}

			// Step 2: Evictor runs after persister so it sees updated scores
			if r.evictor != nil {
				evicted, err := r.evictor.CheckAndEvict(bgCtx, r.config.ConversationID)
				if err != nil {
					r.log.Error("L2→L4 eviction failed", "error", err)
				} else if evicted > 0 {
					r.log.Info("L2→L4 eviction complete", "evicted", evicted)
				}
			}
		}()
	}

	// Log usage with cache metrics
	if finalResp != nil {
		logFields := []any{
			"input_tokens", finalResp.Usage.InputTokens,
			"output_tokens", finalResp.Usage.OutputTokens,
			"stop_reason", finalResp.StopReason,
			"l1_messages", len(r.messages),
		}
		if finalResp.Usage.CacheCreationInputTokens != nil {
			logFields = append(logFields, "cache_create", *finalResp.Usage.CacheCreationInputTokens)
		}
		if finalResp.Usage.CacheReadInputTokens != nil {
			logFields = append(logFields, "cache_read", *finalResp.Usage.CacheReadInputTokens)
		}
		r.log.Info("turn complete", logFields...)
	}

	return nil
}

// assistantFromResponse converts an API response to a MessageParam preserving
// both text and tool_use blocks for the agentic loop.
func assistantFromResponse(resp *api.Response) api.MessageParam {
	var blocks []any
	for _, block := range resp.Content {
		switch {
		case block.IsText():
			blocks = append(blocks, api.TextBlockParam{
				Type: api.ContentBlockTypeText,
				Text: block.Text,
			})
		case block.IsToolUse():
			// Preserve tool_use blocks for multi-turn tool loops
			input := make(map[string]any)
			if block.Input != nil {
				// Best-effort parse — empty input map is acceptable fallback
				json.Unmarshal(block.Input, &input)
			}
			blocks = append(blocks, api.ToolUseBlockParam{
				Type:  api.ContentBlockTypeToolUse,
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		case block.IsThinking():
			blocks = append(blocks, api.ThinkingBlock{
				Type:      string(api.ContentBlockTypeThinking),
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case block.Type == string(api.ContentBlockTypeRedactedThink):
			blocks = append(blocks, api.RedactedThinkingBlock{
				Type: string(api.ContentBlockTypeRedactedThink),
				Data: block.Data,
			})
		}
	}

	return api.MessageParam{
		Role:    api.RoleAssistant,
		Content: blocks,
	}
}
