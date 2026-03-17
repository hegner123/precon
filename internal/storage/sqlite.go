package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hegner123/precon/internal/tier"
	_ "modernc.org/sqlite"
)

// parseRFC3339 parses an RFC3339 timestamp string and returns the resulting time.
// Timestamps are written by this package in RFC3339 — parse failure means DB corruption, not user input.
func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Topic represents a conversation topic stored in L2.
type Topic struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Name           string    `json:"name"`
	Keywords       []string  `json:"keywords"`
	Relevance      float64   `json:"relevance"`
	IsCurrent      bool      `json:"is_current"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SQLiteStore implements tier.Store for L2 hot storage backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// compile-time interface check
var _ tier.Store = (*SQLiteStore)(nil)

// NewSQLiteStore opens a SQLite database at dbPath, runs the schema migration,
// and returns a ready-to-use store. The caller must call Close when done.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := db.Exec(Schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Store saves a memory into L2 storage. It always sets tier to L2.
func (s *SQLiteStore) Store(ctx context.Context, mem *tier.Memory) error {
	keywords, err := json.Marshal(mem.Keywords)
	if err != nil {
		return fmt.Errorf("marshal keywords: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	isSummary := 0
	if mem.IsSummary {
		isSummary = 1
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO memories (
			id, conversation_id, topic_id, content, is_summary, full_content_ref,
			token_count, relevance, tier, keywords, created_at, last_accessed_at, promoted_from
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID,
		mem.ConversationID,
		mem.TopicID,
		mem.Content,
		isSummary,
		mem.FullContentRef,
		mem.TokenCount,
		mem.Relevance,
		int(tier.L2),
		string(keywords),
		mem.CreatedAt.Format(time.RFC3339),
		mem.LastAccessedAt.Format(time.RFC3339),
		int(mem.PromotedFrom),
	)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO memories_fts (memory_id, content, keywords) VALUES (?, ?, ?)`,
		mem.ID,
		mem.Content,
		string(keywords),
	)
	if err != nil {
		return fmt.Errorf("insert fts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit store: %w", err)
	}

	return nil
}

// Retrieve fetches a single memory by ID.
func (s *SQLiteStore) Retrieve(ctx context.Context, id string) (*tier.Memory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
		        token_count, relevance, tier, keywords, created_at, last_accessed_at, promoted_from
		 FROM memories WHERE id = ?`, id)

	mem, err := scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("retrieve memory %s: %w", id, err)
	}
	return mem, nil
}

// Query searches L2 using FTS5 full-text search. It extracts significant words
// from the prompt (dropping words under 3 characters), joins them with OR for
// the FTS5 MATCH expression, and normalizes FTS5 rank scores to a 0-1 range.
func (s *SQLiteStore) Query(ctx context.Context, prompt string, limit int) ([]tier.RetrievalResult, error) {
	terms := extractSearchTerms(prompt)
	if len(terms) == 0 {
		return nil, nil
	}

	matchExpr := strings.Join(terms, " OR ")

	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.conversation_id, m.topic_id, m.content, m.is_summary,
		        m.full_content_ref, m.token_count, m.relevance, m.tier, m.keywords,
		        m.created_at, m.last_accessed_at, m.promoted_from,
		        fts.rank
		 FROM memories_fts fts
		 JOIN memories m ON m.id = fts.memory_id
		 WHERE memories_fts MATCH ?
		 ORDER BY fts.rank
		 LIMIT ?`,
		matchExpr, limit)
	if err != nil {
		return nil, fmt.Errorf("query fts: %w", err)
	}
	defer rows.Close()

	type scored struct {
		mem  tier.Memory
		rank float64
	}

	var results []scored
	var minRank, maxRank float64
	first := true

	for rows.Next() {
		var (
			m           tier.Memory
			isSummary   int
			tierInt     int
			promotedInt int
			kwJSON      string
			createdStr  string
			accessedStr string
			rank        float64
		)

		err := rows.Scan(
			&m.ID, &m.ConversationID, &m.TopicID, &m.Content, &isSummary,
			&m.FullContentRef, &m.TokenCount, &m.Relevance, &tierInt, &kwJSON,
			&createdStr, &accessedStr, &promotedInt,
			&rank,
		)
		if err != nil {
			return nil, fmt.Errorf("scan query result: %w", err)
		}

		m.IsSummary = isSummary != 0
		m.Tier = tier.Level(tierInt)
		m.PromotedFrom = tier.Level(promotedInt)

		// Keywords stored as JSON by this package — unmarshal failure means DB corruption
		if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
			m.Keywords = nil
		}

		m.CreatedAt = parseRFC3339(createdStr)
		m.LastAccessedAt = parseRFC3339(accessedStr)

		if first {
			minRank = rank
			maxRank = rank
			first = false
		} else {
			minRank = min(minRank, rank)
			maxRank = max(maxRank, rank)
		}

		results = append(results, scored{mem: m, rank: rank})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate query results: %w", err)
	}

	// Normalize ranks to 0-1. FTS5 rank is negative (more negative = better match).
	// We invert so higher score = better match.
	out := make([]tier.RetrievalResult, len(results))
	for i, r := range results {
		score := 1.0
		if maxRank != minRank {
			// rank is negative; minRank is the best (most negative).
			// Normalize: best match -> 1.0, worst match -> ~0.0
			score = (maxRank - r.rank) / (maxRank - minRank)
		}
		out[i] = tier.RetrievalResult{
			Memory:     r.mem,
			Score:      score,
			SourceTier: tier.L2,
		}
	}

	return out, nil
}

// Delete removes a memory from both the memories table and the FTS index.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE memory_id = ?`, id); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

// List returns all L2 memories for a conversation, ordered by creation time descending.
// If conversationID is empty, all L2 memories are returned.
func (s *SQLiteStore) List(ctx context.Context, conversationID string) ([]tier.Memory, error) {
	var rows *sql.Rows
	var err error

	if conversationID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
			        token_count, relevance, tier, keywords, created_at, last_accessed_at, promoted_from
			 FROM memories WHERE tier = ? ORDER BY created_at DESC`,
			int(tier.L2))
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
			        token_count, relevance, tier, keywords, created_at, last_accessed_at, promoted_from
			 FROM memories WHERE conversation_id = ? AND tier = ? ORDER BY created_at DESC`,
			conversationID, int(tier.L2))
	}
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	var memories []tier.Memory
	for rows.Next() {
		mem, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan list result: %w", err)
		}
		memories = append(memories, *mem)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate list results: %w", err)
	}

	return memories, nil
}

// Level returns tier.L2, identifying this as the hot storage tier.
func (s *SQLiteStore) Level() tier.Level {
	return tier.L2
}

// UpdateRelevance updates the relevance score for a memory by ID.
func (s *SQLiteStore) UpdateRelevance(ctx context.Context, id string, score float64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE memories SET relevance = ?, last_accessed_at = ? WHERE id = ?`,
		score, time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("update relevance for %s: %w", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("memory %s not found", id)
	}
	return nil
}

// DB returns the underlying *sql.DB so other stores (e.g., L3) can share the connection.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// CreateTopic inserts a new topic into the topics table.
func (s *SQLiteStore) CreateTopic(ctx context.Context, topic *Topic) error {
	keywords, err := json.Marshal(topic.Keywords)
	if err != nil {
		return fmt.Errorf("marshal topic keywords: %w", err)
	}

	isCurrent := 0
	if topic.IsCurrent {
		isCurrent = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO topics (id, conversation_id, name, keywords, relevance, is_current, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		topic.ID,
		topic.ConversationID,
		topic.Name,
		string(keywords),
		topic.Relevance,
		isCurrent,
		topic.CreatedAt.Format(time.RFC3339),
		topic.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert topic: %w", err)
	}
	return nil
}

// GetTopic retrieves a single topic by ID.
func (s *SQLiteStore) GetTopic(ctx context.Context, id string) (*Topic, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, conversation_id, name, keywords, relevance, is_current, created_at, updated_at
		 FROM topics WHERE id = ?`, id)

	var (
		t          Topic
		kwJSON     string
		isCurrent  int
		createdStr string
		updatedStr string
	)

	err := row.Scan(&t.ID, &t.ConversationID, &t.Name, &kwJSON, &t.Relevance,
		&isCurrent, &createdStr, &updatedStr)
	if err != nil {
		return nil, fmt.Errorf("get topic %s: %w", id, err)
	}

	t.IsCurrent = isCurrent != 0

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	if err := json.Unmarshal([]byte(kwJSON), &t.Keywords); err != nil {
		t.Keywords = nil
	}

	t.CreatedAt = parseRFC3339(createdStr)
	t.UpdatedAt = parseRFC3339(updatedStr)

	return &t, nil
}

// ListTopics returns all topics for a conversation.
func (s *SQLiteStore) ListTopics(ctx context.Context, conversationID string) ([]Topic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, name, keywords, relevance, is_current, created_at, updated_at
		 FROM topics WHERE conversation_id = ? ORDER BY updated_at DESC`,
		conversationID)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer rows.Close()

	var topics []Topic
	for rows.Next() {
		var (
			t          Topic
			kwJSON     string
			isCurrent  int
			createdStr string
			updatedStr string
		)

		err := rows.Scan(&t.ID, &t.ConversationID, &t.Name, &kwJSON, &t.Relevance,
			&isCurrent, &createdStr, &updatedStr)
		if err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}

		t.IsCurrent = isCurrent != 0

		// Keywords stored as JSON by this package — unmarshal failure means DB corruption
		if err := json.Unmarshal([]byte(kwJSON), &t.Keywords); err != nil {
			t.Keywords = nil
		}

		t.CreatedAt = parseRFC3339(createdStr)
		t.UpdatedAt = parseRFC3339(updatedStr)

		topics = append(topics, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}

	return topics, nil
}

// scanMemory scans a single row from a *sql.Row into a Memory.
func scanMemory(row *sql.Row) (*tier.Memory, error) {
	var (
		m           tier.Memory
		isSummary   int
		tierInt     int
		promotedInt int
		kwJSON      string
		createdStr  string
		accessedStr string
	)

	err := row.Scan(
		&m.ID, &m.ConversationID, &m.TopicID, &m.Content, &isSummary,
		&m.FullContentRef, &m.TokenCount, &m.Relevance, &tierInt, &kwJSON,
		&createdStr, &accessedStr, &promotedInt,
	)
	if err != nil {
		return nil, err
	}

	m.IsSummary = isSummary != 0
	m.Tier = tier.Level(tierInt)
	m.PromotedFrom = tier.Level(promotedInt)

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
		m.Keywords = nil
	}

	m.CreatedAt = parseRFC3339(createdStr)
	m.LastAccessedAt = parseRFC3339(accessedStr)

	return &m, nil
}

// scanMemoryFromRows scans a single row from *sql.Rows into a Memory.
func scanMemoryFromRows(rows *sql.Rows) (*tier.Memory, error) {
	var (
		m           tier.Memory
		isSummary   int
		tierInt     int
		promotedInt int
		kwJSON      string
		createdStr  string
		accessedStr string
	)

	err := rows.Scan(
		&m.ID, &m.ConversationID, &m.TopicID, &m.Content, &isSummary,
		&m.FullContentRef, &m.TokenCount, &m.Relevance, &tierInt, &kwJSON,
		&createdStr, &accessedStr, &promotedInt,
	)
	if err != nil {
		return nil, err
	}

	m.IsSummary = isSummary != 0
	m.Tier = tier.Level(tierInt)
	m.PromotedFrom = tier.Level(promotedInt)

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	if err := json.Unmarshal([]byte(kwJSON), &m.Keywords); err != nil {
		m.Keywords = nil
	}

	m.CreatedAt = parseRFC3339(createdStr)
	m.LastAccessedAt = parseRFC3339(accessedStr)

	return &m, nil
}

// extractSearchTerms splits a prompt into significant words for FTS5 matching.
// Words shorter than 3 characters are dropped as common/stop words.
// All non-alphanumeric characters are stripped to avoid FTS5 syntax errors
// (characters like . * + - : ^ are FTS5 operators).
func extractSearchTerms(prompt string) []string {
	words := strings.Fields(prompt)
	terms := make([]string, 0, len(words))
	for _, w := range words {
		var cleaned strings.Builder
		for _, ch := range w {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
				cleaned.WriteRune(ch)
			}
		}
		term := cleaned.String()
		if len(term) >= 3 {
			terms = append(terms, term)
		}
	}
	return terms
}
