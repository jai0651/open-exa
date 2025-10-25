package indexer

import (
	"ai-search/internal/chunker"
	"ai-search/internal/embeddings"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
)

// Indexer defines the interface for indexing content
type Indexer interface {
	// Index indexes a document with its chunks and embeddings
	Index(ctx context.Context, doc *Document, chunks []*chunker.Chunk, embeddings [][]float32) error

	// Search performs a search query
	Search(ctx context.Context, query string, limit int) ([]*SearchResult, error)

	// Close closes the indexer
	Close() error
}

// Document represents a document to be indexed
type Document struct {
	ID      string
	URL     string
	Title   string
	Content string
	Meta    map[string]interface{}
}

// SearchResult represents a search result
type SearchResult struct {
	DocumentID string
	ChunkID    string
	Score      float32
	Text       string
	Metadata   map[string]interface{}
}

// Config holds indexer configuration
type Config struct {
	Embedder       embeddings.Embedder
	Chunker        chunker.Chunker
	ChromaURL      string
	ElasticURL     string
	CollectionName string
}

// hybridIndexer implements the Indexer interface using ChromaDB and Elasticsearch
type hybridIndexer struct {
	config       Config
	httpClient   *http.Client
	chromaClient chroma.Client
	collection   chroma.Collection
}

// ChromaDB structures are now handled by the chroma-go client

// Elasticsearch structures
type ElasticsearchDoc struct {
	DocumentID string                 `json:"document_id"`
	ChunkID    string                 `json:"chunk_id"`
	Text       string                 `json:"text"`
	Title      string                 `json:"title"`
	URL        string                 `json:"url"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type ElasticsearchResponse struct {
	Hits struct {
		Hits []struct {
			ID     string           `json:"_id"`
			Score  float64          `json:"_score"`
			Source ElasticsearchDoc `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// NewIndexer creates a new indexer instance
func NewIndexer(config Config) Indexer {
	// Set defaults
	if config.ChromaURL == "" {
		config.ChromaURL = "http://localhost:8000"
	}
	if config.ElasticURL == "" {
		config.ElasticURL = "http://localhost:9200"
	}
	if config.CollectionName == "" {
		config.CollectionName = "ai_search_documents"
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create ChromaDB client
	chromaClient, err := chroma.NewHTTPClient(
		chroma.WithBaseURL(config.ChromaURL),
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to create ChromaDB client: %v", err))
	}

	indexer := &hybridIndexer{
		config:       config,
		httpClient:   httpClient,
		chromaClient: chromaClient,
	}

	// Initialize collections
	ctx := context.Background()
	indexer.initializeCollections(ctx)

	return indexer
}

// initializeCollections sets up ChromaDB collection and Elasticsearch index
func (i *hybridIndexer) initializeCollections(ctx context.Context) {
	// Create ChromaDB collection
	i.createChromaCollection(ctx)

	// Create Elasticsearch index
	i.createElasticsearchIndex(ctx)
}

// createChromaCollection creates a ChromaDB collection
func (i *hybridIndexer) createChromaCollection(ctx context.Context) {
	// Get or create collection using the ChromaDB client
	collection, err := i.chromaClient.GetOrCreateCollection(ctx, i.config.CollectionName)
	if err != nil {
		fmt.Printf("Failed to create ChromaDB collection: %v\n", err)
		return
	}
	i.collection = collection
	fmt.Printf("ChromaDB collection '%s' ready\n", i.config.CollectionName)
}

// createElasticsearchIndex creates an Elasticsearch index
func (i *hybridIndexer) createElasticsearchIndex(ctx context.Context) {
	indexName := "ai_search_documents"
	url := fmt.Sprintf("%s/%s", i.config.ElasticURL, indexName)

	// Check if index exists
	req, _ := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	resp, err := i.httpClient.Do(req)
	if err == nil && resp.StatusCode == 200 {
		resp.Body.Close()
		return // Index already exists
	}

	// Create index with mapping
	mapping := map[string]interface{}{
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"document_id": map[string]string{"type": "keyword"},
				"chunk_id":    map[string]string{"type": "keyword"},
				"text":        map[string]string{"type": "text", "analyzer": "standard"},
				"title":       map[string]string{"type": "text", "analyzer": "standard"},
				"url":         map[string]string{"type": "keyword"},
				"metadata":    map[string]string{"type": "object"},
			},
		},
	}

	jsonData, _ := json.Marshal(mapping)
	req, _ = http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(string(jsonData)))
	req.Header.Set("Content-Type", "application/json")

	resp, err = i.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// Index indexes a document with its chunks and embeddings
func (i *hybridIndexer) Index(ctx context.Context, doc *Document, chunks []*chunker.Chunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("chunks and embeddings count mismatch")
	}

	// Index in ChromaDB (vector search)
	if err := i.indexInChroma(ctx, doc, chunks, embeddings); err != nil {
		return fmt.Errorf("failed to index in ChromaDB: %w", err)
	}

	// Index in Elasticsearch (BM25 search)
	if err := i.indexInElasticsearch(ctx, doc, chunks); err != nil {
		return fmt.Errorf("failed to index in Elasticsearch: %w", err)
	}

	return nil
}

// indexInChroma indexes documents in ChromaDB
func (i *hybridIndexer) indexInChroma(ctx context.Context, doc *Document, chunks []*chunker.Chunk, embeddings [][]float32) error {
	if i.collection == nil {
		return fmt.Errorf("ChromaDB collection not initialized")
	}

	// Prepare data for ChromaDB
	documents := make([]string, len(chunks))
	metadatas := make([]chroma.DocumentMetadata, len(chunks))
	ids := make([]string, len(chunks))

	for j, chunk := range chunks {
		documents[j] = chunk.Text
		metadatas[j] = chroma.NewDocumentMetadata(
			chroma.NewStringAttribute("document_id", doc.ID),
			chroma.NewStringAttribute("chunk_id", chunk.ID),
			chroma.NewStringAttribute("title", doc.Title),
			chroma.NewStringAttribute("url", doc.URL),
			chroma.NewIntAttribute("start_pos", int64(chunk.StartPos)),
			chroma.NewIntAttribute("end_pos", int64(chunk.EndPos)),
		)
		ids[j] = chunk.ID
	}

	// Add to ChromaDB using the client
	// Convert string IDs to DocumentID type
	documentIDs := make([]chroma.DocumentID, len(ids))
	for i, id := range ids {
		documentIDs[i] = chroma.DocumentID(id)
	}

	err := i.collection.Add(ctx,
		chroma.WithIDs(documentIDs...),
		chroma.WithTexts(documents...),
		chroma.WithMetadatas(metadatas...),
	)
	if err != nil {
		return fmt.Errorf("failed to add to ChromaDB: %w", err)
	}

	return nil
}

// indexInElasticsearch indexes documents in Elasticsearch
func (i *hybridIndexer) indexInElasticsearch(ctx context.Context, doc *Document, chunks []*chunker.Chunk) error {
	indexName := "ai_search_documents"

	for _, chunk := range chunks {
		docData := ElasticsearchDoc{
			DocumentID: doc.ID,
			ChunkID:    chunk.ID,
			Text:       chunk.Text,
			Title:      doc.Title,
			URL:        doc.URL,
			Metadata:   chunk.Metadata,
		}

		jsonData, err := json.Marshal(docData)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/%s/_doc/%s", i.config.ElasticURL, indexName, chunk.ID)
		req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(string(jsonData)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := i.httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("Elasticsearch request failed with status %d", resp.StatusCode)
		}
	}

	return nil
}

// Search performs a hybrid search query
func (i *hybridIndexer) Search(ctx context.Context, query string, limit int) ([]*SearchResult, error) {
	// Get query embedding
	queryEmbedding, err := i.config.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get query embedding: %w", err)
	}

	// Vector search in ChromaDB
	vectorResults, err := i.searchChroma(ctx, queryEmbedding, limit*2) // Get more results for reranking
	if err != nil {
		return nil, fmt.Errorf("failed to search ChromaDB: %w", err)
	}

	// BM25 search in Elasticsearch
	bm25Results, err := i.searchElasticsearch(ctx, query, limit*2)
	if err != nil {
		return nil, fmt.Errorf("failed to search Elasticsearch: %w", err)
	}

	// Combine and rerank results
	combinedResults := i.combineResults(vectorResults, bm25Results, limit)

	return combinedResults, nil
}

// searchChroma performs vector search in ChromaDB
func (i *hybridIndexer) searchChroma(ctx context.Context, queryEmbedding []float32, limit int) ([]*SearchResult, error) {
	if i.collection == nil {
		return nil, fmt.Errorf("ChromaDB collection not initialized")
	}

	// Query ChromaDB using the client
	queryResult, err := i.collection.Query(ctx,
		chroma.WithQueryTexts("query"), // Use text query instead of embeddings for now
		chroma.WithNResults(limit),
		chroma.WithIncludeQuery(chroma.IncludeDocuments, chroma.IncludeMetadatas, chroma.IncludeDistances),
	)
	if err != nil {
		return nil, fmt.Errorf("ChromaDB query failed: %w", err)
	}

	var results []*SearchResult
	documentGroups := queryResult.GetDocumentsGroups()
	if len(documentGroups) > 0 && len(documentGroups[0]) > 0 {
		documents := documentGroups[0]
		metadataGroups := queryResult.GetMetadatasGroups()
		distanceGroups := queryResult.GetDistancesGroups()

		metadatas := metadataGroups[0]
		distances := distanceGroups[0]

		for j, document := range documents {
			if j < len(metadatas) && j < len(distances) {
				score := float32(1.0 - distances[j]) // Convert distance to similarity

				// Convert document to string
				documentText := fmt.Sprintf("%v", document)

				// Convert metadata to map
				metadataMap := make(map[string]interface{})
				// For now, just use a simple approach
				metadataMap["chunk_id"] = fmt.Sprintf("chunk_%d", j)

				results = append(results, &SearchResult{
					DocumentID: "unknown", // Will be extracted from metadata later
					ChunkID:    fmt.Sprintf("chunk_%d", j),
					Score:      score,
					Text:       documentText,
					Metadata:   metadataMap,
				})
			}
		}
	}

	return results, nil
}

// searchElasticsearch performs BM25 search in Elasticsearch
func (i *hybridIndexer) searchElasticsearch(ctx context.Context, query string, limit int) ([]*SearchResult, error) {
	indexName := "ai_search_documents"
	url := fmt.Sprintf("%s/%s/_search", i.config.ElasticURL, indexName)

	payload := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"text^2", "title^1.5"},
			},
		},
		"size": limit,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Elasticsearch search failed with status %d", resp.StatusCode)
	}

	var response ElasticsearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	var results []*SearchResult
	for _, hit := range response.Hits.Hits {
		results = append(results, &SearchResult{
			DocumentID: hit.Source.DocumentID,
			ChunkID:    hit.Source.ChunkID,
			Score:      float32(hit.Score),
			Text:       hit.Source.Text,
			Metadata:   hit.Source.Metadata,
		})
	}

	return results, nil
}

// combineResults combines and reranks results from both search methods
func (i *hybridIndexer) combineResults(vectorResults, bm25Results []*SearchResult, limit int) []*SearchResult {
	// Create a map to track unique results
	resultMap := make(map[string]*SearchResult)

	// Add vector results with higher weight
	for _, result := range vectorResults {
		key := result.ChunkID
		if existing, exists := resultMap[key]; exists {
			// Combine scores (weighted average)
			existing.Score = (existing.Score*0.3 + result.Score*0.7)
		} else {
			result.Score *= 0.7 // Weight vector results
			resultMap[key] = result
		}
	}

	// Add BM25 results
	for _, result := range bm25Results {
		key := result.ChunkID
		if existing, exists := resultMap[key]; exists {
			// Combine scores (weighted average)
			existing.Score = (existing.Score*0.7 + result.Score*0.3)
		} else {
			result.Score *= 0.3 // Weight BM25 results
			resultMap[key] = result
		}
	}

	// Convert to slice and sort by score
	var combinedResults []*SearchResult
	for _, result := range resultMap {
		combinedResults = append(combinedResults, result)
	}

	// Simple sort by score (descending)
	for i := 0; i < len(combinedResults); i++ {
		for j := i + 1; j < len(combinedResults); j++ {
			if combinedResults[i].Score < combinedResults[j].Score {
				combinedResults[i], combinedResults[j] = combinedResults[j], combinedResults[i]
			}
		}
	}

	// Return top results
	if len(combinedResults) > limit {
		return combinedResults[:limit]
	}

	return combinedResults
}

// Close closes the indexer
func (i *hybridIndexer) Close() error {
	if i.chromaClient != nil {
		return i.chromaClient.Close()
	}
	return nil
}
