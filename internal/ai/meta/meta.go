package meta

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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

// CRF bounds for an AI suggestion. Wider than the 18–28 the prompt asks for, so
// a defensible answer outside that band is still accepted, but narrow enough to
// reject a value that would produce something unwatchable or enormous.
const (
	MinSuggestedCRF = 14
	MaxSuggestedCRF = 34
)

// AnalyzeEncoding asks the AI provider to recommend a CRF for the given source.
//
// summary should be a short description of the source — see
// media.MediaInfo.EncodingSummary. Passing raw ffprobe JSON here was the
// previous behaviour and is counterproductive: a UHD REMUX probe runs to tens
// of kilobytes, and burying four relevant facts in it makes a small local model
// markedly worse at the task.
//
// Returns 0 and an error when no usable value could be obtained; the caller is
// expected to fall back to its configured default.
func (c *Cleaner) AnalyzeEncoding(ctx context.Context, summary string) (int, error) {
	if c.provider == nil {
		return 0, fmt.Errorf("AI provider not configured")
	}

	prompt := fmt.Sprintf(`Recommend a CRF (Constant Rate Factor) for re-encoding this video to H.265, balancing visual quality against file size.

%s

Guidance: lower CRF means higher quality and a larger file. Grainy or highly detailed sources need a lower CRF to avoid smearing; clean digital sources tolerate a higher one. Typical values run from 18 to 28.

Respond with a single integer and nothing else. No explanation, no units, no punctuation.

Example response: 22`, summary)

	response, err := c.provider.Analyze(ctx, prompt)
	if err != nil {
		return 0, err
	}

	crf, err := ExtractCRF(response)
	if err != nil {
		return 0, err
	}
	return crf, nil
}

// firstInteger matches the first run of digits anywhere in a string, optionally
// preceded by a minus sign so a negative answer is detected and rejected rather
// than silently read as positive.
var firstInteger = regexp.MustCompile(`-?\d+`)

// ExtractCRF pulls a CRF value out of a model response.
//
// The previous implementation used fmt.Sscanf(response, "%d", &crf), which
// requires the response to *begin* with digits. Instruction-following is the
// first thing to degrade on small local models, so "I'd suggest CRF 22" — a
// perfectly good answer — failed outright and was logged as an error.
func ExtractCRF(response string) (int, error) {
	match := firstInteger.FindString(strings.TrimSpace(response))
	if match == "" {
		return 0, fmt.Errorf("no number in response %q", truncateForError(response))
	}

	crf, err := strconv.Atoi(match)
	if err != nil {
		return 0, fmt.Errorf("unparseable number %q in response: %w", match, err)
	}

	if crf < MinSuggestedCRF || crf > MaxSuggestedCRF {
		return 0, fmt.Errorf("suggested CRF %d is outside the accepted range %d–%d",
			crf, MinSuggestedCRF, MaxSuggestedCRF)
	}
	return crf, nil
}
