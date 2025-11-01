package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	ServerHost string
	ServerPort int

	// Database configuration
	DatabaseType     string
	DatabaseHost     string
	DatabasePort     int
	DatabaseName     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseSSLMode  string

	// Vector database configuration
	ChromaURL      string
	ElasticURL     string
	CollectionName string

	// LLM configuration
	LLMProvider     string
	LLMModel        string
	LLMAPIKey       string
	LLMBaseURL      string
	EnableReranking bool

	// Embedding configuration
	EmbeddingModel   string
	EmbeddingAPIKey  string
	EmbeddingBaseURL string

	// Chunking configuration
	ChunkSize    int
	OverlapSize  int
	MinChunkSize int

	// Crawler configuration
	MaxWorkers    int
	RateLimit     float64
	MaxPageSize   int64
	UserAgent     string
	Timeout       int
	RespectRobots bool
}

// LoadConfig loads configuration from environment variables with defaults
func LoadConfig() *Config {
	// Try to load .env file from current directory first
	if err := godotenv.Load(); err != nil {
		// Try to load from workspace root (common when debugging or running from subdirectories)
		if wd, err2 := os.Getwd(); err2 == nil {
			var envPath string
			// If we're in a subdirectory, try going up to find .env
			if strings.Contains(wd, "/cmd/") {
				parts := strings.Split(wd, "/cmd/")
				envPath = parts[0] + "/.env"
			} else {
				// Try parent directory
				envPath = wd + "/.env"
				// If not found, try parent's parent
				if _, err := os.Stat(envPath); os.IsNotExist(err) {
					parentDir := strings.TrimSuffix(wd, "/"+strings.Split(wd, "/")[len(strings.Split(wd, "/"))-1])
					envPath = parentDir + "/.env"
				}
			}
			if err := godotenv.Load(envPath); err == nil {
				log.Printf("Loaded .env from %s", envPath)
			} else {
				log.Println("No .env file found, using system environment variables")
			}
		} else {
			log.Println("No .env file found, using system environment variables")
		}
	}
	config := &Config{
		// Server defaults
		ServerHost: getEnv("SERVER_HOST", "localhost"),
		ServerPort: getEnvInt("SERVER_PORT", 8080),

		// Database defaults
		DatabaseType:     getEnv("DATABASE_TYPE", "postgres"),
		DatabaseHost:     getEnv("DATABASE_HOST", "localhost"),
		DatabasePort:     getEnvInt("DATABASE_PORT", 5432),
		DatabaseName:     getEnv("DATABASE_NAME", "ai_search"),
		DatabaseUser:     getEnv("DATABASE_USER", "postgres"),
		DatabasePassword: getEnv("DATABASE_PASSWORD", "postgres"),
		DatabaseSSLMode:  getEnv("DATABASE_SSL_MODE", "disable"),

		// Vector database defaults
		ChromaURL:      getEnv("CHROMA_URL", "http://localhost:8000"),
		ElasticURL:     getEnv("ELASTIC_URL", "http://localhost:9200"),
		CollectionName: getEnv("COLLECTION_NAME", "ai_search_documents"),

		// LLM defaults
		LLMProvider:     getEnv("LLM_PROVIDER", "openrouter"),
		LLMModel:        getEnv("LLM_MODEL", "openai/gpt-3.5-turbo"),
		LLMAPIKey:       getEnv("LLM_API_KEY", ""),
		LLMBaseURL:      getEnv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
		EnableReranking: getEnvBool("ENABLE_RERANKING", false),

		// Embedding defaults (OpenAI)
		EmbeddingModel:   getEnv("EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingAPIKey:  getEnv("EMBEDDING_API_KEY", ""),
		EmbeddingBaseURL: getEnv("EMBEDDING_BASE_URL", "https://api.openai.com/v1"),

		// Chunking defaults
		ChunkSize:    getEnvInt("CHUNK_SIZE", 1000),
		OverlapSize:  getEnvInt("OVERLAP_SIZE", 200),
		MinChunkSize: getEnvInt("MIN_CHUNK_SIZE", 100),

		// Crawler defaults
		MaxWorkers:    getEnvInt("MAX_WORKERS", 5),
		RateLimit:     getEnvFloat("RATE_LIMIT", 0.1),
		MaxPageSize:   int64(getEnvInt("MAX_PAGE_SIZE", 1024*1024)),
		UserAgent:     getEnv("USER_AGENT", "ai-search/1.0"),
		Timeout:       getEnvInt("TIMEOUT", 30),
		RespectRobots: getEnvBool("RESPECT_ROBOTS", false),
	}

	return config
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an environment variable as an integer with a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvFloat gets an environment variable as a float with a default value
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

// getEnvBool gets an environment variable as a boolean with a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
