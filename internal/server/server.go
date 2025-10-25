package server

import (
	"ai-search/internal/retriever"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// Server defines the interface for the HTTP API server
type Server interface {
	// Start starts the HTTP server
	Start(ctx context.Context) error

	// Stop stops the HTTP server
	Stop(ctx context.Context) error

	// RegisterRoutes registers API routes
	RegisterRoutes()
}

// Config holds server configuration
type Config struct {
	Host      string
	Port      int
	Retriever retriever.Retriever
}

// httpServer implements the Server interface
type httpServer struct {
	config    Config
	server    *http.Server
	retriever retriever.Retriever
}

// SearchRequest represents a search request
type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// SearchResponse represents a search response
type SearchResponse struct {
	Query   string                  `json:"query"`
	Results []*SearchResultResponse `json:"results"`
	Total   int                     `json:"total"`
	Time    int64                   `json:"time_ms"`
}

// SearchResultResponse represents a search result in the API response
type SearchResultResponse struct {
	DocumentID string                 `json:"document_id"`
	ChunkID    string                 `json:"chunk_id"`
	Score      float32                `json:"score"`
	Text       string                 `json:"text"`
	Title      string                 `json:"title,omitempty"`
	URL        string                 `json:"url,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

// NewServer creates a new HTTP server instance
func NewServer(config Config) Server {
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 8080
	}

	return &httpServer{
		config:    config,
		retriever: config.Retriever,
	}
}

// Start starts the HTTP server
func (s *httpServer) Start(ctx context.Context) error {
	s.RegisterRoutes()

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler:      nil, // Use default mux
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on %s", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	return s.Stop(ctx)
}

// Stop stops the HTTP server
func (s *httpServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// RegisterRoutes registers API routes
func (s *httpServer) RegisterRoutes() {
	http.HandleFunc("/api/search", s.handleSearch)
	http.HandleFunc("/api/health", s.handleHealth)
	http.HandleFunc("/", s.handleRoot)
}

// handleSearch handles search requests
func (s *httpServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow GET and POST
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req SearchRequest
	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	} else {
		// GET request - parse query parameters
		req.Query = r.URL.Query().Get("q")
		if req.Query == "" {
			http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
			return
		}

		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if limit, err := strconv.Atoi(limitStr); err == nil {
				req.Limit = limit
			}
		}
	}

	// Set defaults
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Limit > 100 {
		req.Limit = 100 // Cap at 100 results
	}

	// Perform search
	results, err := s.retriever.Retrieve(r.Context(), req.Query, req.Limit)
	if err != nil {
		log.Printf("Search error: %v", err)
		http.Error(w, "Search failed", http.StatusInternalServerError)
		return
	}

	// Convert results to response format
	var responseResults []*SearchResultResponse
	for _, result := range results {
		responseResult := &SearchResultResponse{
			DocumentID: result.DocumentID,
			ChunkID:    result.ChunkID,
			Score:      result.Score,
			Text:       result.Text,
			Metadata:   result.Metadata,
		}

		// Extract title and URL from metadata if available
		if title, ok := result.Metadata["title"].(string); ok {
			responseResult.Title = title
		}
		if url, ok := result.Metadata["url"].(string); ok {
			responseResult.URL = url
		}

		responseResults = append(responseResults, responseResult)
	}

	// Create response
	response := SearchResponse{
		Query:   req.Query,
		Results: responseResults,
		Total:   len(responseResults),
		Time:    time.Since(startTime).Milliseconds(),
	}

	// Set content type and encode response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleHealth handles health check requests
func (s *httpServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRoot handles root requests
func (s *httpServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>AI Search Engine</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .container { max-width: 800px; margin: 0 auto; }
        .search-box { width: 100%; padding: 10px; font-size: 16px; margin: 20px 0; }
        .search-btn { padding: 10px 20px; font-size: 16px; background: #007bff; color: white; border: none; cursor: pointer; }
        .result { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        .result-title { font-weight: bold; color: #007bff; }
        .result-text { margin: 10px 0; }
        .result-score { color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>AI Search Engine</h1>
        <p>Search through indexed documents using semantic and keyword search.</p>
        
        <form id="searchForm">
            <input type="text" id="query" class="search-box" placeholder="Enter your search query..." required>
            <button type="submit" class="search-btn">Search</button>
        </form>
        
        <div id="results"></div>
    </div>

    <script>
        document.getElementById('searchForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const query = document.getElementById('query').value;
            const resultsDiv = document.getElementById('results');
            
            resultsDiv.innerHTML = '<p>Searching...</p>';
            
            try {
                const response = await fetch('/api/search?q=' + encodeURIComponent(query));
                const data = await response.json();
                
                if (data.results && data.results.length > 0) {
                    let html = '<h2>Search Results (' + data.total + ')</h2>';
                    data.results.forEach(result => {
                        html += '<div class="result">';
                        html += '<div class="result-title">' + (result.title || 'Untitled') + '</div>';
                        html += '<div class="result-text">' + result.text + '</div>';
                        html += '<div class="result-score">Score: ' + result.score.toFixed(3) + '</div>';
                        if (result.url) {
                            html += '<div><a href="' + result.url + '" target="_blank">' + result.url + '</a></div>';
                        }
                        html += '</div>';
                    });
                    resultsDiv.innerHTML = html;
                } else {
                    resultsDiv.innerHTML = '<p>No results found.</p>';
                }
            } catch (error) {
                resultsDiv.innerHTML = '<p>Error: ' + error.message + '</p>';
            }
        });
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}
