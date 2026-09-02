package util

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain title", "Normal Title", "Normal Title"},
		{"colon", "Title: With Colon", "Title_ With Colon"},
		{"slashes", "Title/With/Slashes", "Title_With_Slashes"},
		{"assorted illegal", "Title<>With|Invalid*Chars", "Title__With_Invalid_Chars"},
		{"surrounding space", "  Spaces Around  ", "Spaces Around"},
		{"backslashes", `Title\With\Backslashes`, "Title_With_Backslashes"},
		{"quotes", `Title "Quoted"`, "Title _Quoted_"},
		{"question mark", "Who Framed Roger Rabbit?", "Who Framed Roger Rabbit_"},
		{"trailing dot", "Mr. Nobody.", "Mr. Nobody"},
		{"leading dot stays hidden-free", ".hidden", "hidden"},
		{"trailing space before dot", "Title . ", "Title"},
		{"unicode preserved", "Alien³ (1992)", "Alien³ (1992)"},
		{"control characters removed", "Title\x00\x1fWith\x7fControl", "TitleWithControl"},
		{"newline removed", "Title\nSecond", "TitleSecond"},
		{"tab removed", "Title\tTabbed", "TitleTabbed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeFilename(tc.in); got != tc.want {
				t.Errorf("SanitizeFilename(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

// Path separators must be replaced, never interpreted — the result is joined to
// a directory by the caller.
func TestSanitizeFilenameCannotEscapeDirectory(t *testing.T) {
	for _, in := range []string{
		"../../etc/passwd",
		`..\..\windows\system32\config`,
		"/absolute/path",
		"a/../../b",
	} {
		got := SanitizeFilename(in)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("SanitizeFilename(%q) = %q — retains a path separator", in, got)
		}
	}
}

func TestSanitizeFilenameCapsLength(t *testing.T) {
	got := SanitizeFilename(strings.Repeat("A", 500))
	if n := len([]rune(got)); n != MaxFilenameRunes {
		t.Errorf("length = %d runes, want %d", n, MaxFilenameRunes)
	}
}

// Truncation must not split a multi-byte character.
func TestSanitizeFilenameTruncatesOnRuneBoundary(t *testing.T) {
	got := SanitizeFilename(strings.Repeat("é", 500))
	if n := len([]rune(got)); n != MaxFilenameRunes {
		t.Errorf("length = %d runes, want %d", n, MaxFilenameRunes)
	}
	for _, r := range got {
		if r != 'é' {
			t.Fatalf("truncation corrupted a rune: found %q", r)
		}
	}
}

func TestSanitizeFilenameAvoidsWindowsReservedNames(t *testing.T) {
	for _, in := range []string{"CON", "con", "PRN", "AUX", "NUL", "COM1", "LPT9"} {
		if got := SanitizeFilename(in); strings.EqualFold(got, in) {
			t.Errorf("SanitizeFilename(%q) = %q — reserved device name passed through", in, got)
		}
	}
	// A name that merely starts with a reserved word is fine.
	if got := SanitizeFilename("Contact (1997)"); got != "Contact (1997)" {
		t.Errorf("SanitizeFilename(\"Contact (1997)\") = %q, want it unchanged", got)
	}
}

func TestSanitizeFilenameEmptyResult(t *testing.T) {
	for _, in := range []string{"", "   ", "...", "\x00\x01"} {
		if got := SanitizeFilename(in); got != "" {
			t.Errorf("SanitizeFilename(%q) = %q, want empty so callers can reject it", in, got)
		}
	}
}

func TestFileOwnershipDisabledByDefault(t *testing.T) {
	if NoOwnership().Enabled() {
		t.Error("NoOwnership must be disabled")
	}
	// Zero is a real uid (root), so it cannot double as the sentinel.
	if !(FileOwnership{UID: 0, GID: 0}).Enabled() {
		t.Error("uid/gid 0 is root, not 'unset' — it must count as enabled")
	}
	if !(FileOwnership{UID: 1000, GID: OwnershipDisabled}).Enabled() {
		t.Error("a set uid alone should enable ownership")
	}
}

// Apply on a disabled ownership must be a no-op, including for paths that do
// not exist — the non-replacing output path calls it unconditionally.
func TestFileOwnershipApplyDisabledIsNoop(t *testing.T) {
	if err := NoOwnership().Apply("/nonexistent/path/file.mkv"); err != nil {
		t.Errorf("disabled Apply should do nothing, got %v", err)
	}
}

func TestFileOwnershipString(t *testing.T) {
	if got := NoOwnership().String(); got != "unchanged" {
		t.Errorf("String() = %q, want %q", got, "unchanged")
	}
	if got := (FileOwnership{UID: 1000, GID: 1000}).String(); got != "1000:1000" {
		t.Errorf("String() = %q, want %q", got, "1000:1000")
	}
}
