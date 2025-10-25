package retriever

import (
	"ai-search/internal/indexer"
	"context"
	"fmt"
	"time"
)

// Retriever defines the interface for document retrieval
type Retriever interface {
	// Retrieve retrieves documents based on a query
	Retrieve(ctx context.Context, query string, limit int) ([]*indexer.SearchResult, error)

	// SetReranker sets the reranker for post-processing results
	SetReranker(reranker Reranker)
}

// Reranker defines the interface for reranking search results
type Reranker interface {
	// Rerank reranks search results using LLM
	Rerank(ctx context.Context, query string, results []*indexer.SearchResult) ([]*indexer.SearchResult, error)
}

// Config holds retriever configuration
type Config struct {
	Indexer indexer.Indexer
	// Add more config as needed
}

// hybridRetriever implements the Retriever interface
type hybridRetriever struct {
	config   Config
	reranker Reranker
}

// NewHybridRetriever creates a new hybrid retriever
func NewHybridRetriever(config Config) Retriever {
	return &hybridRetriever{
		config: config,
	}
}

// Retrieve retrieves documents based on a query
func (r *hybridRetriever) Retrieve(ctx context.Context, query string, limit int) ([]*indexer.SearchResult, error) {
	// Use the indexer to perform hybrid search
	results, err := r.config.Indexer.Search(ctx, query, limit*2) // Get more results for reranking
	if err != nil {
		return nil, fmt.Errorf("failed to search index: %w", err)
	}

	// If we have a reranker, do async reranking in background
	if r.reranker != nil && len(results) > 0 {
		// Start async reranking in background - don't wait for it
		go func() {
			rerankCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := r.reranker.Rerank(rerankCtx, query, results)
			if err != nil {
				fmt.Printf("Warning: Async reranking failed: %v\n", err)
			} else {
				fmt.Printf("Async reranking completed for query: %s\n", query)
			}
		}()
	}

	// Limit results to requested amount
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SetReranker sets the reranker for post-processing results
func (r *hybridRetriever) SetReranker(reranker Reranker) {
	r.reranker = reranker
}
