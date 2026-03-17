package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/tier"
)

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

	// Build system prompt: base prompt + injected retrieval context
	systemPrompt := r.config.SystemPrompt
	if retrievedContext != "" {
		systemPrompt += "\n\n" + retrievedContext
	}
	if systemPrompt != "" {
		sys := api.NewSystemString(systemPrompt)
		req.System = &sys
	}

	// Add tools to request if enabled
	if r.registry != nil {
		req.Tools = r.registry.APITools()
	}

	// Agentic loop: stream → check for tool_use → execute → send results → repeat
	// Collect turn context for L2 persistence
	var turnThinking strings.Builder // accumulated thinking across iterations
	var turnTools []toolRecord       // tool calls + results
	var finalResp *api.Response
	for iteration := 0; ; iteration++ {
		req.Messages = r.messages

		r.redraw("thinking...")
		r.bz.CursorToScroll()

		// Stream response with thinking and text
		var responseText strings.Builder
		inThinking := false
		resp, err := r.client.StreamWithCallback(ctx, req, func(event *api.StreamEvent) error {
			switch event.Type {
			case api.StreamEventContentBlockStart:
				if event.ContentBlock != nil && event.ContentBlock.IsThinking() {
					inThinking = true
				}
			case api.StreamEventContentBlockDelta:
				if event.Delta == nil {
					return nil
				}
				if event.Delta.Thinking != "" {
					fmt.Print(r.thinkingStyle.Render(event.Delta.Thinking))
					turnThinking.WriteString(event.Delta.Thinking)
				}
				if event.Delta.Text != "" {
					if inThinking {
						fmt.Println()
						inThinking = false
					}
					responseText.WriteString(event.Delta.Text)
					fmt.Print(event.Delta.Text)
				}
			case api.StreamEventContentBlockStop:
				if inThinking {
					fmt.Println()
					inThinking = false
				}
			}
			return nil
		})

		if err != nil {
			if iteration == 0 && len(r.messages) > 0 {
				r.messages = r.messages[:len(r.messages)-1]
			}
			return fmt.Errorf("API call failed (iteration %d): %w", iteration, err)
		}

		if responseText.Len() > 0 {
			fmt.Println()
		}

		// Check for tool use
		if !resp.HasToolUse() || r.executor == nil {
			finalResp = resp

			// Add assistant text to L1
			r.messages = append(r.messages, api.MessageParam{
				Role:    api.RoleAssistant,
				Content: responseText.String(),
			})
			break
		}

		// Has tool_use blocks — execute tools and continue loop
		// Build assistant message with both text and tool_use blocks
		r.messages = append(r.messages, assistantFromResponse(resp))

		// Execute each tool and collect results
		var resultBlocks []any
		for _, block := range resp.GetToolUses() {
			var input map[string]any
			if unmarshalErr := json.Unmarshal(block.Input, &input); unmarshalErr != nil {
				resultBlocks = append(resultBlocks, api.NewToolResultBlockError(block.ID,
					fmt.Sprintf("failed to parse tool input: %s", unmarshalErr)))
				continue
			}

			// Show tool name and prettified input
			inputPretty, marshalErr := json.MarshalIndent(input, "    ", "  ")
			if marshalErr != nil {
				inputPretty = []byte(fmt.Sprintf("%v", input))
			}
			fmt.Println(r.toolStyle.Render(fmt.Sprintf("  tool: %s", block.Name)))
			fmt.Println(r.toolStyle.Render(fmt.Sprintf("    input: %s", string(inputPretty))))

			result := r.executor.Execute(ctx, block.Name, input)
			rec := toolRecord{Name: block.Name, Input: input}
			if result.IsError {
				fmt.Println(r.errorStyle.Render(fmt.Sprintf("    error: %s", truncate(result.Error, 200))))
				resultBlocks = append(resultBlocks, api.NewToolResultBlockError(block.ID, result.Error))
				rec.Error = result.Error
			} else {
				output := result.Output
				preview := prettyOutput(output, 500)
				if preview != "" {
					fmt.Println(r.toolStyle.Render(fmt.Sprintf("    output: %s", preview)))
				}
				resultBlocks = append(resultBlocks, api.NewToolResultBlock(block.ID, output))
				rec.Output = output
			}
			turnTools = append(turnTools, rec)
		}

		// Add tool results as user message
		r.messages = append(r.messages, api.MessageParam{
			Role:    api.RoleUser,
			Content: resultBlocks,
		})

		// Status for next iteration
		r.redraw("thinking...")
		r.bz.CursorToScroll()
		finalResp = resp
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

	// [Unconscious] Persistence — runs in background on engine context
	if r.l2 != nil {
		// Always do raw L2 persistence (immediate, synchronous)
		r.persistTurn(r.engineCtx, tc)

		// If the background persister is configured, also run smart analysis
		if r.persister != nil {
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.runBackgroundPersister(tc)
			}()
		}
	}
	// Phase 6: Check L2→L4 eviction after persistence
	if r.evictor != nil {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			evicted, err := r.evictor.CheckAndEvict(r.engineCtx, r.config.ConversationID)
			if err != nil {
				r.log.Error("L2→L4 eviction failed", "error", err)
			} else if evicted > 0 {
				r.log.Info("L2→L4 eviction complete", "evicted", evicted)
			}
		}()
	}

	// Log usage
	if finalResp != nil {
		r.log.Info("turn complete",
			"input_tokens", finalResp.Usage.InputTokens,
			"output_tokens", finalResp.Usage.OutputTokens,
			"stop_reason", finalResp.StopReason,
			"l1_messages", len(r.messages),
		)
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
