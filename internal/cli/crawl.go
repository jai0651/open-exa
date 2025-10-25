package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"ai-search/internal/chunker"
	"ai-search/internal/config"
	"ai-search/internal/crawler"
	"ai-search/internal/embeddings"
	"ai-search/internal/indexer"
	"ai-search/internal/store"

	"github.com/spf13/cobra"
)

var (
	crawlURL   string
	crawlDepth int
)

// crawlCmd represents the crawl command
var crawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Crawl and parse web pages",
	Long: `Crawl web pages starting from a given URL, respecting robots.txt
and implementing polite crawling with rate limiting.`,
	RunE: runCrawl,
}

func init() {
	crawlCmd.Flags().StringVarP(&crawlURL, "url", "u", "", "Starting URL to crawl (required)")
	crawlCmd.Flags().IntVarP(&crawlDepth, "depth", "d", 1, "Maximum crawl depth")

	crawlCmd.MarkFlagRequired("url")
}

func runCrawl(cmd *cobra.Command, args []string) error {
	// Parse the starting URL
	startURL, err := url.Parse(crawlURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Load configuration
	cfg := config.LoadConfig()

	// Validate required configuration for indexing
	if cfg.EmbeddingAPIKey == "" {
		return fmt.Errorf("EMBEDDING_API_KEY environment variable is required for indexing")
	}

	fmt.Printf("Starting crawl of %s (depth: %d)\n", crawlURL, crawlDepth)
	fmt.Println("Initializing components...")

	// Initialize components
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

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

	// Create crawler configuration
	crawlerConfig := crawler.Config{
		MaxWorkers:    cfg.MaxWorkers,
		RateLimit:     cfg.RateLimit,
		MaxPageSize:   cfg.MaxPageSize,
		UserAgent:     cfg.UserAgent,
		Timeout:       cfg.Timeout,
		RespectRobots: cfg.RespectRobots,
	}

	// Create crawler instance
	c := crawler.NewCrawler(crawlerConfig)

	fmt.Println("Starting crawl and indexing...")

	// Start crawling
	pageChan, errorChan := c.Crawl(ctx, startURL, crawlDepth)

	// Process results
	pageCount := 0
	errorCount := 0
	indexedCount := 0

	for {
		select {
		case page, ok := <-pageChan:
			if !ok {
				// Channel closed, check for errors
				select {
				case err := <-errorChan:
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						errorCount++
					}
				default:
					// No more errors
				}
				goto done
			}

			pageCount++
			fmt.Printf("Processing page %d: %s\n", pageCount, page.Title)

			// Save document to store
			doc := &store.Document{
				ID:      page.ContentHash,
				URL:     page.URL.String(),
				Title:   page.Title,
				Content: page.Content,
				Meta: map[string]interface{}{
					"meta_desc":    page.MetaDesc,
					"links_count":  len(page.Links),
					"depth":        page.Depth,
					"content_hash": page.ContentHash,
				},
			}

			if err := documentStore.SaveDocument(ctx, doc); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to save document: %v\n", err)
				continue
			}

			// Chunk the content
			chunks := textChunker.Chunk(page.Content)
			if len(chunks) == 0 {
				fmt.Printf("  No chunks created for %s\n", page.Title)
				continue
			}

			// Generate embeddings for chunks
			var chunkTexts []string
			for _, chunk := range chunks {
				chunkTexts = append(chunkTexts, chunk.Text)
			}

			embeddings, err := embedder.EmbedBatch(ctx, chunkTexts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate embeddings: %v\n", err)
				continue
			}

			// Save chunks to store
			if err := documentStore.SaveChunks(ctx, doc.ID, chunks); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to save chunks: %v\n", err)
				continue
			}

			// Index in vector and keyword search
			indexDoc := &indexer.Document{
				ID:      doc.ID,
				URL:     doc.URL,
				Title:   doc.Title,
				Content: doc.Content,
				Meta:    doc.Meta,
			}

			if err := hybridIndexer.Index(ctx, indexDoc, chunks, embeddings); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to index document: %v\n", err)
				continue
			}

			indexedCount++
			fmt.Printf("  Indexed %d chunks for %s\n", len(chunks), page.Title)

		case err := <-errorChan:
			if err != nil {
				fmt.Fprintf(os.Stderr, "Crawl error: %v\n", err)
				errorCount++
			}
		}
	}

done:
	fmt.Printf("\nCrawl completed. Processed %d pages, indexed %d pages, %d errors.\n", pageCount, indexedCount, errorCount)
	return nil
}

// truncateText truncates text to the specified length
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
