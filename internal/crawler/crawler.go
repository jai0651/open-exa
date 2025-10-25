package crawler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ai-search/internal/parser"

	"github.com/sirupsen/logrus"
)

// Crawler defines the interface for web crawling functionality
type Crawler interface {
	// Crawl starts crawling from the given URL with specified depth
	Crawl(ctx context.Context, startURL *url.URL, maxDepth int) (<-chan *Page, <-chan error)

	// SetRateLimit sets the rate limit for crawling (requests per second)
	SetRateLimit(rate float64)

	// SetMaxWorkers sets the maximum number of concurrent workers
	SetMaxWorkers(workers int)
}

// Page represents a crawled web page
type Page struct {
	URL         *url.URL
	Title       string
	Content     string
	MetaDesc    string
	Links       []*url.URL
	ContentHash string
	Depth       int
}

// urlWithDepth represents a URL with its crawl depth
type urlWithDepth struct {
	url   *url.URL
	depth int
}

// Config holds crawler configuration
type Config struct {
	MaxWorkers    int
	RateLimit     float64
	MaxPageSize   int64
	UserAgent     string
	Timeout       int
	RespectRobots bool
}

// crawler implements the Crawler interface
type crawler struct {
	config       Config
	client       *http.Client
	robotsCache  *RobotsCache
	rateLimiters map[string]*time.Ticker
	rateMutex    sync.RWMutex
	parser       parser.Parser
	normalizer   parser.URLNormalizer
	logger       *logrus.Logger
}

// NewCrawler creates a new crawler instance
func NewCrawler(config Config) Crawler {
	if config.UserAgent == "" {
		config.UserAgent = "ai-search/1.0"
	}
	if config.MaxWorkers == 0 {
		config.MaxWorkers = 10
	}
	if config.RateLimit == 0 {
		config.RateLimit = 1.0
	}
	if config.MaxPageSize == 0 {
		config.MaxPageSize = 1024 * 1024 // 1MB
	}
	if config.Timeout == 0 {
		config.Timeout = 30
	}

	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
	})

	return &crawler{
		config:       config,
		client:       client,
		robotsCache:  NewRobotsCache(),
		rateLimiters: make(map[string]*time.Ticker),
		parser:       parser.NewHTMLParser(),
		normalizer:   parser.NewURLNormalizer(),
		logger:       logger,
	}
}

// Crawl starts crawling from the given URL with specified depth
func (c *crawler) Crawl(ctx context.Context, startURL *url.URL, maxDepth int) (<-chan *Page, <-chan error) {
	pageChan := make(chan *Page, 100)
	errorChan := make(chan error, 100)

	go func() {
		defer close(pageChan)
		defer close(errorChan)

		visited := make(map[string]bool)
		visitedMutex := sync.RWMutex{}

		// Worker pool
		urlChan := make(chan urlWithDepth, 1000)
		var wg sync.WaitGroup

		// Start workers
		fmt.Printf("DEBUG: Starting %d workers\n", c.config.MaxWorkers)
		for i := 0; i < c.config.MaxWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				fmt.Printf("DEBUG: Worker %d starting\n", workerID)
				c.worker(ctx, urlChan, pageChan, errorChan, visited, &visitedMutex, maxDepth)
				fmt.Printf("DEBUG: Worker %d finished\n", workerID)
			}(i)
		}

		// Start with the initial URL at depth 0
		fmt.Printf("DEBUG: Starting crawl with URL: %s\n", startURL.String())
		urlChan <- urlWithDepth{url: startURL, depth: 0}

		// Wait for workers to finish processing
		wg.Wait()
		close(urlChan)
	}()

	return pageChan, errorChan
}

// worker processes URLs from the queue
func (c *crawler) worker(ctx context.Context, urlChan chan urlWithDepth, pageChan chan<- *Page, errorChan chan<- error, visited map[string]bool, visitedMutex *sync.RWMutex, maxDepth int) {
	fmt.Printf("DEBUG: Worker started\n")
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("DEBUG: Worker context done\n")
			return
		case urlData, ok := <-urlChan:
			if !ok {
				fmt.Printf("DEBUG: URL channel closed\n")
				return
			}
			fmt.Printf("DEBUG: Worker received URL from channel\n")

			url := urlData.url
			depth := urlData.depth

			// Check if already visited
			urlStr := url.String()
			visitedMutex.RLock()
			if visited[urlStr] {
				visitedMutex.RUnlock()
				c.logger.Debugf("Already visited: %s", urlStr)
				continue
			}
			visitedMutex.RUnlock()

			// Mark as visited
			visitedMutex.Lock()
			visited[urlStr] = true
			visitedMutex.Unlock()

			fmt.Printf("DEBUG: Processing URL: %s (depth: %d)\n", urlStr, depth)
			c.logger.Infof("Processing URL: %s (depth: %d)", urlStr, depth)

			// Check robots.txt
			if c.config.RespectRobots && !c.canCrawl(url) {
				fmt.Printf("DEBUG: Robots.txt disallows crawling: %s\n", urlStr)
				c.logger.Debugf("Robots.txt disallows crawling: %s", urlStr)
				continue
			}

			// Rate limiting
			fmt.Printf("DEBUG: Applying rate limit for: %s\n", urlStr)
			c.rateLimit(url)

			// Fetch and parse the page
			fmt.Printf("DEBUG: About to fetch and parse: %s\n", urlStr)
			page, err := c.fetchAndParse(ctx, url)
			if err != nil {
				fmt.Printf("DEBUG: Failed to fetch %s: %v\n", urlStr, err)
				c.logger.Errorf("Failed to fetch %s: %v", urlStr, err)
				errorChan <- fmt.Errorf("failed to fetch %s: %w", urlStr, err)
				continue
			}
			fmt.Printf("DEBUG: Successfully fetched and parsed: %s\n", urlStr)

			// Set the correct depth
			page.Depth = depth
			fmt.Printf("DEBUG: Sending page to channel: %s\n", page.Title)
			pageChan <- page

			// Add new URLs to queue if within depth limit
			if depth < maxDepth {
				for _, link := range page.Links {
					select {
					case urlChan <- urlWithDepth{url: link, depth: depth + 1}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}
}

// fetchAndParse fetches a URL and parses its content
func (c *crawler) fetchAndParse(ctx context.Context, targetURL *url.URL) (*Page, error) {
	fmt.Printf("DEBUG: Fetching URL: %s\n", targetURL.String())
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		fmt.Printf("DEBUG: HTTP request failed: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Printf("DEBUG: HTTP response status: %d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	// Limit response size
	limitedReader := io.LimitReader(resp.Body, c.config.MaxPageSize)

	// Parse the HTML
	parsed, err := c.parser.ParseHTML(limitedReader, targetURL)
	if err != nil {
		return nil, err
	}

	// Calculate content hash
	hash := sha256.Sum256([]byte(parsed.Text))
	contentHash := fmt.Sprintf("%x", hash)

	// Normalize links
	var normalizedLinks []*url.URL
	for _, link := range parsed.Links {
		if normalized, err := c.normalizer.Normalize(link.String(), targetURL); err == nil && c.normalizer.IsValid(normalized) {
			normalizedLinks = append(normalizedLinks, normalized)
		}
	}

	return &Page{
		URL:         targetURL,
		Title:       parsed.Title,
		Content:     parsed.Text,
		MetaDesc:    parsed.MetaDesc,
		Links:       normalizedLinks,
		ContentHash: contentHash,
		Depth:       0, // Will be set by the worker
	}, nil
}

// canCrawl checks if the URL can be crawled according to robots.txt
func (c *crawler) canCrawl(url *url.URL) bool {
	robots, err := c.robotsCache.GetRobots(c.client, url.Host, c.config.UserAgent)
	if err != nil {
		c.logger.Debugf("Failed to get robots.txt for %s: %v", url.Host, err)
		return true // Allow crawling if robots.txt is not accessible
	}

	return robots.CanCrawl(url.Path)
}

// rateLimit implements rate limiting per domain
func (c *crawler) rateLimit(url *url.URL) {
	// Skip rate limiting if rate limit is 0 or negative
	if c.config.RateLimit <= 0 {
		return
	}

	domain := url.Host

	c.rateMutex.RLock()
	ticker, exists := c.rateLimiters[domain]
	c.rateMutex.RUnlock()

	if !exists {
		interval := time.Duration(1.0/c.config.RateLimit) * time.Second
		if interval <= 0 {
			interval = time.Second // Default to 1 second if rate limit is too high
		}
		fmt.Printf("DEBUG: Creating new ticker for domain %s with interval %v\n", domain, interval)
		ticker = time.NewTicker(interval)

		c.rateMutex.Lock()
		c.rateLimiters[domain] = ticker
		c.rateMutex.Unlock()
	}

	fmt.Printf("DEBUG: Waiting for ticker for domain %s\n", domain)
	select {
	case <-ticker.C:
		fmt.Printf("DEBUG: Ticker received for domain %s\n", domain)
	case <-time.After(5 * time.Second):
		fmt.Printf("DEBUG: Rate limit timeout for domain %s\n", domain)
	}
}

// SetRateLimit sets the rate limit for crawling (requests per second)
func (c *crawler) SetRateLimit(rate float64) {
	c.config.RateLimit = rate
	// Clear existing rate limiters to use new rate
	c.rateMutex.Lock()
	for _, ticker := range c.rateLimiters {
		ticker.Stop()
	}
	c.rateLimiters = make(map[string]*time.Ticker)
	c.rateMutex.Unlock()
}

// SetMaxWorkers sets the maximum number of concurrent workers
func (c *crawler) SetMaxWorkers(workers int) {
	c.config.MaxWorkers = workers
}
