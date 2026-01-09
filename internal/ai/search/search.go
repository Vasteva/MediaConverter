package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/rwurtz/vastiva/internal/ai"
)

// Searcher handles AI-powered natural language search
type Searcher struct {
	provider ai.Provider
}

// NewSearcher creates a new searcher
func NewSearcher(p ai.Provider) *Searcher {
	return &Searcher{provider: p}
}

// MediaItem represents a searchable item
type MediaItem struct {
	ID    string
	Title string
	Path  string
}

// Match queries the media library using natural language
func (s *Searcher) Match(ctx context.Context, query string, items []MediaItem) ([]string, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("AI provider not configured")
	}

	if len(items) == 0 {
		return []string{}, nil
	}

	// Build a context string of available media
	var libraryBuilder strings.Builder
	for _, item := range items {
		libraryBuilder.WriteString(fmt.Sprintf("- ID: %s, Title: %s\n", item.ID, item.Title))
	}

	prompt := fmt.Sprintf(`
		You are a media discovery assistant. A user is searching for media with the query: "%s"
		
		Here is the media library:
		%s
		
		Rank the media items by relevance to the query. 
		Return ONLY a comma-separated list of the matching IDs in order of relevance.
		If no items match, return "NONE".
		
		Example Output: 20240101-abc, 20240102-def
	`, query, libraryBuilder.String())

	response, err := s.provider.Analyze(ctx, prompt)
	if err != nil {
		return nil, err
	}

	cleanResp := strings.TrimSpace(response)
	if cleanResp == "NONE" || cleanResp == "" {
		return []string{}, nil
	}

	// Parse IDs
	parts := strings.Split(cleanResp, ",")
	var result []string
	for _, p := range parts {
		id := strings.TrimSpace(p)
		if id != "" {
			result = append(result, id)
		}
	}

	return result, nil
}
