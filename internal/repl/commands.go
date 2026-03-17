package repl

import (
	"fmt"
	"strings"

	lip "charm.land/lipgloss/v2"

	"github.com/hegner123/precon/internal/tier"
)

// handleCommand processes slash commands.
// Returns (handled, quit). When quit is true, the Run loop should exit cleanly.
func (r *REPL) handleCommand(input string) (handled bool, quit bool) {
	r.bz.CursorToScroll()

	// Extract command and args
	parts := strings.SplitN(input, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch {
	case cmd == "/exit" || cmd == "/quit" || cmd == ":q" || cmd == ":q!" || cmd == ":wq":
		fmt.Println("goodbye")
		return true, true
	case cmd == "/clear":
		r.messages = nil
		fmt.Println(r.statusStyle.Render("context cleared"))
	case cmd == "/tokens":
		r.printTokenEstimate()
	case cmd == "/recall":
		r.cmdRecall(args)
	case cmd == "/tiers":
		r.cmdTiers()
	case cmd == "/topics":
		r.cmdTopics()
	case cmd == "/help":
		r.printHelp()
	default:
		fmt.Println(r.statusStyle.Render("unknown command: " + input))
	}
	return true, false
}

// printTokenEstimate prints an estimate of current L1 token usage.
func (r *REPL) printTokenEstimate() {
	estimated := r.estimateL1Tokens()
	budget := r.config.MaxContextTokens - r.config.ResponseReserve
	pct := float64(estimated) / float64(budget) * 100
	fmt.Println(r.statusStyle.Render(fmt.Sprintf(
		"L1: ~%d tokens (%.1f%% of %d budget), %d messages",
		estimated, pct, budget, len(r.messages),
	)))
}

// printHelp prints available commands.
func (r *REPL) printHelp() {
	help := `Commands:
  /recall <query>  Search memory across all tiers
  /tiers           Show memory tier statistics
  /topics          Show topics from L2
  /tokens          Show L1 token usage estimate
  /clear           Clear conversation context
  /help            Show this help
  /exit            Exit precon`
	fmt.Println(help)
}

// cmdRecall searches across all tiers for memories matching a query.
func (r *REPL) cmdRecall(query string) {
	if query == "" {
		fmt.Println(r.errorStyle.Render("  usage: /recall <query>"))
		return
	}

	ctx := r.engineCtx
	fmt.Println(r.statusStyle.Render(fmt.Sprintf("  searching for: %s", query)))
	fmt.Println()

	printed := 0

	// Search L2
	if r.l2 != nil {
		results, err := r.l2.Query(ctx, query, 5)
		if err != nil {
			r.log.Warn("L2 recall failed", "error", err)
		}
		for _, res := range results {
			printRecallResult(res, r.toolStyle, r.statusStyle)
			printed++
		}
	}

	// Search L3
	if r.l3 != nil {
		results, err := r.l3.Query(ctx, query, 5)
		if err != nil {
			r.log.Warn("L3 recall failed", "error", err)
		}
		for _, res := range results {
			printRecallResult(res, r.toolStyle, r.statusStyle)
			printed++
		}
	}

	// Search L4
	if r.l4 != nil {
		results, err := r.l4.Query(ctx, query, 5)
		if err != nil {
			r.log.Warn("L4 recall failed", "error", err)
		}
		for _, res := range results {
			printRecallResult(res, r.toolStyle, r.statusStyle)
			printed++
		}
	}

	if printed == 0 {
		fmt.Println(r.statusStyle.Render("  no results found"))
	} else {
		fmt.Println(r.statusStyle.Render(fmt.Sprintf("  %d results", printed)))
	}
}

// printRecallResult formats a single recall result.
func printRecallResult(res tier.RetrievalResult, toolStyle, statusStyle lip.Style) {
	content := res.Memory.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	// Replace newlines with spaces for compact display
	content = strings.ReplaceAll(content, "\n", " ")

	keywords := ""
	if len(res.Memory.Keywords) > 0 {
		keywords = " [" + strings.Join(res.Memory.Keywords, ", ") + "]"
	}

	fmt.Printf("  %s  ", toolStyle.Render(fmt.Sprintf("%-8s %.2f", res.SourceTier, res.Score)))
	fmt.Printf("%s%s\n", statusStyle.Render(keywords), "")
	fmt.Printf("    %s\n\n", content)
}

// cmdTiers shows memory tier statistics.
func (r *REPL) cmdTiers() {
	ctx := r.engineCtx
	fmt.Println()

	// L1
	l1Count := len(r.messages)
	l1Tokens := r.estimateL1Tokens()
	fmt.Printf("  %s  %d messages, ~%dk tokens (active context)\n",
		r.promptStyle.Render("L1 Active "),
		l1Count, l1Tokens/1000)

	// L2
	if r.l2 != nil {
		memories, err := r.l2.List(ctx, "")
		if err != nil {
			fmt.Printf("  %s  error: %v\n", r.errorStyle.Render("L2 Hot    "), err)
		} else {
			totalTokens := 0
			for _, m := range memories {
				totalTokens += m.TokenCount
			}
			fmt.Printf("  %s  %d memories, ~%dk tokens (SQLite + FTS5)\n",
				r.toolStyle.Render("L2 Hot    "),
				len(memories), totalTokens/1000)
		}
	}

	// L3
	if r.l3 != nil {
		summaries, err := r.l3.List(ctx, "")
		if err != nil {
			fmt.Printf("  %s  error: %v\n", r.errorStyle.Render("L3 Warm   "), err)
		} else {
			fmt.Printf("  %s  %d summaries (pointers to L4)\n",
				r.statusStyle.Render("L3 Warm   "),
				len(summaries))
		}
	}

	// L4
	if r.l4 != nil {
		total, err := r.l4.TotalTokens(ctx)
		if err != nil {
			fmt.Printf("  %s  error: %v\n", r.errorStyle.Render("L4 Semantic"), err)
		} else {
			memories, listErr := r.l4.List(ctx, "")
			count := 0
			if listErr == nil {
				count = len(memories)
			}
			fmt.Printf("  %s  %d memories, ~%dk tokens (pgvector)\n",
				r.thinkingStyle.Render("L4 Semantic"),
				count, total/1000)
		}
	}

	fmt.Println()
}

// cmdTopics shows topics from L2.
func (r *REPL) cmdTopics() {
	if r.l2 == nil {
		fmt.Println(r.statusStyle.Render("  L2 not configured"))
		return
	}

	ctx := r.engineCtx
	topics, err := r.l2.ListTopics(ctx, r.config.ConversationID)
	if err != nil {
		fmt.Println(r.errorStyle.Render(fmt.Sprintf("  error: %v", err)))
		return
	}

	if len(topics) == 0 {
		fmt.Println(r.statusStyle.Render("  no topics"))
		return
	}

	fmt.Println()
	for _, t := range topics {
		current := ""
		if t.IsCurrent {
			current = r.promptStyle.Render(" (current)")
		}
		keywords := ""
		if len(t.Keywords) > 0 {
			keywords = r.statusStyle.Render(" [" + strings.Join(t.Keywords, ", ") + "]")
		}
		fmt.Printf("  %s  %.2f  %s%s%s\n",
			t.CreatedAt.Format("Jan 02 15:04"),
			t.Relevance,
			t.Name,
			keywords,
			current)
	}
	fmt.Println()
}
