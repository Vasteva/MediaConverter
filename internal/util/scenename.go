package util

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsedName is the result of reading a media filename.
type ParsedName struct {
	Title string // cleaned title, empty when nothing usable was found
	Year  int    // 0 when no year was identified
}

// Usable reports whether the parse produced something worth using as a name.
func (p ParsedName) Usable() bool {
	return p.Title != "" && hasLetterOrDigit(p.Title)
}

// String renders the parse as "Title (Year)", or just the title when no year
// was found.
func (p ParsedName) String() string {
	if p.Title == "" {
		return ""
	}
	if p.Year == 0 {
		return p.Title
	}
	return p.Title + " (" + strconv.Itoa(p.Year) + ")"
}

// releaseMarkers are tokens that begin the technical portion of a scene
// filename. Everything from the first marker onward is release metadata, not
// title — resolution, source, codec, audio layout, edition and release group.
//
// The list only needs to be good enough to locate where the title ends. Tokens
// after the year are discarded wholesale regardless, so this matters mainly for
// names that carry no year.
var releaseMarkers = map[string]bool{
	// Resolution and scan
	"480P": true, "576P": true, "720P": true, "1080P": true, "1080I": true,
	"2160P": true, "4K": true, "8K": true, "UHD": true, "HD": true, "SD": true,
	// Source
	"BLURAY": true, "BLU": true, "BDRIP": true, "BRRIP": true, "BR": true,
	"BDREMUX": true, "REMUX": true, "WEB": true, "WEBRIP": true, "WEBDL": true,
	"HDTV": true, "PDTV": true, "DVDRIP": true, "DVD": true, "DVDSCR": true,
	"HDDVD": true, "VHSRIP": true, "CAM": true, "TS": true, "TELESYNC": true,
	"DISK": true, "DISC": true, "ISO": true, "COMPLETE": true, "BD25": true,
	"BD50": true, "COASTER": true, "ATVP": true, "MA": true, "NF": true,
	"AMZN": true, "DSNP": true, "HMAX": true, "MGMP": true,
	// Video codec
	"X264": true, "X265": true, "H264": true, "H265": true, "H": true,
	"HEVC": true, "AVC": true, "XVID": true, "DIVX": true, "VC": true,
	"MPEG2": true, "AV1": true, "10BIT": true, "8BIT": true,
	// HDR
	"HDR": true, "HDR10": true, "HDR10PLUS": true, "DV": true, "DOVI": true,
	"SDR": true, "HLG": true,
	// Audio
	"DTS": true, "DTSHD": true, "DTSX": true, "AC3": true, "EAC3": true,
	"DD": true, "DDP": true, "AAC": true, "FLAC": true, "TRUEHD": true,
	"ATMOS": true, "OPUS": true, "MP3": true, "PCM": true, "LPCM": true,
	// Edition and status
	"PROPER": true, "REPACK": true, "RERIP": true, "EXTENDED": true,
	"UNRATED": true, "THEATRICAL": true, "DIRECTORS": true, "REMASTERED": true,
	"IMAX": true, "HYBRID": true, "LIMITED": true, "INTERNAL": true,
	"CUT": true, "EDITION": true, "VERSION": true, "AI": true,
	"SPECIAL": true, "ANNIVERSARY": true, "MULTI": true, "UNTOUCHED": true,
	// This pipeline's own output suffix
	"OPTIMIZED": true,
}

// makemkvTitle matches MakeMKV's per-title suffix, e.g. "_t00".
var makemkvTitle = regexp.MustCompile(`^[tT]\d{2,3}$`)

// tokenSplitter breaks a filename into words. Scene names use "." as the word
// separator; spaces and underscores appear in hand-named files.
var tokenSplitter = regexp.MustCompile(`[.\s_]+`)

// trimToken strips the punctuation that wraps a token without being part of it.
var trimToken = strings.NewReplacer("(", "", ")", "", "[", "", "]", "", "{", "", "}", "")

// ParseSceneName extracts a title and year from a media filename.
//
// It exists because scene filenames are highly regular —
// Title.Year.Quality.Codec-GROUP — and a regular format should be parsed, not
// guessed at. Asking a language model to do this produced "Days Later" from
// "28.Days.Later.2002.REPACK...", silently dropping a token that a parser
// cannot lose.
//
// The title is everything before the year. That single rule discards
// resolution, source, codec, audio layout, edition and release group in one
// step, without needing to recognise any of them. Markers are only consulted to
// decide which of several four-digit tokens is the year, and to find where the
// title ends in names that carry no year at all.
func ParseSceneName(filename string) ParsedName {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	raw := tokenSplitter.Split(base, -1)
	tokens := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.TrimSpace(trimToken.Replace(tok))
		if tok == "" || makemkvTitle.MatchString(tok) {
			continue
		}
		tokens = append(tokens, tok)
	}
	if len(tokens) == 0 {
		return ParsedName{}
	}

	firstMarker := len(tokens)
	for i, tok := range tokens {
		if isReleaseMarker(tok) {
			firstMarker = i
			break
		}
	}

	// The year is the last year-like token before the technical section. Taking
	// the *last* one is what makes "Blade Runner 2049 2017" and "2012 2009"
	// resolve correctly — in both, the earlier number belongs to the title.
	yearIndex, year := -1, 0
	for i := 0; i < firstMarker; i++ {
		if y, ok := yearFromToken(tokens[i]); ok {
			yearIndex, year = i, y
		}
	}

	// Edition markers can sit between the title and the year
	// ("Blade.Trinity.Extended.2004", "Stargate.Special.Edition.1994"), which
	// puts the year past the first marker. Widen the search rather than lose it.
	if yearIndex < 0 {
		for i := firstMarker; i < len(tokens); i++ {
			if y, ok := yearFromToken(tokens[i]); ok {
				yearIndex, year = i, y
				break
			}
		}
	}

	// A year at position zero cannot be the year — there would be no title in
	// front of it. Treat it as part of the title instead.
	if yearIndex == 0 {
		yearIndex, year = -1, 0
	}

	end := firstMarker
	if yearIndex >= 0 {
		end = yearIndex
	}

	// Trailing markers can survive when the year sat behind them.
	titleTokens := tokens[:end]
	for len(titleTokens) > 0 && isReleaseMarker(titleTokens[len(titleTokens)-1]) {
		titleTokens = titleTokens[:len(titleTokens)-1]
	}

	title := strings.Join(titleTokens, " ")
	title = strings.Trim(title, " -–—:,")

	return ParsedName{Title: SanitizeFilename(title), Year: year}
}

func isReleaseMarker(token string) bool {
	upper := strings.ToUpper(token)
	if releaseMarkers[upper] {
		return true
	}
	// Hyphens join a marker to a group or to another marker ("Bluray-2160p",
	// "REMUX-FraMeSToR", "BR-DISK", "WEB-DL"), so the leading part decides.
	//
	// Only the leading part: checking every part made
	// "The-Rescuers-35th-Anniversary-Edition" a marker because it ends in
	// "Edition", which swallowed the whole title.
	if before, _, found := strings.Cut(upper, "-"); found {
		return releaseMarkers[before]
	}
	return false
}

// yearFromToken reports whether a token is a plausible release year.
func yearFromToken(token string) (int, bool) {
	// Other tools append their own suffixes ("2013-TdarrCacheFile-9Tul...").
	if before, _, found := strings.Cut(token, "-"); found {
		token = before
	}
	if len(token) != 4 {
		return 0, false
	}
	n, err := strconv.Atoi(token)
	if err != nil {
		return 0, false
	}
	// 1888 is the earliest surviving film; allow a couple of years ahead for
	// announced releases.
	if n < 1888 || n > time.Now().Year()+2 {
		return 0, false
	}
	return n, true
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	// Non-ASCII letters count too (e.g. "Alien³").
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}
