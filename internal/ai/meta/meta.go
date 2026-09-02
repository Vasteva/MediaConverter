package meta

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/Vasteva/MediaConverter/internal/ai"
	"github.com/Vasteva/MediaConverter/internal/util"
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

	// The example output is deliberately unquoted. An earlier version wrapped it
	// in quotation marks, and models copied the punctuation faithfully — which
	// is how files named "Batman.Ninja (2018)".mkv, quotation marks included,
	// ended up on disk.
	prompt := fmt.Sprintf(`Extract the clean movie or TV show title and the release year from this filename.

Filename: %s

Respond with the title and year only, on a single line, formatted as: Title (Year)
Do not use quotation marks, code fences, or any explanation.
If the year is unknown, respond with the title alone.

Example input: The.Matrix.1999.1080p.BluRay.x264.mkv
Example output: The Matrix (1999)`, filename)

	cleaned, err := c.provider.Analyze(ctx, prompt)
	if err != nil {
		return "", err
	}

	title, err := ExtractTitle(cleaned)
	if err != nil {
		return "", fmt.Errorf("unusable AI response %q: %w", truncateForError(cleaned), err)
	}
	return title, nil
}

// maxTitleRunes rejects responses long enough to be prose rather than a title.
// The longest legitimate film titles run to roughly 100 characters.
const maxTitleRunes = 120

// quoteRunes are stripped from both ends of a model response. Models wrap
// output in these unprompted, and several are illegal in filenames anyway.
const quoteRunes = "\"'`“”‘’«»"

// codeFence matches a leading or trailing markdown fence, with or without a
// language tag.
var codeFence = regexp.MustCompile("(?m)^\\s*```[a-zA-Z0-9]*\\s*$")

// ExtractTitle turns a raw model response into a filesystem-safe base name.
//
// Model output is untrusted input. Beyond the obvious formatting noise — code
// fences, wrapping quotes, a trailing paragraph of explanation — a response can
// contain path separators, and the caller joins the result to a directory. Path
// components are neutralised rather than interpreted, so nothing here can
// escape the destination directory.
//
// Returns an error when nothing plausible survives, so the caller can keep the
// original filename instead of writing something nonsensical.
func ExtractTitle(raw string) (string, error) {
	s := codeFence.ReplaceAllString(raw, "")

	// Take the first non-empty line. Models often append an explanation, and a
	// newline in a filename is legal on Linux but pathological.
	s = firstNonEmptyLine(s)
	if s == "" {
		return "", fmt.Errorf("response was empty")
	}

	s = strings.Trim(s, quoteRunes+" \t")

	if len([]rune(s)) > maxTitleRunes {
		return "", fmt.Errorf("response was %d characters, expected a title", len([]rune(s)))
	}

	title := util.SanitizeFilename(s)
	if title == "" {
		return "", fmt.Errorf("nothing usable remained after sanitisation")
	}

	// Sanitisation replaces illegal characters rather than dropping them, so a
	// response of "///???" survives as "______" — non-empty, and useless. A real
	// title has at least one letter or digit in it.
	if !hasAlphanumeric(title) {
		return "", fmt.Errorf("no alphanumeric content in %q", title)
	}
	return title, nil
}

func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func truncateForError(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "..."
	}
	return s
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
