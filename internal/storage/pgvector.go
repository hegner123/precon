package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"

	"github.com/hegner123/precon/internal/embedding"
	"github.com/hegner123/precon/internal/tier"
)

// PgvectorStore implements tier.Store backed by PostgreSQL + pgvector for L4 semantic storage.
type PgvectorStore struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	embedC *embedding.Client // embedding client for query-time embedding
}

// Compile-time check that PgvectorStore implements tier.Store.
var _ tier.Store = (*PgvectorStore)(nil)

// NewPgvectorStore connects to PostgreSQL and registers pgvector types.
// The embedClient is used to embed query prompts for semantic similarity search.
func NewPgvectorStore(ctx context.Context, connStr string, embedClient *embedding.Client, log *slog.Logger) (*PgvectorStore, error) {
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse pg config: %w", err)
	}

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to pg: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pg: %w", err)
	}

	log.Debug("pgvector store connected")
	return &PgvectorStore{pool: pool, log: log, embedC: embedClient}, nil
}

// InitSchema creates the pgvector extension and precon_memories table with HNSW index.
// Returns a VectorNotEnabled error if the vector extension is not available,
// which callers can check to provide a user-friendly setup prompt.
func (s *PgvectorStore) InitSchema(ctx context.Context) error {
	// Step 1: Check if vector extension is available
	if err := s.ensureVectorExtension(ctx); err != nil {
		return err
	}

	// Step 2: Create table and indexes
	tableQueries := []string{
		`CREATE TABLE IF NOT EXISTS precon_memories (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			topic_id TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			is_summary INTEGER DEFAULT 0,
			full_content_ref TEXT DEFAULT '',
			token_count INTEGER DEFAULT 0,
			relevance REAL DEFAULT 1.0,
			keywords JSONB DEFAULT '[]',
			embedding vector(768),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			promoted_from INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS precon_memories_conversation_idx
			ON precon_memories(conversation_id)`,
		`CREATE INDEX IF NOT EXISTS precon_memories_embedding_idx
			ON precon_memories USING hnsw (embedding vector_cosine_ops)`,
	}

	for _, q := range tableQueries {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("init L4 schema: %w", err)
		}
	}

	s.log.Debug("L4 schema initialized")
	return nil
}

// VectorNotEnabled is returned when PostgreSQL is reachable but the vector
// extension is not enabled. Callers should prompt the user to enable it.
type VectorNotEnabled struct {
	ConnStr string
	DBName  string
}

func (e *VectorNotEnabled) Error() string {
	return fmt.Sprintf("pgvector extension is not enabled on database %q", e.DBName)
}

// ensureVectorExtension checks if the vector extension exists, and if not,
// attempts to create it. If creation fails (permissions), returns VectorNotEnabled
// with enough info for the caller to guide the user.
func (s *PgvectorStore) ensureVectorExtension(ctx context.Context) error {
	// Check if already enabled
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')").Scan(&exists)
	if err != nil {
		return fmt.Errorf("check vector extension: %w", err)
	}
	if exists {
		return nil
	}

	// Try to create it
	_, err = s.pool.Exec(ctx, "CREATE EXTENSION vector")
	if err == nil {
		s.log.Info("pgvector extension created")
		return nil
	}

	// Creation failed — extract DB name for the error message
	var dbName string
	if scanErr := s.pool.QueryRow(ctx, "SELECT current_database()").Scan(&dbName); scanErr != nil {
		dbName = "(unknown)"
	}

	return &VectorNotEnabled{DBName: dbName}
}

// Store saves a memory to L4 with its embedding.
// The memory's Content is embedded before storage.
func (s *PgvectorStore) Store(ctx context.Context, mem *tier.Memory) error {
	start := time.Now()

	// Generate embedding for the content
	emb, err := s.embedC.Embed(ctx, mem.Content)
	if err != nil {
		return fmt.Errorf("embed content for L4 store: %w", err)
	}

	keywordsJSON, err := json.Marshal(mem.Keywords)
	if err != nil {
		keywordsJSON = []byte("[]")
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO precon_memories
			(id, conversation_id, topic_id, content, is_summary, full_content_ref,
			 token_count, relevance, keywords, embedding, created_at, last_accessed_at, promoted_from)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (id) DO UPDATE SET
			content = EXCLUDED.content,
			embedding = EXCLUDED.embedding,
			relevance = EXCLUDED.relevance,
			keywords = EXCLUDED.keywords,
			last_accessed_at = EXCLUDED.last_accessed_at`,
		mem.ID, mem.ConversationID, mem.TopicID, mem.Content,
		mem.IsSummary, mem.FullContentRef,
		mem.TokenCount, mem.Relevance,
		keywordsJSON, pgvector.NewVector([]float32(emb)),
		mem.CreatedAt, mem.LastAccessedAt, int(mem.PromotedFrom),
	)
	if err != nil {
		return fmt.Errorf("L4 store: %w", err)
	}

	s.log.Debug("L4 stored memory", "id", mem.ID, "tokens", mem.TokenCount, "elapsed", time.Since(start))
	return nil
}

// StoreWithEmbedding saves a memory to L4 with a pre-computed embedding.
// Use this when the embedding was already generated (e.g., during eviction).
func (s *PgvectorStore) StoreWithEmbedding(ctx context.Context, mem *tier.Memory, emb embedding.Embedding) error {
	keywordsJSON, err := json.Marshal(mem.Keywords)
	if err != nil {
		keywordsJSON = []byte("[]")
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO precon_memories
			(id, conversation_id, topic_id, content, is_summary, full_content_ref,
			 token_count, relevance, keywords, embedding, created_at, last_accessed_at, promoted_from)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (id) DO UPDATE SET
			content = EXCLUDED.content,
			embedding = EXCLUDED.embedding,
			relevance = EXCLUDED.relevance,
			keywords = EXCLUDED.keywords,
			last_accessed_at = EXCLUDED.last_accessed_at`,
		mem.ID, mem.ConversationID, mem.TopicID, mem.Content,
		mem.IsSummary, mem.FullContentRef,
		mem.TokenCount, mem.Relevance,
		keywordsJSON, pgvector.NewVector([]float32(emb)),
		mem.CreatedAt, mem.LastAccessedAt, int(mem.PromotedFrom),
	)
	if err != nil {
		return fmt.Errorf("L4 store with embedding: %w", err)
	}
	return nil
}

// Retrieve fetches a single memory by ID from L4.
func (s *PgvectorStore) Retrieve(ctx context.Context, id string) (*tier.Memory, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
				token_count, relevance, keywords, created_at, last_accessed_at, promoted_from
		 FROM precon_memories WHERE id = $1`, id)

	mem, err := s.scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("L4 retrieve %s: %w", id, err)
	}

	// Update last_accessed_at — best-effort, don't fail the read for a timestamp update
	if _, err := s.pool.Exec(ctx, "UPDATE precon_memories SET last_accessed_at = $1 WHERE id = $2", time.Now(), id); err != nil {
		s.log.Warn("failed to update last_accessed_at", "id", id, "error", err)
	}

	return mem, nil
}

// Query performs semantic similarity search against L4.
// The prompt is embedded via the RunPod client, then matched against stored vectors.
func (s *PgvectorStore) Query(ctx context.Context, prompt string, limit int) ([]tier.RetrievalResult, error) {
	start := time.Now()

	// Embed the prompt
	emb, err := s.embedC.Embed(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("embed prompt for L4 query: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
				token_count, relevance, keywords, created_at, last_accessed_at, promoted_from,
				1 - (embedding <=> $1) AS similarity
		 FROM precon_memories
		 ORDER BY embedding <=> $1
		 LIMIT $2`,
		pgvector.NewVector([]float32(emb)), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("L4 query: %w", err)
	}
	defer rows.Close()

	var results []tier.RetrievalResult
	for rows.Next() {
		var mem tier.Memory
		var keywordsJSON []byte
		var similarity float64
		var promotedFrom int

		if err := rows.Scan(
			&mem.ID, &mem.ConversationID, &mem.TopicID, &mem.Content,
			&mem.IsSummary, &mem.FullContentRef,
			&mem.TokenCount, &mem.Relevance,
			&keywordsJSON, &mem.CreatedAt, &mem.LastAccessedAt,
			&promotedFrom, &similarity,
		); err != nil {
			return nil, fmt.Errorf("scan L4 result: %w", err)
		}

		// Keywords stored as JSON by this package — unmarshal failure means DB corruption
		if err := json.Unmarshal(keywordsJSON, &mem.Keywords); err != nil {
			s.log.Warn("corrupt keywords JSON in L4", "id", mem.ID, "error", err)
		}
		mem.Tier = tier.L4
		mem.PromotedFrom = tier.Level(promotedFrom)

		results = append(results, tier.RetrievalResult{
			Memory:     mem,
			Score:      similarity,
			SourceTier: tier.L4,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("L4 query rows: %w", err)
	}

	// Batch update last_accessed_at for returned results — best-effort
	if len(results) > 0 {
		now := time.Now()
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.Memory.ID
		}
		if _, err := s.pool.Exec(ctx,
			"UPDATE precon_memories SET last_accessed_at = $1 WHERE id = ANY($2)",
			now, ids); err != nil {
			s.log.Warn("failed to batch update last_accessed_at", "count", len(ids), "error", err)
		}
	}

	s.log.Debug("L4 query complete",
		"results", len(results),
		"elapsed", time.Since(start),
	)

	return results, nil
}

// Delete removes a memory from L4 by ID.
func (s *PgvectorStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM precon_memories WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("L4 delete %s: %w", id, err)
	}
	return nil
}

// List returns all memories for a conversation from L4.
// If conversationID is empty, all L4 memories are returned (matches SQLiteStore.List behavior).
func (s *PgvectorStore) List(ctx context.Context, conversationID string) ([]tier.Memory, error) {
	var rows pgx.Rows
	var err error

	if conversationID == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
					token_count, relevance, keywords, created_at, last_accessed_at, promoted_from
			 FROM precon_memories
			 ORDER BY created_at DESC`)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, conversation_id, topic_id, content, is_summary, full_content_ref,
					token_count, relevance, keywords, created_at, last_accessed_at, promoted_from
			 FROM precon_memories
			 WHERE conversation_id = $1
			 ORDER BY created_at DESC`, conversationID)
	}
	if err != nil {
		return nil, fmt.Errorf("L4 list: %w", err)
	}
	defer rows.Close()

	var memories []tier.Memory
	for rows.Next() {
		var mem tier.Memory
		var keywordsJSON []byte
		var promotedFrom int

		if err := rows.Scan(
			&mem.ID, &mem.ConversationID, &mem.TopicID, &mem.Content,
			&mem.IsSummary, &mem.FullContentRef,
			&mem.TokenCount, &mem.Relevance,
			&keywordsJSON, &mem.CreatedAt, &mem.LastAccessedAt,
			&promotedFrom,
		); err != nil {
			return nil, fmt.Errorf("scan L4 memory: %w", err)
		}

		// Keywords stored as JSON by this package — unmarshal failure means DB corruption
		if err := json.Unmarshal(keywordsJSON, &mem.Keywords); err != nil {
			s.log.Warn("corrupt keywords JSON in L4", "id", mem.ID, "error", err)
		}
		mem.Tier = tier.L4
		mem.PromotedFrom = tier.Level(promotedFrom)
		memories = append(memories, mem)
	}

	return memories, rows.Err()
}

// Level returns tier.L4.
func (s *PgvectorStore) Level() tier.Level {
	return tier.L4
}

// Close shuts down the connection pool.
func (s *PgvectorStore) Close() {
	s.pool.Close()
}

// TotalTokens returns the sum of token_count across all L4 memories.
func (s *PgvectorStore) TotalTokens(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, "SELECT COALESCE(SUM(token_count), 0) FROM precon_memories").Scan(&total)
	return total, err
}

// scanMemory scans a single row into a tier.Memory.
func (s *PgvectorStore) scanMemory(row pgx.Row) (*tier.Memory, error) {
	var mem tier.Memory
	var keywordsJSON []byte
	var promotedFrom int

	if err := row.Scan(
		&mem.ID, &mem.ConversationID, &mem.TopicID, &mem.Content,
		&mem.IsSummary, &mem.FullContentRef,
		&mem.TokenCount, &mem.Relevance,
		&keywordsJSON, &mem.CreatedAt, &mem.LastAccessedAt,
		&promotedFrom,
	); err != nil {
		return nil, err
	}

	// Keywords stored as JSON by this package — unmarshal failure means DB corruption
	json.Unmarshal(keywordsJSON, &mem.Keywords)
	mem.Tier = tier.L4
	mem.PromotedFrom = tier.Level(promotedFrom)
	return &mem, nil
}
