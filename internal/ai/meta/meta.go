package meta

import (
	"context"
	"fmt"
	"strings"

	"github.com/Vasteva/MediaConverter/internal/ai"
)

// Cleaner handles AI-powered metadata cleaning
type Cleaner struct {
	provider ai.Provider
}

// NewCleaner creates a new metadata cleaner
func NewCleaner(p ai.Provider) *Cleaner {
	return &Cleaner{provider: p}
}

// CleanFilename uses AI to parse a messy filename and return a clean title and year
func (c *Cleaner) CleanFilename(ctx context.Context, filename string) (string, error) {
	if c.provider == nil {
		return "", fmt.Errorf("AI provider not configured")
	}

	prompt := fmt.Sprintf(`
		Extract the clean movie or TV show title and the release year from this filename.
		Filename: "%s"
		
		Return ONLY the clean title and year in this format: "Title (Year)"
		If year is unknown, return ONLY the Title.
		Example Input: "The.Matrix.1999.1080p.BluRay.x264.mkv"
		Example Output: "The Matrix (1999)"
	`, filename)

	cleaned, err := c.provider.Analyze(ctx, prompt)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(cleaned), nil
}

// AnalyzeEncoding uses AI to recommend optimal encoding settings based on media info
func (c *Cleaner) AnalyzeEncoding(ctx context.Context, rawJSON string) (int, error) {
	if c.provider == nil {
		return 23, fmt.Errorf("AI provider not configured")
	}

	prompt := fmt.Sprintf(`
		Analyze this ffprobe JSON output and recommend the optimal CRF (Constant Rate Factor) 
		for H.265 encoding to balance high quality and small file size.
		
		Media Info: %s
		
		Return ONLY the recommended CRF as an integer (typically between 18 and 28).
		Example Output: 22
	`, rawJSON)

	response, err := c.provider.Analyze(ctx, prompt)
	if err != nil {
		return 23, err
	}

	// Parse the response for the integer
	var crf int
	_, err = fmt.Sscanf(strings.TrimSpace(response), "%d", &crf)
	if err != nil {
		return 23, fmt.Errorf("failed to parse AI response: %v", err)
	}

	if crf < 10 || crf > 51 {
		return 23, fmt.Errorf("AI returned invalid CRF: %d", crf)
	}

	return crf, nil
}
