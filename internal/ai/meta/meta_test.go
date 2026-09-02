package meta

import (
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
