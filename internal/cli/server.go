package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ai-search/internal/chunker"
	"ai-search/internal/config"
	"ai-search/internal/embeddings"
	"ai-search/internal/indexer"
	"ai-search/internal/llm"
	"ai-search/internal/retriever"
	"ai-search/internal/server"
	"ai-search/internal/store"

	"github.com/spf13/cobra"
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the AI search server",
	Long: `Start the HTTP API server for the AI search engine.
The server provides REST endpoints for searching indexed documents.`,
	RunE: runServer,
}

func init() {
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg := config.LoadConfig()

	// Validate required configuration
	if cfg.LLMAPIKey == "" {
		return fmt.Errorf("LLM_API_KEY environment variable is required")
	}
	if cfg.EmbeddingAPIKey == "" {
		return fmt.Errorf("EMBEDDING_API_KEY environment variable is required")
	}

	fmt.Println("Starting AI Search Server...")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Server: %s:%d\n", cfg.ServerHost, cfg.ServerPort)
	fmt.Printf("  Database: %s (%s:%d/%s)\n", cfg.DatabaseType, cfg.DatabaseHost, cfg.DatabasePort, cfg.DatabaseName)
	fmt.Printf("  ChromaDB: %s\n", cfg.ChromaURL)
	fmt.Printf("  Elasticsearch: %s\n", cfg.ElasticURL)
	fmt.Printf("  LLM: %s (%s)\n", cfg.LLMProvider, cfg.LLMModel)

	// Initialize components
	ctx := context.Background()

	// Initialize store
	storeConfig := store.Config{
		Type:     cfg.DatabaseType,
		Host:     cfg.DatabaseHost,
		Port:     cfg.DatabasePort,
		Database: cfg.DatabaseName,
		Username: cfg.DatabaseUser,
		Password: cfg.DatabasePassword,
		SSLMode:  cfg.DatabaseSSLMode,
	}
	documentStore := store.NewStore(storeConfig)
	defer documentStore.Close()

	// Initialize chunker
	chunkerConfig := chunker.Config{
		ChunkSize:    cfg.ChunkSize,
		OverlapSize:  cfg.OverlapSize,
		MinChunkSize: cfg.MinChunkSize,
	}
	textChunker := chunker.NewTextChunker(chunkerConfig)

	// Initialize embedder
	embedderConfig := embeddings.Config{
		Model:     cfg.EmbeddingModel,
		APIKey:    cfg.EmbeddingAPIKey,
		BaseURL:   cfg.EmbeddingBaseURL,
		BatchSize: 10,
		Timeout:   30,
	}
	embedder := embeddings.NewEmbedder(embedderConfig)

	// Initialize indexer
	indexerConfig := indexer.Config{
		Embedder:       embedder,
		Chunker:        textChunker,
		ChromaURL:      cfg.ChromaURL,
		ElasticURL:     cfg.ElasticURL,
		CollectionName: cfg.CollectionName,
	}
	hybridIndexer := indexer.NewIndexer(indexerConfig)
	defer hybridIndexer.Close()

	// Initialize LLM
	llmConfig := llm.Config{
		Provider: cfg.LLMProvider,
		Model:    cfg.LLMModel,
		APIKey:   cfg.LLMAPIKey,
		BaseURL:  cfg.LLMBaseURL,
		Timeout:  30,
	}
	llmClient := llm.NewLLM(llmConfig)

	// Initialize retriever
	retrieverConfig := retriever.Config{
		Indexer: hybridIndexer,
	}
	hybridRetriever := retriever.NewHybridRetriever(retrieverConfig)

	// Only enable reranking if configured
	if cfg.EnableReranking {
		hybridRetriever.SetReranker(&llmReranker{llm: llmClient})
		fmt.Printf("LLM reranking enabled\n")
	} else {
		fmt.Printf("LLM reranking disabled\n")
	}

	// Initialize server
	serverConfig := server.Config{
		Host:      cfg.ServerHost,
		Port:      cfg.ServerPort,
		Retriever: hybridRetriever,
	}
	httpServer := server.NewServer(serverConfig)

	// Start server
	fmt.Printf("\nServer starting on http://%s:%d\n", cfg.ServerHost, cfg.ServerPort)
	fmt.Println("Press Ctrl+C to stop the server")

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start server in goroutine
	go func() {
		if err := httpServer.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")
	cancel()

	return nil
}

// llmReranker implements the retriever.Reranker interface
type llmReranker struct {
	llm llm.LLM
}

// Rerank reranks search results using LLM
func (r *llmReranker) Rerank(ctx context.Context, query string, results []*indexer.SearchResult) ([]*indexer.SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	// Convert results to strings for LLM processing
	var resultTexts []string
	for _, result := range results {
		resultTexts = append(resultTexts, result.Text)
	}

	// Use LLM to rerank
	rerankedTexts, err := r.llm.Rerank(ctx, query, resultTexts)
	if err != nil {
		return results, err // Return original order if reranking fails
	}

	// Create a map of text to result for quick lookup
	textToResult := make(map[string]*indexer.SearchResult)
	for _, result := range results {
		textToResult[result.Text] = result
	}

	// Reorder results based on LLM reranking
	var rerankedResults []*indexer.SearchResult
	for _, text := range rerankedTexts {
		if result, exists := textToResult[text]; exists {
			rerankedResults = append(rerankedResults, result)
		}
	}

	// Add any results that weren't reranked (fallback)
	for _, result := range results {
		found := false
		for _, reranked := range rerankedResults {
			if reranked.ChunkID == result.ChunkID {
				found = true
				break
			}
		}
		if !found {
			rerankedResults = append(rerankedResults, result)
		}
	}

	return rerankedResults, nil
}
