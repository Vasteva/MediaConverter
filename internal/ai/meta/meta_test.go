package meta

import (
	"context"
	"strings"
	"testing"
)

// TestExtractTitleFromObservedResponses covers the output that actually reached
// the filesystem at WTZHOME. Four of the first five files in the output
// directory carried literal quotation marks, and one carried a colon:
//
//	"Batman v Superman: Dawn of Justice (2016)".mkv
//	"Batman.Ninja (2018)".mkv
//	"Beauty and the Beast (1991)".mkv
//	"Bedazzled (2000)".mkv
//
// The colon alone makes the name unusable over SMB.
func TestExtractTitleFromObservedResponses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"clean response", "The Matrix (1999)", "The Matrix (1999)"},
		{"wrapped in quotes", `"Batman.Ninja (2018)"`, "Batman.Ninja (2018)"},
		{"quotes and whitespace", "  \"Bedazzled (2000)\"  \n", "Bedazzled (2000)"},
		{"colon in title", `"Batman v Superman: Dawn of Justice (2016)"`, "Batman v Superman_ Dawn of Justice (2016)"},
		{"single quotes", "'Aliens (1986)'", "Aliens (1986)"},
		{"smart quotes", "“Alien³ (1992)”", "Alien³ (1992)"},
		{"backticks", "`Alpha (2018)`", "Alpha (2018)"},
		{"markdown fence", "```\nAvengers Endgame (2019)\n```", "Avengers Endgame (2019)"},
		{"fence with language tag", "```text\nAmerican Pie (1999)\n```", "American Pie (1999)"},
		{"trailing explanation", "Aquaman (2018)\n\nThis is a 2018 superhero film.", "Aquaman (2018)"},
		{"windows-illegal characters", `Face/Off <2> |1997|`, "Face_Off _2_ _1997_"},
		{"trailing dot stripped", "Mr. Nobody.", "Mr. Nobody"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractTitle(tc.raw)
			if err != nil {
				t.Fatalf("ExtractTitle(%q) returned error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ExtractTitle(%q)\n got: %q\nwant: %q", tc.raw, got, tc.want)
			}
		})
	}
}

// A model response is untrusted input that the caller turns into a write path.
// Path separators must be neutralised, never interpreted.
func TestExtractTitleNeutralisesPathTraversal(t *testing.T) {
	for _, raw := range []string{
		"../../etc/passwd",
		`..\..\windows\system32`,
		"/etc/cron.d/evil",
		"Title (2020)/../../../root/.ssh/authorized_keys",
	} {
		got, err := ExtractTitle(raw)
		if err != nil {
			continue // rejecting outright is also acceptable
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("ExtractTitle(%q) = %q — still contains a path separator", raw, got)
		}
		if got == ".." || strings.HasPrefix(got, "..") && !strings.ContainsAny(got, " (") {
			t.Errorf("ExtractTitle(%q) = %q — resolves to a parent directory reference", raw, got)
		}
	}
}

func TestExtractTitleRejectsUnusableResponses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace only", "   \n\t  "},
		{"quotes only", `""`},
		{"prose", "I'm sorry, but I cannot determine the title of this file from the information provided. " +
			"The filename appears to be obfuscated and does not contain enough detail for me to identify the movie."},
		{"only illegal characters", `///???`},
		{"only dots", "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ExtractTitle(tc.raw); err == nil {
				t.Errorf("expected rejection of %q, got title %q", tc.raw, got)
			}
		})
	}
}

// Windows and SMB reserve these names regardless of extension.
func TestExtractTitleAvoidsReservedNames(t *testing.T) {
	for _, raw := range []string{"CON", "nul", "Com1", "LPT9"} {
		got, err := ExtractTitle(raw)
		if err != nil {
			t.Fatalf("ExtractTitle(%q): %v", raw, err)
		}
		if strings.EqualFold(got, raw) {
			t.Errorf("ExtractTitle(%q) = %q — reserved device name passed through", raw, got)
		}
	}
}

func TestExtractTitleCapsLength(t *testing.T) {
	// Under the prose threshold but longer than any filesystem-safe base name.
	long := strings.Repeat("A", 119)
	got, err := ExtractTitle(long)
	if err != nil {
		t.Fatalf("ExtractTitle: %v", err)
	}
	if len([]rune(got)) > 150 {
		t.Errorf("title is %d runes, expected it to be capped", len([]rune(got)))
	}
}

// TestExtractCRFFromModelResponses covers the responses a small local model
// actually produces. The previous parser used fmt.Sscanf(response, "%d", &crf),
// which requires the response to *begin* with digits — so every conversational
// answer here failed outright and was logged as an error, even though the
// fallback to the configured CRF worked correctly.
func TestExtractCRFFromModelResponses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"bare integer", "22", 22},
		{"trailing newline", "22\n", 22},
		{"leading prose", "I'd suggest CRF 22", 22},
		{"full sentence", "Based on the bitrate, I recommend a CRF of 20 for this source.", 20},
		{"labelled", "CRF: 24", 24},
		{"markdown emphasis", "**23**", 23},
		{"code fence", "```\n21\n```", 21},
		{"trailing explanation", "19\n\nThis is a grainy film source.", 19},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractCRF(tc.raw)
			if err != nil {
				t.Fatalf("ExtractCRF(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ExtractCRF(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestExtractCRFRejectsUnusableValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"no digits", "I cannot determine an appropriate value."},
		{"empty", ""},
		{"too low", "2"},
		{"too high", "51"},
		{"negative", "-5"},
		{"zero", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ExtractCRF(tc.raw); err == nil {
				t.Errorf("expected rejection of %q, got CRF %d", tc.raw, got)
			}
		})
	}
}

// A rejected suggestion must return 0, not a plausible-looking default. The
// previous signature returned 23 alongside the error, which invites a caller to
// use a value that no model produced.
func TestExtractCRFReturnsZeroOnFailure(t *testing.T) {
	if got, _ := ExtractCRF("no number here"); got != 0 {
		t.Errorf("failed parse returned %d, want 0 so the caller uses its own default", got)
	}
}

// stubProvider returns a canned response, so the fallback path can be exercised
// without a live model.
type stubProvider struct {
	response string
	called   bool
}

func (s *stubProvider) Analyze(_ context.Context, _ string) (string, error) {
	s.called = true
	return s.response, nil
}
func (s *stubProvider) Transcribe(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubProvider) VerifyMedia(_ context.Context, _, _ []string) (bool, error) {
	return true, nil
}
func (s *stubProvider) GetName() string { return "stub" }

// TestCleanFilenamePrefersParser is the fix for the reported bug. A model turned
// "28.Days.Later.2002..." into "Days Later"; the parser cannot drop a token, and
// it now runs first — so the model is never consulted for a name like this.
func TestCleanFilenamePrefersParser(t *testing.T) {
	provider := &stubProvider{response: "Days Later"}
	cleaner := NewCleaner(provider)

	title, source, err := cleaner.CleanFilename(context.Background(),
		"28.Days.Later.2002.REPACK.BluRay.1080p.DTS-HD.MA.5.1.AVC.HYBRID.REMUX-FraMeSToR.mkv")
	if err != nil {
		t.Fatalf("CleanFilename: %v", err)
	}
	if title != "28 Days Later (2002)" {
		t.Errorf("title = %q, want %q", title, "28 Days Later (2002)")
	}
	if source != SourceParser {
		t.Errorf("source = %q, want %q", source, SourceParser)
	}
	if provider.called {
		t.Error("the model was consulted for a filename the parser handles")
	}
}

// A name the parser cannot read still reaches the model.
func TestCleanFilenameFallsBackToModel(t *testing.T) {
	provider := &stubProvider{response: "Some Obscure Film (1974)"}
	cleaner := NewCleaner(provider)

	_, source, err := cleaner.CleanFilename(context.Background(), "1080p.BluRay.x264-GROUP.mkv")
	if err == nil && source != SourceModel {
		t.Errorf("source = %q, want the model to be consulted", source)
	}
	if !provider.called {
		t.Error("the model should be consulted when the parser finds no title")
	}
}

// Without a provider, an unparseable name is an error rather than a bad guess.
func TestCleanFilenameNoProviderNoParse(t *testing.T) {
	cleaner := NewCleaner(nil)
	if _, _, err := cleaner.CleanFilename(context.Background(), "1080p.BluRay.x264-GROUP.mkv"); err == nil {
		t.Error("expected an error when nothing can name the file")
	}
}

// The parser still works with no provider at all — most renames need no model.
func TestCleanFilenameParserWorksWithoutProvider(t *testing.T) {
	cleaner := NewCleaner(nil)
	title, source, err := cleaner.CleanFilename(context.Background(), "Aliens.1986.1080p.BluRay.x264.mkv")
	if err != nil {
		t.Fatalf("CleanFilename: %v", err)
	}
	if title != "Aliens (1986)" || source != SourceParser {
		t.Errorf("got %q via %q, want %q via %q", title, source, "Aliens (1986)", SourceParser)
	}
}

func TestCheckTitleAgainstSource(t *testing.T) {
	cases := []struct {
		name      string
		title     string
		filename  string
		wantError bool
	}{
		{"exact match", "Aliens", "Aliens.1986.1080p.mkv", false},
		{"case differs", "aliens", "ALIENS.1986.mkv", false},
		{"partial expansion is allowed", "Star Wars A New Hope", "Star.Wars.New.Hope.mkv", false},
		{"unrelated title rejected", "The Godfather", "Aliens.1986.1080p.BluRay.mkv", true},
		{"hallucinated entirely", "Some Film Nobody Made", "Aliens.1986.mkv", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTitleAgainstSource(tc.title, tc.filename)
			if tc.wantError && err == nil {
				t.Errorf("expected %q to be rejected for %q", tc.title, tc.filename)
			}
			if !tc.wantError && err != nil {
				t.Errorf("expected %q to be accepted for %q, got %v", tc.title, tc.filename, err)
			}
		})
	}
}
