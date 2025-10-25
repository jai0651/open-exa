package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder defines the interface for generating embeddings
type Embedder interface {
	// Embed generates embeddings for the given text
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the embedding dimension size
	Dimensions() int
}

// Config holds embedder configuration
type Config struct {
	Model     string
	BatchSize int
	Timeout   int
	APIKey    string
	BaseURL   string
}

// openAIEmbedder implements the Embedder interface using OpenAI API
type openAIEmbedder struct {
	config     Config
	httpClient *http.Client
	dimensions int
}

// OpenAIRequest represents the request structure for OpenAI API
type OpenAIRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// OpenAIResponse represents the response structure from OpenAI API
type OpenAIResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// NewEmbedder creates a new embedder instance
func NewEmbedder(config Config) Embedder {
	// Set defaults
	if config.Model == "" {
		config.Model = "text-embedding-3-small" // Default model
	}
	if config.BatchSize == 0 {
		config.BatchSize = 10 // Default batch size
	}
	if config.Timeout == 0 {
		config.Timeout = 30 // Default timeout in seconds
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}

	httpClient := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	return &openAIEmbedder{
		config:     config,
		httpClient: httpClient,
		dimensions: 1536, // text-embedding-3-small dimensions
	}
}

// Embed generates embeddings for the given text
func (e *openAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts
func (e *openAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// Split into batches if necessary
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += e.config.BatchSize {
		end := i + e.config.BatchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// embedBatch processes a single batch of texts
func (e *openAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	request := OpenAIRequest{
		Model: e.config.Model,
		Input: texts,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.config.BaseURL+"/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Sort embeddings by index to maintain order
	embeddings := make([][]float32, len(texts))
	for _, data := range response.Data {
		if data.Index < len(embeddings) {
			embeddings[data.Index] = data.Embedding
		}
	}

	return embeddings, nil
}

// Dimensions returns the embedding dimension size
func (e *openAIEmbedder) Dimensions() int {
	return e.dimensions
}
