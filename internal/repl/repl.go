// Package repl provides a styled readline REPL for the Precon conversation loop.
//
// Not a Bubbletea TUI. A standard REPL like Claude Code — readline input, streaming
// output, styled with lipgloss and glamour for markdown rendering.
package repl

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/google/uuid"
	"github.com/hegner123/bezel"

	"github.com/hegner123/precon/internal/api"
	"github.com/hegner123/precon/internal/persister"
	"github.com/hegner123/precon/internal/retriever"
	"github.com/hegner123/precon/internal/storage"
	"github.com/hegner123/precon/internal/synthesizer"
	"github.com/hegner123/precon/internal/tools"
)

// Config holds REPL configuration.
type Config struct {
	Model            string
	MaxTokens        int
	SystemPrompt     string
	ConversationID   string // Persistent conversation ID for L2 storage
	MaxContextTokens int    // L1 token budget (default 200000)
	ResponseReserve  int    // Tokens reserved for response (default 8192)
}

// REPL is the interactive conversation loop.
type REPL struct {
	log         *slog.Logger
	client      *api.Client
	config      Config
	messages    []api.MessageParam       // L1 active context
	l2          *storage.SQLiteStore     // L2 hot storage (nil if not configured)
	retriever   *retriever.Retriever     // Pre-prompt retrieval (nil if not configured)
	persister   *persister.Persister     // Background persister (nil if not configured)
	executor    *tools.Executor          // Tool executor (nil if tools disabled)
	registry    *tools.Registry          // Tool registry (nil if tools disabled)
	synthesizer *synthesizer.Synthesizer // Phase 6: context synthesis (nil if not configured)
	evictor     *storage.Evictor         // Phase 6: L2→L4 eviction (nil if not configured)
	l3          *storage.L3Store         // L3 warm storage (nil if not configured)
	l4          *storage.PgvectorStore   // L4 semantic storage (nil if not configured)
	renderer    *glamour.TermRenderer

	// Cached system prompt block -- built once, reused every turn.
	// Contains the static base system prompt with long TTL cache control.
	staticSystemBlock api.TextBlockParam

	// Bezel — terminal chrome with scroll region
	bz   *bezel.Bezel
	ed   bezel.LineEditor
	hist bezel.History
	km   bezel.KeyMap

	// Engine context for background goroutines (persister).
	// Outlives individual request contexts so persistence survives cancellation.
	engineCtx    context.Context
	engineCancel context.CancelFunc
	wg           sync.WaitGroup

	// Styles
	promptStyle   lip.Style
	statusStyle   lip.Style
	errorStyle    lip.Style
	toolStyle     lip.Style
	thinkingStyle lip.Style
}

// New creates a new REPL.
func New(log *slog.Logger, client *api.Client, cfg Config) (*REPL, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return nil, fmt.Errorf("create markdown renderer: %w", err)
	}

	if cfg.ConversationID == "" {
		cfg.ConversationID = uuid.New().String()
	}
	if cfg.MaxContextTokens == 0 {
		cfg.MaxContextTokens = 200000
	}
	if cfg.ResponseReserve == 0 {
		cfg.ResponseReserve = 8192
	}

	engineCtx, engineCancel := context.WithCancel(context.Background())

	r := &REPL{
		log:           log,
		client:        client,
		config:        cfg,
		messages:      nil,
		renderer:      renderer,
		km:            bezel.DefaultKeyMap(),
		engineCtx:     engineCtx,
		engineCancel:  engineCancel,
		promptStyle:   lip.NewStyle().Foreground(lip.Color("#7C3AED")).Bold(true),
		statusStyle:   lip.NewStyle().Foreground(lip.Color("#9CA3AF")),
		errorStyle:    lip.NewStyle().Foreground(lip.Color("#EF4444")).Bold(true),
		toolStyle:     lip.NewStyle().Foreground(lip.Color("#F59E0B")),
		thinkingStyle: lip.NewStyle().Foreground(lip.Color("#C4B5FD")).Italic(true),
	}

	// Pre-build the static system prompt block with 1-hour cache.
	// This block never changes between turns, forming a stable cache prefix.
	if cfg.SystemPrompt != "" {
		r.staticSystemBlock = api.NewTextBlockWithCache(cfg.SystemPrompt, api.WithLongCache())
	}

	return r, nil
}

// SetL2 attaches L2 hot storage for message persistence.
func (r *REPL) SetL2(store *storage.SQLiteStore) {
	r.l2 = store
}

// SetRetriever enables pre-prompt context retrieval (unconscious pipeline).
func (r *REPL) SetRetriever(ret *retriever.Retriever) {
	r.retriever = ret
}

// SetPersister enables the background Haiku-based persistence agent.
func (r *REPL) SetPersister(p *persister.Persister) {
	r.persister = p
}

// SetTools enables agentic tool use.
func (r *REPL) SetTools(registry *tools.Registry, executor *tools.Executor) {
	r.registry = registry
	r.executor = executor
}

// SetSynthesizer enables the Phase 6 context synthesis agent.
// When retrieval results exceed the synthesis budget, the synthesizer
// compresses them via Haiku before injection into the system prompt.
func (r *REPL) SetSynthesizer(s *synthesizer.Synthesizer) {
	r.synthesizer = s
}

// SetEvictor enables the L2→L4 eviction pipeline.
func (r *REPL) SetEvictor(e *storage.Evictor) {
	r.evictor = e
}

// SetL3 attaches L3 warm storage for tier stats and recall.
func (r *REPL) SetL3(store *storage.L3Store) {
	r.l3 = store
}

// SetL4 attaches L4 semantic storage for tier stats and recall.
func (r *REPL) SetL4(store *storage.PgvectorStore) {
	r.l4 = store
}

// Run starts the REPL loop. Blocks until the user exits.
func (r *REPL) Run(ctx context.Context) error {
	defer r.Close()

	// Push terminal content up to make room for the bezel. Without this,
	// launching near the bottom of the screen leaves the cursor inside the
	// bezel area and all fmt.Println output is hidden.
	const bezelRows = 7
	for range bezelRows {
		fmt.Print("\n")
	}

	bz, err := bezel.New(os.Stdin, os.Stdout, bezelRows)
	if err != nil {
		return fmt.Errorf("create bezel: %w", err)
	}
	r.bz = bz
	defer r.bz.Close()

	// Park cursor at the bottom of the scroll region. bezel.New() restores
	// the cursor to its pre-init position, which may be inside the bezel
	// area if the terminal had few rows above the cursor at launch.
	size := r.bz.Size()
	scrollEnd := int(size.Rows) - bezelRows
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	fmt.Fprintf(os.Stdout, "\033[%d;1H", scrollEnd)

	// Print header in the scroll region
	fmt.Println(r.promptStyle.Render("  precon") + r.statusStyle.Render(" — pre-conscious context management"))
	fmt.Println(r.statusStyle.Render(fmt.Sprintf("  model: %s · L1: %d messages", r.config.Model, len(r.messages))))
	fmt.Println()

	r.redraw("ready")

	for ev := range r.bz.Events() {
		if ev.Type == bezel.EventResize {
			r.redraw("ready")
			continue
		}

		action, text := r.ed.HandleEvent(ev, r.km, &r.hist)
		switch action {
		case bezel.ActionQuit:
			return nil
		case bezel.ActionSubmit:
			input := strings.TrimSpace(text)
			if input == "" {
				r.redraw("ready")
				continue
			}
			r.hist.Add(input)

			// Print user message to scroll region
			fmt.Println(r.promptStyle.Render("precon > ") + input)

			// Handle commands
			if strings.HasPrefix(input, "/") || strings.HasPrefix(input, ":") {
				handled, quit := r.handleCommand(input)
				if quit {
					return nil
				}
				if handled {
					r.redraw("ready")
					continue
				}
			}

			// Process message — output goes to scroll region.
			// streamWithBezel() inside send() handles bezel events during
			// streaming (Ctrl-C, resize, typing). No drain goroutine needed.
			sendCtx, sendCancel := context.WithCancel(ctx)
			r.redraw("thinking...")
			sendErr := r.send(sendCtx, input)
			sendCancel()

			if sendErr != nil {
				if sendCtx.Err() != nil {
					fmt.Println(r.statusStyle.Render("  cancelled"))
				} else {
					fmt.Println(r.errorStyle.Render("  error: " + sendErr.Error()))
				}
			}
			fmt.Println()
			r.redraw("ready")
			continue
		case bezel.ActionNone:
			continue
		}
		r.redraw("editing")
	}

	return nil
}

// redraw updates the bezel chrome with the current editor state.
func (r *REPL) redraw(status string) {
	l1Count := len(r.messages)
	l1Tokens := r.estimateL1Tokens()

	size := r.bz.Size()
	border := strings.Repeat("─", int(size.Cols))

	statusLine := r.statusStyle.Render(fmt.Sprintf(
		"  %s · L1: %d messages · ~%dk tokens · %s",
		r.config.Model, l1Count, l1Tokens/1000, status))

	prompt := "precon > " + r.ed.StringWithCursor()
	hints := r.statusStyle.Render("  enter send · alt-enter newline · ctrl-c quit · /help")

	r.bz.Redraw(
		"",                           // row 0: blank
		statusLine,                   // row 1: status
		"",                           // row 2: blank
		r.statusStyle.Render(border), // row 3: top border
		prompt,                       // row 4: prompt (cursor here)
		r.statusStyle.Render(border), // row 5: bottom border
		hints,                        // row 6: hints
	)
}

// Close cancels the engine context, waits for background goroutines, and cleans up resources.
func (r *REPL) Close() {
	r.engineCancel()
	r.wg.Wait()
	tools.GetAgentLogger().Close()
}
