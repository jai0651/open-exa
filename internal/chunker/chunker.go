package chunker

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Chunker defines the interface for text chunking
type Chunker interface {
	// Chunk splits text into overlapping chunks
	Chunk(text string) []*Chunk
}

// Chunk represents a text chunk
type Chunk struct {
	ID       string
	Text     string
	StartPos int
	EndPos   int
	Metadata map[string]interface{}
}

// Config holds chunker configuration
type Config struct {
	ChunkSize    int
	OverlapSize  int
	MinChunkSize int
}

// textChunker implements the Chunker interface
type textChunker struct {
	config Config
}

// NewTextChunker creates a new text chunker
func NewTextChunker(config Config) Chunker {
	// Set defaults
	if config.ChunkSize == 0 {
		config.ChunkSize = 1000 // Default chunk size
	}
	if config.OverlapSize == 0 {
		config.OverlapSize = 200 // Default overlap
	}
	if config.MinChunkSize == 0 {
		config.MinChunkSize = 100 // Minimum chunk size
	}

	return &textChunker{
		config: config,
	}
}

// Chunk splits text into overlapping chunks
func (c *textChunker) Chunk(text string) []*Chunk {
	if text == "" {
		return []*Chunk{}
	}

	// Clean and normalize text
	text = c.cleanText(text)

	// Split into sentences for better chunk boundaries
	sentences := c.splitIntoSentences(text)

	var chunks []*Chunk
	var currentChunk strings.Builder
	var startPos int
	chunkID := 0

	for _, sentence := range sentences {
		// Check if adding this sentence would exceed chunk size
		if currentChunk.Len()+len(sentence) > c.config.ChunkSize && currentChunk.Len() > 0 {
			// Create chunk from current content
			chunkText := strings.TrimSpace(currentChunk.String())
			if len(chunkText) >= c.config.MinChunkSize {
				chunk := c.createChunk(chunkID, chunkText, startPos, startPos+len(chunkText))
				chunks = append(chunks, chunk)
				chunkID++
			}

			// Start new chunk with overlap
			overlapText := c.getOverlapText(chunkText)
			currentChunk.Reset()
			currentChunk.WriteString(overlapText)
			startPos = c.calculateStartPos(text, overlapText)
		}

		// Add current sentence
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
	}

	// Add final chunk if it has content
	if currentChunk.Len() > 0 {
		chunkText := strings.TrimSpace(currentChunk.String())
		if len(chunkText) >= c.config.MinChunkSize {
			chunk := c.createChunk(chunkID, chunkText, startPos, startPos+len(chunkText))
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}

// cleanText cleans and normalizes text
func (c *textChunker) cleanText(text string) string {
	// Remove extra whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	// Remove control characters
	text = regexp.MustCompile(`[\x00-\x1F\x7F]`).ReplaceAllString(text, "")

	return strings.TrimSpace(text)
}

// splitIntoSentences splits text into sentences
func (c *textChunker) splitIntoSentences(text string) []string {
	// Simple sentence splitting based on punctuation
	re := regexp.MustCompile(`[.!?]+\s+`)
	sentences := re.Split(text, -1)

	var result []string
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence != "" {
			result = append(result, sentence)
		}
	}

	return result
}

// getOverlapText gets the overlap text from the end of a chunk
func (c *textChunker) getOverlapText(chunkText string) string {
	if len(chunkText) <= c.config.OverlapSize {
		return chunkText
	}

	// Find a good break point (sentence boundary)
	overlapStart := len(chunkText) - c.config.OverlapSize
	for i := overlapStart; i < len(chunkText); i++ {
		if unicode.IsSpace(rune(chunkText[i])) {
			return chunkText[i+1:]
		}
	}

	// If no good break point, just take the last overlapSize characters
	return chunkText[len(chunkText)-c.config.OverlapSize:]
}

// calculateStartPos calculates the start position of a chunk in the original text
func (c *textChunker) calculateStartPos(originalText, chunkText string) int {
	pos := strings.Index(originalText, chunkText)
	if pos == -1 {
		return 0
	}
	return pos
}

// createChunk creates a new chunk with metadata
func (c *textChunker) createChunk(id int, text string, startPos, endPos int) *Chunk {
	// Generate chunk ID
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", id, text)))
	chunkID := fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes as ID

	return &Chunk{
		ID:       chunkID,
		Text:     text,
		StartPos: startPos,
		EndPos:   endPos,
		Metadata: map[string]interface{}{
			"chunk_size": len(text),
			"chunk_id":   id,
		},
	}
}
