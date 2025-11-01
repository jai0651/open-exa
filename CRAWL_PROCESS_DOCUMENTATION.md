# AI Search Crawl Process Documentation

## Command: `./bin/ai-search crawl --url https://example.com --depth 2`

This document provides a detailed breakdown of what happens when you execute the crawl command with the specified parameters.

## Overview

The crawl command initiates a web crawling process that:
1. Starts from the specified URL (https://example.com)
2. Crawls up to depth 2 (the starting page + 2 levels of links)
3. Processes each page through a complete AI search pipeline
4. Stores the results in both PostgreSQL and vector databases

## Detailed Process Flow

### 1. Command Line Processing
- **Entry Point**: `cmd/ai-search/main.go` → `cli.Execute()`
- **CLI Handler**: `internal/cli/crawl.go` → `runCrawl()`
- **URL Validation**: Parses and validates the starting URL
- **Configuration**: Loads environment variables and sets defaults

### 2. Configuration Loading
The system loads configuration from environment variables with these defaults:

```go
// Database Configuration
DatabaseType: "postgres"
DatabaseHost: "localhost"
DatabasePort: 5432
DatabaseName: "ai_search"
DatabaseUser: "postgres"
DatabasePassword: "postgres"

// Vector Database Configuration
ChromaURL: "http://localhost:8000"
ElasticURL: "http://localhost:9200"
CollectionName: "ai_search_documents"

// Embedding Configuration
EmbeddingModel: "text-embedding-3-small"
EmbeddingAPIKey: "" // REQUIRED - must be set
EmbeddingBaseURL: "https://api.openai.com/v1"

// Chunking Configuration
ChunkSize: 1000
OverlapSize: 200
MinChunkSize: 100

// Crawler Configuration
MaxWorkers: 5
RateLimit: 0.1 (requests per second)
MaxPageSize: 1048576 (1MB)
UserAgent: "ai-search/1.0"
Timeout: 30 seconds
RespectRobots: false
```

### 3. Component Initialization

#### 3.1 Database Store (PostgreSQL)
- **Purpose**: Stores document metadata and chunks
- **Schema**: Creates tables for documents and chunks
- **Connection**: Establishes connection to PostgreSQL database

#### 3.2 Text Chunker
- **Purpose**: Splits content into overlapping chunks for better search
- **Configuration**: 1000 character chunks with 200 character overlap
- **Algorithm**: Sentence-aware chunking with smart boundaries

#### 3.3 Embedding Service
- **Purpose**: Generates vector embeddings for semantic search
- **Provider**: OpenAI API (text-embedding-3-small model)
- **Dimensions**: 1536-dimensional vectors
- **Batch Processing**: Processes up to 10 texts at once

#### 3.4 Hybrid Indexer
- **Purpose**: Indexes content in both vector and keyword search systems
- **Vector DB**: ChromaDB for semantic search
- **Keyword DB**: Elasticsearch for traditional search
- **Collection**: Creates/uses "ai_search_documents" collection

#### 3.5 Web Crawler
- **Purpose**: Fetches and parses web pages
- **Workers**: 5 concurrent workers by default
- **Rate Limiting**: 0.1 requests per second per domain
- **Robots.txt**: Optional respect for robots.txt (disabled by default)

### 4. Crawling Process

#### 4.1 Initial Setup
```
Starting crawl of https://example.com (depth: 2)
Initializing components...
Starting crawl and indexing...
```

#### 4.2 Worker Pool Architecture
- **Workers**: 5 concurrent goroutines process URLs
- **URL Queue**: Channel-based queue for pending URLs
- **Visited Tracking**: Prevents duplicate crawling
- **Depth Management**: Tracks crawl depth for each URL

#### 4.3 Per-Page Processing Pipeline

For each page discovered:

##### Step 1: URL Processing
- **Validation**: Checks if URL is already visited
- **Robots Check**: Optional robots.txt compliance check
- **Rate Limiting**: Applies per-domain rate limiting
- **Depth Check**: Ensures we don't exceed max depth (2)

##### Step 2: HTTP Fetching
- **Request**: Creates HTTP GET request with proper User-Agent
- **Timeout**: 30-second timeout per request
- **Size Limit**: Maximum 1MB response size
- **Content Type**: Only processes HTML content

##### Step 3: HTML Parsing
- **Title Extraction**: Extracts page title
- **Meta Description**: Extracts meta description
- **Text Content**: Extracts clean text (removes scripts, styles)
- **Link Extraction**: Finds all links on the page
- **Content Hash**: Generates SHA256 hash for deduplication

##### Step 4: Link Processing
- **URL Normalization**: Canonicalizes URLs
- **Validation**: Filters out invalid URLs (PDFs, images, etc.)
- **Depth Assignment**: Assigns depth = current_depth + 1
- **Queue Addition**: Adds valid links to worker queue

### 5. Content Processing Pipeline

For each successfully crawled page:

#### 5.1 Document Storage
```go
Document {
    ID: content_hash,
    URL: page_url,
    Title: page_title,
    Content: extracted_text,
    Meta: {
        "meta_desc": meta_description,
        "links_count": number_of_links,
        "depth": crawl_depth,
        "content_hash": sha256_hash
    }
}
```

#### 5.2 Text Chunking
- **Input**: Full page content
- **Process**: Splits into 1000-character chunks with 200-character overlap
- **Output**: Array of chunks with metadata
- **Smart Boundaries**: Breaks at sentence boundaries when possible

#### 5.3 Embedding Generation
- **Input**: Array of chunk texts
- **Process**: Calls OpenAI API for each chunk
- **Model**: text-embedding-3-small (1536 dimensions)
- **Batch Size**: Up to 10 chunks per API call
- **Output**: Array of 1536-dimensional vectors

#### 5.4 Chunk Storage
- **Database**: Saves chunks to PostgreSQL
- **Metadata**: Stores chunk position, size, and relationships

#### 5.5 Vector Indexing
- **ChromaDB**: Stores embeddings for semantic search
- **Document ID**: Links chunks to parent document
- **Metadata**: Includes title, URL, and chunk information

#### 5.6 Keyword Indexing
- **Elasticsearch**: Stores text for keyword search
- **Full-text Search**: Enables traditional search capabilities
- **Metadata**: Includes all document and chunk metadata

### 6. Progress Reporting

The system provides real-time feedback:

```
Processing page 1: Example Domain
  Indexed 3 chunks for Example Domain
Processing page 2: About Us
  Indexed 2 chunks for About Us
...
```

### 7. Error Handling

- **Network Errors**: Logged and skipped
- **Parse Errors**: Logged and skipped
- **API Errors**: Logged and skipped
- **Database Errors**: Logged and skipped
- **Rate Limiting**: Automatic delays between requests

### 8. Completion Summary

```
Crawl completed. Processed 15 pages, indexed 12 pages, 3 errors.
```

## Configuration Requirements

### Required Environment Variables
- `EMBEDDING_API_KEY`: OpenAI API key for embeddings

### Optional Environment Variables
- `DATABASE_HOST`: PostgreSQL host (default: localhost)
- `DATABASE_PORT`: PostgreSQL port (default: 5432)
- `DATABASE_NAME`: Database name (default: ai_search)
- `DATABASE_USER`: Database user (default: postgres)
- `DATABASE_PASSWORD`: Database password (default: postgres)
- `CHROMA_URL`: ChromaDB URL (default: http://localhost:8000)
- `ELASTIC_URL`: Elasticsearch URL (default: http://localhost:9200)
- `MAX_WORKERS`: Number of concurrent workers (default: 5)
- `RATE_LIMIT`: Requests per second per domain (default: 0.1)

## Dependencies

### External Services Required
1. **PostgreSQL**: Document and chunk storage
2. **ChromaDB**: Vector database for embeddings
3. **Elasticsearch**: Keyword search index
4. **OpenAI API**: Embedding generation

### Internal Components
1. **Crawler**: Web page fetching and parsing
2. **Parser**: HTML content extraction
3. **Chunker**: Text segmentation
4. **Embedder**: Vector generation
5. **Indexer**: Dual indexing system
6. **Store**: Database operations

## Performance Characteristics

- **Concurrency**: 5 workers processing URLs simultaneously
- **Rate Limiting**: 0.1 requests/second per domain (10-second intervals)
- **Memory Usage**: Buffered channels for URL queue (1000 URLs)
- **Timeout**: 30 seconds per HTTP request
- **Batch Processing**: Up to 10 embeddings per API call

## Example Output

```
Starting crawl of https://example.com (depth: 2)
Initializing components...
Starting crawl and indexing...
Processing page 1: Example Domain
  Indexed 3 chunks for Example Domain
Processing page 2: About Us
  Indexed 2 chunks for About Us
Processing page 3: Contact
  Indexed 1 chunks for Contact
Processing page 4: Services
  Indexed 4 chunks for Services
Processing page 5: Products
  Indexed 3 chunks for Products

Crawl completed. Processed 5 pages, indexed 5 pages, 0 errors.
```

This process creates a comprehensive search index that supports both semantic (vector) and keyword search capabilities, making the crawled content searchable through the AI search system.
