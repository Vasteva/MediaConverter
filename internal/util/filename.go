package util

import (
	"regexp"
	"strings"
)

// MaxFilenameRunes caps a generated base name. Most filesystems allow 255
// bytes for a full name; leaving headroom keeps room for the extension, a
// "_optimized" suffix, and multi-byte characters that cost more than one byte
// each.
const MaxFilenameRunes = 150

var (
	// Illegal on Windows and SMB shares, and "/" is illegal everywhere. The set
	// is deliberately the strictest of the platforms involved: this library is
	// served over SMB, so a name that is legal on ext4 but not on Windows still
	// breaks for clients.
	illegalFilenameChars = regexp.MustCompile(`[<>:"/\\|?*]`)

	// Control characters and DEL. These can render a name unusable or, in a
	// terminal, actively misleading.
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// windowsReservedNames cannot be used as a file's base name on Windows or over
// SMB, regardless of extension.
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeFilename converts an arbitrary string into a base name that is safe
// to write on every filesystem this project targets.
//
// It is a last line of defence, not a parser: it assumes the caller has already
// extracted the name it wants. In particular, path separators are replaced
// rather than interpreted, so a value containing "../" cannot escape the
// directory it is joined to.
//
// Returns an empty string when nothing usable survives; callers must handle
// that rather than writing a file with no name.
func SanitizeFilename(name string) string {
	s := controlChars.ReplaceAllString(name, "")
	s = illegalFilenameChars.ReplaceAllString(s, "_")

	// Trailing dots and spaces are silently stripped by Windows, which turns
	// "Title ." into "Title" and breaks any path recorded elsewhere.
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".")
	s = strings.TrimSpace(s)

	if r := []rune(s); len(r) > MaxFilenameRunes {
		s = strings.TrimSpace(string(r[:MaxFilenameRunes]))
	}

	if windowsReservedNames[strings.ToUpper(s)] {
		s += "_"
	}

	return s
}
