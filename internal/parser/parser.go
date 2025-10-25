package parser

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Parser defines the interface for parsing web content
type Parser interface {
	// ParseHTML parses HTML content and extracts structured data
	ParseHTML(content io.Reader, baseURL *url.URL) (*ParsedContent, error)

	// ParseText extracts readable text from HTML
	ParseText(content io.Reader) (string, error)
}

// ParsedContent represents parsed web content
type ParsedContent struct {
	Title       string
	Text        string
	MetaDesc    string
	Links       []*url.URL
	ContentHash string
}

// URLNormalizer handles URL canonicalization
type URLNormalizer interface {
	// Normalize canonicalizes a URL
	Normalize(rawURL string, baseURL *url.URL) (*url.URL, error)

	// IsValid checks if a URL is valid for crawling
	IsValid(url *url.URL) bool
}

// htmlParser implements the Parser interface
type htmlParser struct{}

// urlNormalizer implements the URLNormalizer interface
type urlNormalizer struct{}

// NewHTMLParser creates a new HTML parser
func NewHTMLParser() Parser {
	return &htmlParser{}
}

// NewURLNormalizer creates a new URL normalizer
func NewURLNormalizer() URLNormalizer {
	return &urlNormalizer{}
}

// ParseHTML parses HTML content and extracts structured data
func (p *htmlParser) ParseHTML(content io.Reader, baseURL *url.URL) (*ParsedContent, error) {
	doc, err := html.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	parsed := &ParsedContent{
		Text:  "",
		Links: []*url.URL{},
	}

	// Extract title, meta description, text, and links
	p.extractData(doc, parsed, baseURL)

	// Calculate content hash
	hash := sha256.Sum256([]byte(parsed.Text))
	parsed.ContentHash = fmt.Sprintf("%x", hash)

	return parsed, nil
}

// ParseText extracts readable text from HTML
func (p *htmlParser) ParseText(content io.Reader) (string, error) {
	doc, err := html.Parse(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	var text strings.Builder
	p.extractText(doc, &text)
	return strings.TrimSpace(text.String()), nil
}

// extractData extracts title, meta description, text, and links from HTML node
func (p *htmlParser) extractData(n *html.Node, parsed *ParsedContent, baseURL *url.URL) {
	if n.Type == html.ElementNode {
		// Skip script and style elements
		if n.Data == "script" || n.Data == "style" {
			return
		}

		switch n.Data {
		case "title":
			if n.FirstChild != nil {
				parsed.Title = strings.TrimSpace(n.FirstChild.Data)
			}
		case "meta":
			p.extractMeta(n, parsed)
		case "a":
			p.extractLink(n, parsed, baseURL)
		}
	} else if n.Type == html.TextNode {
		// Extract text content
		content := strings.TrimSpace(n.Data)
		if content != "" {
			parsed.Text += content + " "
		}
	}

	// Recursively process child nodes
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		p.extractData(c, parsed, baseURL)
	}
}

// extractMeta extracts meta tags
func (p *htmlParser) extractMeta(n *html.Node, parsed *ParsedContent) {
	var name, content string
	for _, attr := range n.Attr {
		switch attr.Key {
		case "name":
			name = attr.Val
		case "content":
			content = attr.Val
		}
	}

	if name == "description" && content != "" {
		parsed.MetaDesc = content
	}
}

// extractLink extracts links from anchor tags
func (p *htmlParser) extractLink(n *html.Node, parsed *ParsedContent, baseURL *url.URL) {
	var href string
	for _, attr := range n.Attr {
		if attr.Key == "href" {
			href = attr.Val
			break
		}
	}

	if href != "" {
		if linkURL, err := url.Parse(href); err == nil {
			if resolvedURL := baseURL.ResolveReference(linkURL); resolvedURL != nil {
				parsed.Links = append(parsed.Links, resolvedURL)
			}
		}
	}
}

// extractText extracts readable text from HTML node
func (p *htmlParser) extractText(n *html.Node, text *strings.Builder) {
	if n.Type == html.TextNode {
		content := strings.TrimSpace(n.Data)
		if content != "" {
			text.WriteString(content)
			text.WriteString(" ")
		}
	}

	// Skip script and style elements
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
		return
	}

	// Recursively process child nodes
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		p.extractText(c, text)
	}
}

// Normalize canonicalizes a URL
func (n *urlNormalizer) Normalize(rawURL string, baseURL *url.URL) (*url.URL, error) {
	// Parse the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	// Resolve relative URLs
	if baseURL != nil {
		u = baseURL.ResolveReference(u)
	}

	// Normalize scheme (prefer HTTPS)
	if u.Scheme == "http" {
		u.Scheme = "https"
	}

	// Normalize host (lowercase)
	u.Host = strings.ToLower(u.Host)

	// Remove default ports
	if u.Scheme == "https" && u.Port() == "443" {
		u.Host = u.Hostname()
	} else if u.Scheme == "http" && u.Port() == "80" {
		u.Host = u.Hostname()
	}

	// Normalize path
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "" {
		u.Path = "/"
	}

	// Remove fragment
	u.Fragment = ""

	// Sort query parameters
	u.RawQuery = u.Query().Encode()

	return u, nil
}

// IsValid checks if a URL is valid for crawling
func (n *urlNormalizer) IsValid(u *url.URL) bool {
	// Must have a scheme
	if u.Scheme == "" {
		return false
	}

	// Only allow HTTP and HTTPS
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Must have a host
	if u.Host == "" {
		return false
	}

	// Skip common non-content file extensions
	ext := strings.ToLower(u.Path)
	skipExtensions := []string{".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".zip", ".rar", ".tar", ".gz", ".jpg", ".jpeg", ".png", ".gif", ".svg", ".ico", ".css", ".js", ".xml", ".json"}
	for _, skipExt := range skipExtensions {
		if strings.HasSuffix(ext, skipExt) {
			return false
		}
	}

	// Skip common non-content paths
	skipPaths := []string{"/admin", "/login", "/logout", "/api/", "/static/", "/assets/", "/images/", "/css/", "/js/"}
	for _, skipPath := range skipPaths {
		if strings.HasPrefix(u.Path, skipPath) {
			return false
		}
	}

	// Skip URLs with certain query parameters
	query := u.Query()
	if query.Get("download") != "" || query.Get("attachment") != "" || query.Get("format") == "pdf" {
		return false
	}

	return true
}
