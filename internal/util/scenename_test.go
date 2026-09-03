package util

import "testing"

// Every input here is a real filename from the WTZHOME library. The first case
// is the reported bug: a language model turned it into "Days Later", dropping a
// token a parser cannot lose.
func TestParseSceneNameOnRealLibrary(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{
			"28.Days.Later.2002.REPACK.BluRay.1080p.DTS-HD.MA.5.1.AVC.HYBRID.REMUX-FraMeSToR.h265.mkv",
			"28 Days Later (2002)",
		},
		{
			"28.Weeks.Later.2007.BluRay.1080p.REMUX.AVC.DTS-HD.MA.5.1-LEGi0N.h265.mkv",
			"28 Weeks Later (2007)",
		},
		{"28.Years.Later 2025.mkv", "28 Years Later (2025)"},
		{"28 Years Later The Bone Temple (2026).mkv", "28 Years Later The Bone Temple (2026)"},
		{
			// Digits inside the title as well as a year.
			"2.Fast.2.Furious.2003.UHD.BluRay.2160p.DTS-X.7.1.HEVC.REMUX-FraMeSToR-AsRequested.h265_optimized.mkv",
			"2 Fast 2 Furious (2003)",
		},
		{
			"A.Bad.Moms.Christmas.2017.2160p.WEBRip.x265.10bit.SDR.DTS-HD.MA.5.1-GASMASK.mkv",
			"A Bad Moms Christmas (2017)",
		},
		{
			"A.Good.Day.to.Die.Hard.2013.Theatrical.Cut.REPACK.2160p.MA.WEB-DL.DTS-HD.MA.7.1.DV.HDR.H.265-FLUX.mkv",
			"A Good Day to Die Hard (2013)",
		},
		{
			"A.Knights.Tale.2001.Extended.Cut.UHD.BluRay.2160p.TrueHD.Atmos.7.1.DV.HEVC.REMUX-FraMeSToR-AsRequested.mkv",
			"A Knights Tale (2001)",
		},
		{
			"Alien.1979.Theatrical.Cut.Eng.Fre.Ger.Ita.Spa.Cze.Tha.Jpn.2160p.BluRay.Remux.HDR10Plus.HDR.HEVC.DTS-HD.MA-SGF.h265.mkv",
			"Alien (1979)",
		},
		{
			"Aliens.vs.Predator.Requiem.2007.BluRay.2160p.Ai.DTS-HD.MA.5.1.DD.H265-KC.mkv",
			"Aliens vs Predator Requiem (2007)",
		},
		{
			"Aladdin.and.the.King.of.Thieves.1996.BluRay.Remux.1080p.AVC.DTS-HD.MA.5.1-HiFi-AsRequested.mkv",
			"Aladdin and the King of Thieves (1996)",
		},
		// Already-clean names must survive unchanged.
		{"Aliens (1986).mkv", "Aliens (1986)"},
		{"All Monsters Attack (1969).mkv", "All Monsters Attack (1969)"},
		{"Alien Romulus (2024).mkv", "Alien Romulus (2024)"},
		{"Alpha (2018).h265.mkv", "Alpha (2018)"},
		{"Aladdin (2019).h265.mkv", "Aladdin (2019)"},
		{"American Pie (1999) BR-DISK.mkv", "American Pie (1999)"},
		{"Alien Resurrection (1997) Bluray-2160p Proper.h265.mkv", "Alien Resurrection (1997)"},
		// Non-ASCII must be preserved.
		{"Alien³ (1992).mkv", "Alien³ (1992)"},
		// MakeMKV per-title suffix.
		{"Along Came Polly_t00.mkv", "Along Came Polly"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			got := ParseSceneName(tc.filename).String()
			if got != tc.want {
				t.Errorf("\n filename: %s\n      got: %q\n     want: %q", tc.filename, got, tc.want)
			}
		})
	}
}

// A number in the title is the case that breaks naive parsers, in both
// directions: a leading number that must be kept, and a title that is itself a
// year followed by the real year.
func TestParseSceneNameHandlesNumericTitles(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"Blade.Runner.2049.2017.2160p.BluRay.x265.mkv", "Blade Runner 2049 (2017)"},
		{"2012.2009.1080p.BluRay.x264.mkv", "2012 (2009)"},
		{"1917.2019.2160p.UHD.BluRay.mkv", "1917 (2019)"},
		{"300.2006.1080p.BluRay.x264.mkv", "300 (2006)"},
		// A year-like token with nothing before it is part of the title, not the
		// year — otherwise the title would come out empty.
		{"2012.1080p.BluRay.x264.mkv", "2012"},
		{"1984.mkv", "1984"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := ParseSceneName(tc.filename).String(); got != tc.want {
				t.Errorf("ParseSceneName(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestParseSceneNameUsability(t *testing.T) {
	cases := []struct {
		filename   string
		wantUsable bool
	}{
		{"28.Days.Later.2002.BluRay.mkv", true},
		{"Aliens (1986).mkv", true},
		{"1984.mkv", true},
		// Nothing but release metadata — no title to recover.
		{"1080p.BluRay.x264-GROUP.mkv", false},
		{".mkv", false},
		{"", false},
		{"....mkv", false},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			got := ParseSceneName(tc.filename)
			if got.Usable() != tc.wantUsable {
				t.Errorf("ParseSceneName(%q) = %+v, Usable() = %v, want %v",
					tc.filename, got, got.Usable(), tc.wantUsable)
			}
		})
	}
}

// The parse feeds a filesystem path, so its output must already be safe.
func TestParseSceneNameOutputIsFilesystemSafe(t *testing.T) {
	got := ParseSceneName("Mission: Impossible/Fallout.2018.1080p.mkv").Title
	if got == "" {
		t.Fatal("expected a title")
	}
	for _, bad := range []string{"/", "\\", ":", "*", "?", `"`, "<", ">", "|"} {
		if contains(got, bad) {
			t.Errorf("title %q contains %q, which is illegal in a filename", got, bad)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// These three came from running the parser over a real 543-file library. Each
// was a genuine parser bug, not a bad filename.
func TestParseSceneNameRegressions(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{
			// An edition marker sits between the title and the year, so a year
			// hunt that stops at the first marker never finds it. Previously
			// produced "Blade Trinity" with no year.
			name:     "edition marker before the year",
			filename: "Blade.Trinity.Extended.2004.MULTi.COMPLETE.BLURAY-UNTOUCHED.h265.mkv",
			want:     "Blade Trinity (2004)",
		},
		{
			name:     "two edition markers before the year",
			filename: "Stargate.Special.Edition.1994.MULTi.COMPLETE.BLURAY-HONOR.mkv",
			want:     "Stargate (1994)",
		},
		{
			// Checking every hyphen part made this a release marker because it
			// ends in "Edition", which swallowed the entire title and produced
			// nothing usable at all.
			name:     "hyphenated title ending in a marker word",
			filename: "The-Rescuers-35th-Anniversary-Edition (2012).mkv",
			want:     "The-Rescuers-35th-Anniversary-Edition (2012)",
		},
		{
			// Another tool's cache suffix welded onto the year token.
			name:     "year carrying a foreign suffix",
			filename: "The-Lone-Ranger (2013)-TdarrCacheFile-9TulBVPdixj.mkv",
			want:     "The-Lone-Ranger (2013)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseSceneName(tc.filename).String(); got != tc.want {
				t.Errorf("\n filename: %s\n      got: %q\n     want: %q", tc.filename, got, tc.want)
			}
		})
	}
}

// A filename with no year is not a failure — the title is still recoverable and
// is what should be used.
func TestParseSceneNameWithoutYear(t *testing.T) {
	cases := map[string]string{
		"Dragonslayer_t00.h265.mkv":   "Dragonslayer",
		"Some Film.1080p.BluRay.mkv":  "Some Film",
		"Another.Film.x265-GROUP.mkv": "Another Film",
	}
	for filename, want := range cases {
		t.Run(filename, func(t *testing.T) {
			got := ParseSceneName(filename)
			if got.String() != want {
				t.Errorf("ParseSceneName(%q) = %q, want %q", filename, got.String(), want)
			}
			if got.Year != 0 {
				t.Errorf("expected no year, got %d", got.Year)
			}
			if !got.Usable() {
				t.Error("a title with no year is still usable")
			}
		})
	}
}
