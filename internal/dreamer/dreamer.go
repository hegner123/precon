// Package dreamer implements the background dreaming process.
//
// Dreaming is a periodic (cron-scheduled or daemon-mode) process that:
// - Trims recent topics and organizes them into persistence layers
// - Runs user-configurable analysis prompts across sessions
// - Generates reports on patterns, pain points, and workflow trends
// - Reports become new entries in the memory hierarchy (meta-knowledge)
//
// Dreaming turns the memory multiplex from passive storage into an active
// knowledge base. It's the difference between a filesystem and a database
// with materialized views.
package dreamer

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/hegner123/precon/internal/storage"
	"github.com/hegner123/precon/internal/tier"
)

// maxChunkTokens is the maximum token budget per chunk when splitting
// large memory sets to stay within Haiku's context window.
const maxChunkTokens = 80000

// LLM abstracts the language model used for dream analysis.
type LLM interface {
	// Complete sends a prompt and returns the response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// AnalysisPrompt is a user-configurable prompt for dream analysis.
type AnalysisPrompt struct {
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Lookback string `json:"lookback"` // "1d", "7d", "30d"
}

// Report is the output of a dream analysis.
type Report struct {
	AnalysisName string    `json:"analysis_name"`
	Content      string    `json:"content"`
	GeneratedAt  time.Time `json:"generated_at"`
	LookbackDays int       `json:"lookback_days"`
}

// ProjectReport holds reports for a single project.
type ProjectReport struct {
	ProjectDir string   `json:"project_dir"`
	Reports    []Report `json:"reports"`
}

// Dreamer runs periodic analysis over the memory multiplex.
type Dreamer struct {
	log         *slog.Logger
	llm         LLM
	l2          tier.Store
	l4          tier.Store
	prompts     []AnalysisPrompt
	reportStore tier.Store
}

// New creates a Dreamer with the given stores and analysis prompts.
func New(log *slog.Logger, llm LLM, l2, l4 tier.Store, prompts []AnalysisPrompt) *Dreamer {
	return &Dreamer{
		log:     log,
		llm:     llm,
		l2:      l2,
		l4:      l4,
		prompts: prompts,
	}
}

// SetReportStore configures an optional L2 store for persisting generated reports.
func (d *Dreamer) SetReportStore(store tier.Store) {
	d.reportStore = store
}

// Dream runs all configured analysis prompts and returns reports.
// If a report store is set, each generated report is also persisted as an L2 memory.
func (d *Dreamer) Dream(ctx context.Context) ([]Report, error) {
	var reports []Report

	for _, ap := range d.prompts {
		report, err := d.runAnalysis(ctx, ap)
		if err != nil {
			d.log.Warn("dream analysis failed", "name", ap.Name, "error", err)
			continue
		}
		reports = append(reports, *report)

		if d.reportStore != nil {
			if storeErr := d.storeReport(ctx, ap, report); storeErr != nil {
				d.log.Warn("failed to store dream report", "name", ap.Name, "error", storeErr)
			}
		}
	}

	return reports, nil
}

// storeReport persists a single report as an L2 memory in the report store.
func (d *Dreamer) storeReport(ctx context.Context, ap AnalysisPrompt, report *Report) error {
	now := time.Now()
	mem := &tier.Memory{
		ID:             "dream-" + ap.Name + "-" + now.Format("20060102"),
		ConversationID: "", // cross-project
		Content:        report.Content,
		Keywords:       []string{"dream", "report", ap.Name},
		TokenCount:     len(report.Content) / 4,
		Relevance:      0.7,
		Tier:           tier.L2,
		CreatedAt:      now,
		LastAccessedAt: now,
	}
	return d.reportStore.Store(ctx, mem)
}

// DreamAllProjects runs dream analysis across multiple project databases.
// Each path in projectDBPaths should be an absolute path to a project's SQLite DB.
// For each DB it opens a temporary store, queries memories, runs analysis, and closes.
func (d *Dreamer) DreamAllProjects(ctx context.Context, projectDBPaths []string) ([]ProjectReport, error) {
	var allReports []ProjectReport

	for _, dbPath := range projectDBPaths {
		d.log.Info("dreaming over project", "db", dbPath)

		projectReports, err := d.dreamSingleProject(ctx, dbPath)
		if err != nil {
			d.log.Warn("dream failed for project", "db", dbPath, "error", err)
			continue
		}

		if len(projectReports) > 0 {
			allReports = append(allReports, ProjectReport{
				ProjectDir: filepath.Dir(dbPath),
				Reports:    projectReports,
			})
		}
	}

	return allReports, nil
}

// dreamSingleProject opens one project DB, runs all analysis prompts against it,
// and returns the resulting reports. The DB is closed before returning.
func (d *Dreamer) dreamSingleProject(ctx context.Context, dbPath string) ([]Report, error) {
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open project db %s: %w", dbPath, err)
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			d.log.Warn("failed to close project db", "db", dbPath, "error", cerr)
		}
	}()

	var reports []Report
	for _, ap := range d.prompts {
		report, err := d.runAnalysisWithStore(ctx, store, ap)
		if err != nil {
			d.log.Warn("dream analysis failed for project", "db", dbPath, "name", ap.Name, "error", err)
			continue
		}
		reports = append(reports, *report)
	}

	return reports, nil
}

// runAnalysis executes a single analysis prompt against the dreamer's L2 store.
func (d *Dreamer) runAnalysis(ctx context.Context, ap AnalysisPrompt) (*Report, error) {
	return d.runAnalysisWithStore(ctx, d.l2, ap)
}

// runAnalysisWithStore executes a single analysis prompt against a given store.
// If the memories exceed the token budget, they are chunked and analyzed separately,
// then merged into a single report.
func (d *Dreamer) runAnalysisWithStore(ctx context.Context, store tier.Store, ap AnalysisPrompt) (*Report, error) {
	lookbackDays := parseLookback(ap.Lookback)

	memories, err := store.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to query store: %w", err)
	}

	// Filter by lookback window
	cutoff := time.Now().Add(-time.Duration(lookbackDays) * 24 * time.Hour)
	var recent []tier.Memory
	for _, m := range memories {
		if m.CreatedAt.After(cutoff) {
			recent = append(recent, m)
		}
	}

	if len(recent) == 0 {
		return &Report{
			AnalysisName: ap.Name,
			Content:      "No recent activity in the lookback window.",
			GeneratedAt:  time.Now(),
			LookbackDays: lookbackDays,
		}, nil
	}

	// Estimate total tokens
	totalTokens := 0
	for _, m := range recent {
		totalTokens += m.TokenCount
	}

	// If within budget, run a single analysis pass
	if totalTokens <= maxChunkTokens {
		return d.analyzeMemories(ctx, ap, recent, lookbackDays)
	}

	// Chunk memories and run partial analyses
	d.log.Info("chunking memories for analysis",
		"name", ap.Name,
		"total_tokens", totalTokens,
		"memory_count", len(recent),
	)

	chunks := chunkMemories(recent)
	if len(chunks) == 1 {
		return d.analyzeMemories(ctx, ap, chunks[0], lookbackDays)
	}

	partialReports := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		partial, err := d.analyzeMemories(ctx, ap, chunk, lookbackDays)
		if err != nil {
			d.log.Warn("chunk analysis failed",
				"name", ap.Name,
				"chunk", i+1,
				"total_chunks", len(chunks),
				"error", err,
			)
			continue
		}
		partialReports = append(partialReports, partial.Content)
	}

	if len(partialReports) == 0 {
		return nil, fmt.Errorf("all chunk analyses failed for %s", ap.Name)
	}

	// Merge partial reports into a single coherent report
	merged, err := d.mergeReports(ctx, partialReports)
	if err != nil {
		return nil, fmt.Errorf("merge reports for %s: %w", ap.Name, err)
	}

	return &Report{
		AnalysisName: ap.Name,
		Content:      merged,
		GeneratedAt:  time.Now(),
		LookbackDays: lookbackDays,
	}, nil
}

// analyzeMemories sends a set of memories to the LLM for analysis.
func (d *Dreamer) analyzeMemories(ctx context.Context, ap AnalysisPrompt, memories []tier.Memory, lookbackDays int) (*Report, error) {
	input := buildAnalysisInput(ap, memories, lookbackDays)

	systemPrompt := `You are a dream analysis agent. You review accumulated session data and identify patterns, pain points, and workflow trends. Your reports help the user understand their work habits and recurring challenges.

Be specific. Cite concrete examples from the session data. Quantify when possible ("3 of 5 sessions involved debugging X"). Output actionable observations, not vague summaries.`

	content, err := d.llm.Complete(ctx, systemPrompt, input)
	if err != nil {
		return nil, fmt.Errorf("dream analysis failed: %w", err)
	}

	return &Report{
		AnalysisName: ap.Name,
		Content:      content,
		GeneratedAt:  time.Now(),
		LookbackDays: lookbackDays,
	}, nil
}

// buildAnalysisInput constructs the user prompt from memories and analysis config.
func buildAnalysisInput(ap AnalysisPrompt, memories []tier.Memory, lookbackDays int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ANALYSIS: %s\nLOOKBACK: %d days\nRECENT TOPICS: %d\n\n",
		ap.Name, lookbackDays, len(memories))

	for i, m := range memories {
		fmt.Fprintf(&b, "--- Topic %d (%s) ---\nKeywords: %v\n%s\n\n",
			i+1, m.CreatedAt.Format("2006-01-02"), m.Keywords, m.Content)
	}

	b.WriteString("\n")
	b.WriteString(ap.Prompt)
	return b.String()
}

// mergeReports combines multiple partial analysis reports into one via the LLM.
func (d *Dreamer) mergeReports(ctx context.Context, partials []string) (string, error) {
	systemPrompt := "Merge these partial analysis reports into a single coherent report. Preserve specific examples and quantitative claims."

	var b strings.Builder
	fmt.Fprintf(&b, "The following %d partial reports were generated from chunked analysis of a large memory set.\n\n", len(partials))

	for i, p := range partials {
		fmt.Fprintf(&b, "=== Partial Report %d ===\n%s\n\n", i+1, p)
	}

	merged, err := d.llm.Complete(ctx, systemPrompt, b.String())
	if err != nil {
		return "", fmt.Errorf("merge LLM call failed: %w", err)
	}
	return merged, nil
}

// chunkMemories splits memories into groups that fit within maxChunkTokens.
func chunkMemories(memories []tier.Memory) [][]tier.Memory {
	var chunks [][]tier.Memory
	var current []tier.Memory
	var currentTokens int

	for _, m := range memories {
		if currentTokens+m.TokenCount > maxChunkTokens && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, m)
		currentTokens += m.TokenCount
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// parseLookback converts a lookback string ("1d", "7d", "30d") to days.
func parseLookback(s string) int {
	switch s {
	case "1d":
		return 1
	case "7d":
		return 7
	case "30d":
		return 30
	case "90d":
		return 90
	default:
		return 7 // Default to 1 week
	}
}
