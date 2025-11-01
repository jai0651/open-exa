package store

import (
	"ai-search/internal/chunker"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Store defines the interface for persistent storage
type Store interface {
	// SaveDocument saves a document
	SaveDocument(ctx context.Context, doc *Document) error

	// GetDocument retrieves a document by ID
	GetDocument(ctx context.Context, id string) (*Document, error)

	// SaveChunks saves document chunks
	SaveChunks(ctx context.Context, docID string, chunks []*chunker.Chunk) error

	// GetChunks retrieves chunks for a document
	GetChunks(ctx context.Context, docID string) ([]*chunker.Chunk, error)

	// Close closes the store
	Close() error
}

// Document represents a stored document
type Document struct {
	ID        string
	URL       string
	Title     string
	Content   string
	Meta      map[string]interface{}
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Config holds store configuration
type Config struct {
	Type     string // "memory", "postgres", etc.
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
}

// postgresStore implements the Store interface using PostgreSQL
type postgresStore struct {
	db *sql.DB
}

// NewStore creates a new store instance
func NewStore(config Config) Store {
	if config.Type == "" {
		config.Type = "postgres"
	}
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 5432
	}
	if config.Database == "" {
		config.Database = "ai_search"
	}
	if config.Username == "" {
		config.Username = "postgres"
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database, config.SSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(fmt.Sprintf("Failed to open database: %v", err))
	}

	store := &postgresStore{db: db}

	// Initialize database schema
	if err := store.initSchema(); err != nil {
		panic(fmt.Sprintf("Failed to initialize database schema: %v", err))
	}

	return store
}

// initSchema creates the necessary database tables
func (s *postgresStore) initSchema() error {
	// Create documents table
	documentsSQL := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(255) PRIMARY KEY,
		url TEXT NOT NULL,
		title TEXT,
		content TEXT,
		meta JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	// Create chunks table
	chunksSQL := `
	CREATE TABLE IF NOT EXISTS chunks (
		id VARCHAR(255) PRIMARY KEY,
		document_id VARCHAR(255) NOT NULL,
		text TEXT NOT NULL,
		start_pos INTEGER,
		end_pos INTEGER,
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE
	);`

	// Create indexes
	indexesSQL := []string{
		"CREATE INDEX IF NOT EXISTS idx_documents_url ON documents (url);",
		"CREATE INDEX IF NOT EXISTS idx_chunks_document_id ON chunks (document_id);",
		"CREATE INDEX IF NOT EXISTS idx_chunks_text ON chunks USING gin(to_tsvector('english', text));",
		"CREATE INDEX IF NOT EXISTS idx_documents_meta ON documents USING gin(meta);",
		"CREATE INDEX IF NOT EXISTS idx_chunks_metadata ON chunks USING gin(metadata);",
	}

	if _, err := s.db.Exec(documentsSQL); err != nil {
		return fmt.Errorf("failed to create documents table: %w", err)
	}

	if _, err := s.db.Exec(chunksSQL); err != nil {
		return fmt.Errorf("failed to create chunks table: %w", err)
	}

	for _, indexSQL := range indexesSQL {
		if _, err := s.db.Exec(indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// SaveDocument saves a document
func (s *postgresStore) SaveDocument(ctx context.Context, doc *Document) error {
	// Convert metadata to JSON bytes
	var metaJSON []byte
	if doc.Meta != nil {
		var err error
		metaJSON, err = json.Marshal(doc.Meta)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	query := `
	INSERT INTO documents (id, url, title, content, meta, updated_at)
	VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
	ON CONFLICT (id) DO UPDATE SET
		url = EXCLUDED.url,
		title = EXCLUDED.title,
		content = EXCLUDED.content,
		meta = EXCLUDED.meta,
		updated_at = CURRENT_TIMESTAMP`

	_, err := s.db.ExecContext(ctx, query, doc.ID, doc.URL, doc.Title, doc.Content, metaJSON)
	if err != nil {
		return fmt.Errorf("failed to save document: %w", err)
	}

	return nil
}

// GetDocument retrieves a document by ID
func (s *postgresStore) GetDocument(ctx context.Context, id string) (*Document, error) {
	query := `
	SELECT id, url, title, content, meta, created_at, updated_at
	FROM documents WHERE id = $1`

	var doc Document
	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&doc.ID, &doc.URL, &doc.Title, &doc.Content, &doc.Meta, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("document not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	doc.CreatedAt = createdAt
	doc.UpdatedAt = updatedAt

	return &doc, nil
}

// SaveChunks saves document chunks
func (s *postgresStore) SaveChunks(ctx context.Context, docID string, chunks []*chunker.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing chunks for this document
	deleteQuery := "DELETE FROM chunks WHERE document_id = $1"
	if _, err := tx.ExecContext(ctx, deleteQuery, docID); err != nil {
		return fmt.Errorf("failed to delete existing chunks: %w", err)
	}

	// Insert new chunks
	insertQuery := `
	INSERT INTO chunks (id, document_id, text, start_pos, end_pos, metadata)
	VALUES ($1, $2, $3, $4, $5, $6)`

	for _, chunk := range chunks {
		// Convert metadata to JSON bytes
		var metaJSON []byte
		if chunk.Metadata != nil {
			var err error
			metaJSON, err = json.Marshal(chunk.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal chunk metadata: %w", err)
			}
		}

		_, err = tx.ExecContext(ctx, insertQuery,
			chunk.ID, docID, chunk.Text, chunk.StartPos, chunk.EndPos, metaJSON)
		if err != nil {
			return fmt.Errorf("failed to insert chunk: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetChunks retrieves chunks for a document
func (s *postgresStore) GetChunks(ctx context.Context, docID string) ([]*chunker.Chunk, error) {
	query := `
	SELECT id, text, start_pos, end_pos, metadata
	FROM chunks WHERE document_id = $1
	ORDER BY start_pos`

	rows, err := s.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*chunker.Chunk
	for rows.Next() {
		var chunk chunker.Chunk

		err := rows.Scan(&chunk.ID, &chunk.Text, &chunk.StartPos, &chunk.EndPos, &chunk.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}

		chunks = append(chunks, &chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate chunks: %w", err)
	}

	return chunks, nil
}

// Close closes the store
func (s *postgresStore) Close() error {
	return s.db.Close()
}
