# AI Search Engine

A complete AI-powered search engine built from scratch in Go that combines traditional keyword search with semantic search capabilities.

## Features

- **Web Crawling**: Polite crawler that respects robots.txt and implements rate limiting
- **Text Processing**: HTML parsing, text extraction, and intelligent chunking with overlap
- **Vector Search**: Embedding generation using OpenAI API and vector similarity search with ChromaDB
- **Hybrid Retrieval**: Combines BM25 keyword search (Elasticsearch) with semantic search
- **LLM Reranking**: Uses language models to rerank search results for better relevance
- **HTTP API**: RESTful API with web interface for searching
- **Modular Architecture**: Pluggable interfaces for different components
- **Docker Support**: All services run in Docker containers
- **PostgreSQL Database**: Robust relational database for document storage

## Architecture

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Crawler   │───▶│   Parser    │───▶│   Chunker   │
└─────────────┘    └─────────────┘    └─────────────┘
       │                   │                   │
       ▼                   ▼                   ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Store     │    │ Embeddings  │    │   Indexer   │
│(PostgreSQL) │    │  (OpenAI)   │    │(Chroma+ES)  │
└─────────────┘    └─────────────┘    └─────────────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           ▼
                  ┌─────────────┐
                  │  Retriever  │
                  │ (Hybrid)    │
                  └─────────────┘
                           │
                           ▼
                  ┌─────────────┐
                  │    LLM      │
                  │(OpenRouter) │
                  └─────────────┘
```

## Development Status: ✅ COMPLETE

### Phase 0 - Repository Setup ✅
- [x] Go module and CLI structure
- [x] Package skeletons for all components
- [x] Basic tests and CI setup

### Phase 1 - Fetcher & Parser ✅
- [x] Polite crawler with robots.txt respect
- [x] HTML parser with text extraction
- [x] URL canonicalization and deduplication
- [x] Rate limiting and concurrent workers

### Phase 2 - Embeddings & Indexing ✅
- [x] Text chunking with overlap
- [x] Embedding generation using OpenRouter API
- [x] Vector index storage with ChromaDB
- [x] BM25 keyword indexing with Elasticsearch

### Phase 3 - Retrieval & API ✅
- [x] Hybrid retriever (BM25 + semantic)
- [x] LLM reranking using OpenRouter
- [x] HTTP API server with web interface
- [x] CLI crawl and server commands

## Getting Started

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- OpenAI API key (for embeddings)
- OpenRouter API key (for LLM)
- Make (optional, for using Makefile)

### Quick Start

1. **Clone and setup**:
```bash
git clone <repository-url>
cd ai-search
cp env.example .env
```

2. **Configure environment**:
Edit `.env` file and add your API keys:
```bash
LLM_API_KEY=your_openrouter_api_key_here
EMBEDDING_API_KEY=your_openai_api_key_here
```

3. **Start services**:
```bash
# Start all required services (ChromaDB, Elasticsearch, Redis)
docker-compose up -d

# Wait for services to be ready (about 30 seconds)
```

4. **Install dependencies and build**:
```bash
make deps
make build
```

5. **Crawl and index a website**:
```bash
# Crawl a website and automatically index it
./bin/ai-search crawl --url https://example.com --depth 2
```

6. **Start the search server**:
```bash
# Start the HTTP API server
./bin/ai-search server
```

7. **Search your indexed content**:
- Open http://localhost:8080 in your browser
- Or use the API directly: `curl "http://localhost:8080/api/search?q=your+query"`

### Usage

```bash
# Show help
./bin/ai-search --help

# Crawl and index a website
./bin/ai-search crawl --url https://example.com --depth 2

# Start the search server
./bin/ai-search server

# API endpoints:
# GET  /api/search?q=query&limit=10
# POST /api/search (JSON body: {"query": "text", "limit": 10})
# GET  /api/health
# GET  / (web interface)
```

## Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Lint code
make lint
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Submit a pull request

## License

MIT License - see LICENSE file for details.
