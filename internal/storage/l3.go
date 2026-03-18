package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hegner123/precon/internal/tier"
)

// L3Schema is the SQLite schema for L3 warm storage (summaries with L4 pointers).
// It shares the same database file as L2.
const L3Schema = `
CREATE TABLE IF NOT EXISTS summaries (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    l4_ref TEXT NOT NULL,
    summary TEXT NOT NULL,
    keywords TEXT DEFAULT '[]',
    token_count INTEGER DEFAULT 0,
    relevance REAL DEFAULT 1.0,
    created_at TEXT NOT NULL,
    last_accessed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_summaries_conversation ON summaries(conversation_id);

CREATE VIRTUAL TABLE IF NOT EXISTS summaries_fts USING fts5(
    summary_id UNINDEXED,
    summary,
    keywords
);
`

// Summary holds a generated summary with metadata.
type Summary struct {
	ID             string
	ConversationID string
	L4Ref          string // ID of the full content in L4
	Text           string // compressed summary text
	Keywords       []string
	TokenCount     int
}

// L3Store implements tier.Store for L3 warm storage backed by SQLite summaries.
// It holds compressed summaries with pointers to full content in L4 (PgvectorStore).
// Queries hit the local FTS5 index, then dereference through L4 to return full content.
type L3Store struct {
	db  *sql.DB        // same SQLite DB as L2
	l4  *PgvectorStore // reference to L4 for dereferencing
	log *slog.Logger
}

// Compile-time check that L3Store implements tier.Store.
var _ tier.Store = (*L3Store)(nil)

// NewL3Store creates an L3 warm storage layer sharing the same SQLite database as L2.
// It runs the L3 schema migration (idempotent) and holds a reference to L4 for content dereferencing.
func NewL3Store(db *sql.DB, l4 *PgvectorStore, log *slog.Logger) (*L3Store, error) {
	if _, err := db.Exec(L3Schema); err != nil {
		return nil, fmt.Errorf("apply L3 schema: %w", err)
	}

	log.Debug("L3 schema initialized")
	return &L3Store{db: db, l4: l4, log: log}, nil
}

// Store saves a summary memory into L3 storage.
// The memory's IsSummary should be true and FullContentRef should point to the L4 ID.
func (s *L3Store) Store(ctx context.Context, mem *tier.Memory) error {
	keywords, err := json.Marshal(mem.Keywords)
	if err != nil {
		return fmt.Errorf("marshal keywords: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO summaries (
			id, conversation_id, l4_ref, summary, keywords,
			token_count, relevance, created_at, last_accessed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID,
		mem.ConversationID,
		mem.FullContentRef,
		mem.Content,
		string(keywords),
		mem.TokenCount,
		mem.Relevance,
		mem.CreatedAt.Format(time.RFC3339),
		mem.LastAccessedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert summary: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO summaries_fts (summary_id, summary, keywords) VALUES (?, ?, ?)`,
		mem.ID,
		mem.Content,
		string(keywords),
	)
	if err != nil {
		return fmt.Errorf("insert summary fts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit L3 store: %w", err)
	}

	s.log.Debug("L3 stored summary", "id", mem.ID, "l4_ref", mem.FullContentRef, "tokens", mem.TokenCount)
	return nil
}

// Retrieve fetches a single summary by ID from L3.
func (s *L3Store) Retrieve(ctx context.Context, id string) (*tier.Memory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, conversation_id, l4_ref, summary, keywords,
		        token_count, relevance, created_at, last_accessed_at
		 FROM summaries WHERE id = ?`, id)

	mem, err := s.scanSummary(row)
	if err != nil {
		return nil, fmt.Errorf("L3 retrieve %s: %w", id, err)
	}

	// Update last_accessed_at — best-effort, don't fail the read
	if _, err := s.db.ExecContext(ctx,
		`UPDATE summaries SET last_accessed_at = ? WHERE id = ?`,
		time.Now().Format(time.RFC3339), id); err != nil {
		s.log.Warn("failed to update L3 last_accessed_at", "id", id, "error", err)
	}

	return mem, nil
}

// Query searches L3 using FTS5 full-text search on summaries, then dereferences
// each match through L4 to return the full content. The returned RetrievalResults
// contain full L4 content with the FTS5 match score from L3.
func (s *L3Store) Query(ctx context.Context, prompt string, limit int) ([]tier.RetrievalResult, error) {
	terms := extractSearchTerms(prompt)
	if len(terms) == 0 {
		return nil, nil
	}

	matchExpr := strings.Join(terms, " OR ")

	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.conversation_id, s.l4_ref, s.summary, s.keywords,
		        s.token_count, s.relevance, s.created_at, s.last_accessed_at,
		        fts.rank
		 FROM summaries_fts fts
		 JOIN summaries s ON s.id = fts.summary_id
		 WHERE summaries_fts MATCH ?
		 ORDER BY fts.rank
		 LIMIT ?`,
		matchExpr, limit)
	if err != nil {
		return nil, fmt.Errorf("L3 query fts: %w", err)
	}
	defer rows.Close()

	type scored struct {
		mem   tier.Memory
		l4Ref string
		rank  float64
	}

	var matches []scored
	var minRank, maxRank float64
	first := true

	for rows.Next() {
		var (
			m           tier.Memory
			l4Ref       string
			kwJSON      string
			createdStr  string
			accessedStr string
			rank        float64
		)

		err := rows.Scan(
			&m.ID, &m.ConversationID, &l4Ref, &m.Content,
			&kwJSON, &m.TokenCount, &m.Relevance,
			&createdStr, &accessedStr,
			&rank,
		)
		if err != nil {
			return nil, fmt.Errorf("scan L3 query result: %w", err)
		}

		// Keywords stored as JSON by this package — unmarshal failure means DB corruption
		if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
			s.log.Warn("corrupt keywords JSON in L3", "id", m.ID, "error", err)
			m.Keywords = nil
		}

		m.CreatedAt = parseRFC3339(createdStr)
		m.LastAccessedAt = parseRFC3339(accessedStr)
		m.Tier = tier.L3
		m.IsSummary = true
		m.FullContentRef = l4Ref

		if first {
			minRank = rank
			maxRank = rank
			first = false
		} else {
			minRank = min(minRank, rank)
			maxRank = max(maxRank, rank)
		}

		matches = append(matches, scored{mem: m, l4Ref: l4Ref, rank: rank})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate L3 query results: %w", err)
	}

	// Dereference each match through L4 to get full content.
	// The score comes from L3 FTS5, but the content comes from L4.
	results := make([]tier.RetrievalResult, 0, len(matches))
	for _, m := range matches {
		// Normalize rank to 0-1 score. FTS5 rank is negative (more negative = better).
		score := 1.0
		if maxRank != minRank {
			score = (maxRank - m.rank) / (maxRank - minRank)
		}

		// Dereference the L4 reference to get full content
		fullMem, err := s.l4.Retrieve(ctx, m.l4Ref)
		if err != nil {
			s.log.Warn("L3 dereference failed, returning summary only",
				"summary_id", m.mem.ID, "l4_ref", m.l4Ref, "error", err)
			// Fall back to returning the summary itself
			results = append(results, tier.RetrievalResult{
				Memory:     m.mem,
				Score:      score,
				SourceTier: tier.L3,
			})
			continue
		}

		// Return full L4 content with L3 FTS5 score
		results = append(results, tier.RetrievalResult{
			Memory:     *fullMem,
			Score:      score,
			SourceTier: tier.L3,
		})
	}

	s.log.Debug("L3 query complete", "matches", len(results), "terms", len(terms))
	return results, nil
}

// Delete removes a summary from both the summaries table and the FTS index.
func (s *L3Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM summaries_fts WHERE summary_id = ?`, id); err != nil {
		return fmt.Errorf("delete L3 fts entry: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM summaries WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete L3 summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit L3 delete: %w", err)
	}

	s.log.Debug("L3 deleted summary", "id", id)
	return nil
}

// List returns all summaries for a conversation, ordered by creation time descending.
// If conversationID is empty, all L3 summaries are returned.
func (s *L3Store) List(ctx context.Context, conversationID string) ([]tier.Memory, error) {
	var rows *sql.Rows
	var err error

	if conversationID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, conversation_id, l4_ref, summary, keywords,
			        token_count, relevance, created_at, last_accessed_at
			 FROM summaries ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, conversation_id, l4_ref, summary, keywords,
			        token_count, relevance, created_at, last_accessed_at
			 FROM summaries WHERE conversation_id = ? ORDER BY created_at DESC`,
			conversationID)
	}
	if err != nil {
		return nil, fmt.Errorf("L3 list summaries: %w", err)
	}
	defer rows.Close()

	var memories []tier.Memory
	for rows.Next() {
		mem, err := s.scanSummaryFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan L3 list result: %w", err)
		}
		memories = append(memories, *mem)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate L3 list results: %w", err)
	}

	return memories, nil
}

// Level returns tier.L3, identifying this as the warm storage tier.
func (s *L3Store) Level() tier.Level {
	return tier.L3
}

// scanSummary scans a single row from *sql.Row into a Memory with L3 fields.
func (s *L3Store) scanSummary(row *sql.Row) (*tier.Memory, error) {
	var (
		m           tier.Memory
		l4Ref       string
		kwJSON      string
		createdStr  string
		accessedStr string
	)

	err := row.Scan(
		&m.ID, &m.ConversationID, &l4Ref, &m.Content,
		&kwJSON, &m.TokenCount, &m.Relevance,
		&createdStr, &accessedStr,
	)
	if err != nil {
		return nil, err
	}

	m.IsSummary = true
	m.FullContentRef = l4Ref
	m.Tier = tier.L3

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
		s.log.Warn("corrupt keywords JSON in L3", "id", m.ID, "error", err)
		m.Keywords = nil
	}

	m.CreatedAt = parseRFC3339(createdStr)
	m.LastAccessedAt = parseRFC3339(accessedStr)

	return &m, nil
}

// scanSummaryFromRows scans a single row from *sql.Rows into a Memory with L3 fields.
func (s *L3Store) scanSummaryFromRows(rows *sql.Rows) (*tier.Memory, error) {
	var (
		m           tier.Memory
		l4Ref       string
		kwJSON      string
		createdStr  string
		accessedStr string
	)

	err := rows.Scan(
		&m.ID, &m.ConversationID, &l4Ref, &m.Content,
		&kwJSON, &m.TokenCount, &m.Relevance,
		&createdStr, &accessedStr,
	)
	if err != nil {
		return nil, err
	}

	m.IsSummary = true
	m.FullContentRef = l4Ref
	m.Tier = tier.L3

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
		s.log.Warn("corrupt keywords JSON in L3", "id", m.ID, "error", err)
		m.Keywords = nil
	}

	m.CreatedAt = parseRFC3339(createdStr)
	m.LastAccessedAt = parseRFC3339(accessedStr)

	return &m, nil
}
