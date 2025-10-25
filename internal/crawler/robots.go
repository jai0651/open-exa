package crawler

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Robots represents a robots.txt file
type Robots struct {
	UserAgent  string
	Disallow   []string
	CrawlDelay time.Duration
}

// RobotsCache caches robots.txt files per domain
type RobotsCache struct {
	cache map[string]*Robots
	mutex sync.RWMutex
}

// NewRobotsCache creates a new robots cache
func NewRobotsCache() *RobotsCache {
	return &RobotsCache{
		cache: make(map[string]*Robots),
	}
}

// GetRobots retrieves robots.txt for a domain
func (rc *RobotsCache) GetRobots(client *http.Client, domain string, userAgent string) (*Robots, error) {
	rc.mutex.RLock()
	if robots, exists := rc.cache[domain]; exists {
		rc.mutex.RUnlock()
		return robots, nil
	}
	rc.mutex.RUnlock()

	// Fetch robots.txt
	robotsURL := fmt.Sprintf("https://%s/robots.txt", domain)
	resp, err := client.Get(robotsURL)
	if err != nil {
		// If robots.txt is not accessible, allow crawling
		return &Robots{
			UserAgent:  userAgent,
			Disallow:   []string{},
			CrawlDelay: 0,
		}, nil
	}
	defer resp.Body.Close()

	// Parse robots.txt
	robots, err := parseRobotsTxt(resp.Body, userAgent)
	if err != nil {
		// If parsing fails, allow crawling
		return &Robots{
			UserAgent:  userAgent,
			Disallow:   []string{},
			CrawlDelay: 0,
		}, nil
	}

	// Cache the result
	rc.mutex.Lock()
	rc.cache[domain] = robots
	rc.mutex.Unlock()

	return robots, nil
}

// parseRobotsTxt parses a robots.txt file
func parseRobotsTxt(body io.Reader, userAgent string) (*Robots, error) {
	robots := &Robots{
		UserAgent:  userAgent,
		Disallow:   []string{},
		CrawlDelay: 0,
	}

	scanner := bufio.NewScanner(body)
	var currentUserAgent string
	var inUserAgentSection bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse User-agent directive
		if strings.HasPrefix(strings.ToLower(line), "user-agent:") {
			currentUserAgent = strings.TrimSpace(line[11:])
			inUserAgentSection = (currentUserAgent == "*" || currentUserAgent == userAgent)
			continue
		}

		// Only process directives for our user agent
		if !inUserAgentSection {
			continue
		}

		// Parse Disallow directive
		if strings.HasPrefix(strings.ToLower(line), "disallow:") {
			path := strings.TrimSpace(line[9:])
			if path != "" {
				robots.Disallow = append(robots.Disallow, path)
			}
			continue
		}

		// Parse Crawl-delay directive
		if strings.HasPrefix(strings.ToLower(line), "crawl-delay:") {
			delayStr := strings.TrimSpace(line[12:])
			if delay, err := strconv.Atoi(delayStr); err == nil {
				robots.CrawlDelay = time.Duration(delay) * time.Second
			}
			continue
		}
	}

	return robots, scanner.Err()
}

// CanCrawl checks if a URL can be crawled according to robots.txt
func (r *Robots) CanCrawl(urlPath string) bool {
	for _, disallowPath := range r.Disallow {
		if strings.HasPrefix(urlPath, disallowPath) {
			return false
		}
	}
	return true
}

// GetCrawlDelay returns the crawl delay for this robots.txt
func (r *Robots) GetCrawlDelay() time.Duration {
	return r.CrawlDelay
}
