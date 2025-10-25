package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLM defines the interface for language model interactions
type LLM interface {
	// Generate generates text based on a prompt
	Generate(ctx context.Context, prompt string) (string, error)

	// Rerank reranks search results based on relevance
	Rerank(ctx context.Context, query string, results []string) ([]string, error)
}

// Config holds LLM configuration
type Config struct {
	Provider string // "openai", "anthropic", "local", etc.
	Model    string
	APIKey   string
	BaseURL  string
	Timeout  int
}

// openRouterLLM implements the LLM interface using OpenRouter API
type openRouterLLM struct {
	config     Config
	httpClient *http.Client
}

// OpenRouterRequest represents the request structure for OpenRouter API
type OpenRouterRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

// Message represents a message in the conversation
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenRouterResponse represents the response structure from OpenRouter API
type OpenRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// RerankResult represents a reranked result
type RerankResult struct {
	Text  string
	Score float64
	Index int
}

// NewLLM creates a new LLM instance
func NewLLM(config Config) LLM {
	// Set defaults
	if config.Provider == "" {
		config.Provider = "openrouter"
	}
	if config.Model == "" {
		config.Model = "openai/gpt-3.5-turbo" // Default model
	}
	if config.Timeout == 0 {
		config.Timeout = 30 // Default timeout in seconds
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://openrouter.ai/api/v1"
	}

	httpClient := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	return &openRouterLLM{
		config:     config,
		httpClient: httpClient,
	}
}

// Generate generates text based on a prompt
func (l *openRouterLLM) Generate(ctx context.Context, prompt string) (string, error) {
	messages := []Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	request := OpenRouterRequest{
		Model:       l.config.Model,
		Messages:    messages,
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", l.config.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.config.APIKey)
	req.Header.Set("HTTP-Referer", "https://ai-search.local")
	req.Header.Set("X-Title", "AI Search Engine")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return response.Choices[0].Message.Content, nil
}

// Rerank reranks search results based on relevance
func (l *openRouterLLM) Rerank(ctx context.Context, query string, results []string) ([]string, error) {
	if len(results) == 0 {
		return results, nil
	}

	// Create a prompt for reranking
	prompt := l.createRerankPrompt(query, results)

	// Get LLM response
	response, err := l.Generate(ctx, prompt)
	if err != nil {
		return results, fmt.Errorf("failed to get LLM response: %w", err)
	}

	// Parse the reranked results
	rerankedResults, err := l.parseRerankResponse(response, results)
	if err != nil {
		// If parsing fails, return original order
		return results, nil
	}

	return rerankedResults, nil
}

// createRerankPrompt creates a prompt for reranking search results
func (l *openRouterLLM) createRerankPrompt(query string, results []string) string {
	var builder strings.Builder

	builder.WriteString("You are a search result reranker. Given a search query and a list of search results, please rerank them by relevance to the query.\n\n")
	builder.WriteString(fmt.Sprintf("Search Query: %s\n\n", query))
	builder.WriteString("Search Results:\n")

	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, result))
	}

	builder.WriteString("\nPlease provide the reranked results in the following format:\n")
	builder.WriteString("RERANKED: [list of numbers in order of relevance, separated by commas]\n")
	builder.WriteString("For example: RERANKED: 3,1,5,2,4\n\n")
	builder.WriteString("Only respond with the RERANKED line, nothing else.")

	return builder.String()
}

// parseRerankResponse parses the LLM response to extract reranked results
func (l *openRouterLLM) parseRerankResponse(response string, originalResults []string) ([]string, error) {
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "RERANKED:") {
			// Extract the numbers
			numbersStr := strings.TrimSpace(strings.TrimPrefix(line, "RERANKED:"))
			numbers := strings.Split(numbersStr, ",")

			var rerankedResults []string
			for _, numStr := range numbers {
				numStr = strings.TrimSpace(numStr)
				var index int
				if _, err := fmt.Sscanf(numStr, "%d", &index); err != nil {
					continue
				}

				// Convert to 0-based index
				index--
				if index >= 0 && index < len(originalResults) {
					rerankedResults = append(rerankedResults, originalResults[index])
				}
			}

			// If we got valid results, return them
			if len(rerankedResults) > 0 {
				return rerankedResults, nil
			}
		}
	}

	return nil, fmt.Errorf("could not parse rerank response")
}
