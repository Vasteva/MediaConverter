package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigFile points ConfigFile at a temporary file containing body, and
// restores the original path afterwards.
func withConfigFile(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	original := ConfigFile
	ConfigFile = path
	t.Cleanup(func() { ConfigFile = original })
}

// TestEnvironmentBeatsPersistedConfig is the case that cost a full round of
// deployment debugging.
//
// Compose resolved MAX_CONCURRENT_JOBS=1 and `docker compose config` confirmed
// it, but /data/config.json held maxConcurrentJobs: 2 and won silently — two
// workers, no log line, and the concurrency that caused the original transcode
// failures still in effect.
func TestEnvironmentBeatsPersistedConfig(t *testing.T) {
	withConfigFile(t, `{"maxConcurrentJobs":2,"aiProvider":"openai","crf":28}`)
	t.Setenv("MAX_CONCURRENT_JOBS", "1")
	t.Setenv("AI_PROVIDER", "ollama")

	cfg := Load()

	if cfg.MaxConcurrentJobs != 1 {
		t.Errorf("MaxConcurrentJobs = %d, want 1 — the environment must win", cfg.MaxConcurrentJobs)
	}
	if cfg.AIProvider != "ollama" {
		t.Errorf("AIProvider = %q, want %q", cfg.AIProvider, "ollama")
	}

	// CRF had no environment variable set, so the file value applies.
	if cfg.CRF != 28 {
		t.Errorf("CRF = %d, want 28 — the file should apply where the environment is silent", cfg.CRF)
	}
}

// A silent override is what made this expensive to diagnose, so every conflict
// must be recorded.
func TestConflictsAreRecorded(t *testing.T) {
	withConfigFile(t, `{"maxConcurrentJobs":2,"aiProvider":"openai"}`)
	t.Setenv("MAX_CONCURRENT_JOBS", "1")
	t.Setenv("AI_PROVIDER", "ollama")

	cfg := Load()
	if cfg.lastMerge == nil {
		t.Fatal("no merge record")
	}

	summary := cfg.lastMerge.ConflictSummary()
	for _, want := range []string{"maxConcurrentJobs", "MAX_CONCURRENT_JOBS", "aiProvider", "AI_PROVIDER"} {
		if !strings.Contains(summary, want) {
			t.Errorf("conflict summary missing %q\ngot: %s", want, summary)
		}
	}
}

// Agreement is not a conflict.
func TestNoConflictWhenValuesMatch(t *testing.T) {
	withConfigFile(t, `{"maxConcurrentJobs":1}`)
	t.Setenv("MAX_CONCURRENT_JOBS", "1")

	cfg := Load()
	if got := cfg.lastMerge.Conflicts(); len(got) != 0 {
		t.Errorf("matching values should not be reported as a conflict, got %v", got)
	}
}

// Secrets must never have their values logged, in either direction.
func TestSecretValuesAreNotLogged(t *testing.T) {
	withConfigFile(t, `{"adminPassword":"file-secret","aiApiKey":"sk-file","licenseKey":"VASTIVA-PRO-FILE-0000"}`)
	t.Setenv("ADMIN_PASSWORD", "env-secret")
	t.Setenv("AI_API_KEY", "sk-env")
	t.Setenv("LICENSE_KEY", "VASTIVA-PRO-ENV-1111")

	cfg := Load()
	summary := cfg.lastMerge.ConflictSummary()

	for _, secret := range []string{
		"file-secret", "env-secret",
		"sk-file", "sk-env",
		"VASTIVA-PRO-FILE-0000", "VASTIVA-PRO-ENV-1111",
	} {
		if strings.Contains(summary, secret) {
			t.Errorf("conflict summary leaked a secret value %q:\n%s", secret, summary)
		}
	}

	// It should still say which fields differed.
	for _, field := range []string{"adminPassword", "aiApiKey", "licenseKey"} {
		if !strings.Contains(summary, field) {
			t.Errorf("conflict summary should name the field %q\ngot: %s", field, summary)
		}
	}

	// And the environment values must be the ones in effect.
	if cfg.AdminPassword != "env-secret" {
		t.Error("environment password did not win")
	}
}

// An explicit false in the environment must beat a true on disk. Using
// os.Getenv rather than LookupEnv would treat some of these as unset.
func TestEnvExplicitFalseBeatsFileTrue(t *testing.T) {
	withConfigFile(t, `{"scannerEnabled":true,"deleteSource":true,"replaceInPlace":true}`)
	t.Setenv("SCANNER_ENABLED", "false")
	t.Setenv("DELETE_SOURCE", "false")
	t.Setenv("REPLACE_IN_PLACE", "false")

	cfg := Load()

	if cfg.ScannerEnabled {
		t.Error("ScannerEnabled: environment false must beat file true")
	}
	if cfg.DeleteSource {
		t.Error("DeleteSource: environment false must beat file true")
	}
	if cfg.ReplaceInPlace {
		t.Error("ReplaceInPlace: environment false must beat file true")
	}
}

// With nothing in the environment, the file is authoritative — the UI must keep
// working.
func TestFileAppliesWhenEnvironmentIsSilent(t *testing.T) {
	withConfigFile(t, `{"maxConcurrentJobs":4,"qualityPreset":"slow","crf":19,"scannerEnabled":true}`)
	for _, key := range []string{"MAX_CONCURRENT_JOBS", "QUALITY_PRESET", "CRF", "SCANNER_ENABLED"} {
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.MaxConcurrentJobs != 4 {
		t.Errorf("MaxConcurrentJobs = %d, want 4", cfg.MaxConcurrentJobs)
	}
	if cfg.QualityPreset != "slow" {
		t.Errorf("QualityPreset = %q, want %q", cfg.QualityPreset, "slow")
	}
	if cfg.CRF != 19 {
		t.Errorf("CRF = %d, want 19", cfg.CRF)
	}
	if !cfg.ScannerEnabled {
		t.Error("ScannerEnabled should come from the file")
	}
}

// CRF 0 is lossless and a legitimate setting, so it must survive a round trip
// rather than being read as "absent".
func TestExplicitZeroIsNotTreatedAsAbsent(t *testing.T) {
	withConfigFile(t, `{"crf":0,"puid":0,"pgid":0}`)
	for _, key := range []string{"CRF", "PUID", "PGID"} {
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.CRF != 0 {
		t.Errorf("CRF = %d, want 0 — an explicit zero must be honoured", cfg.CRF)
	}
	if cfg.PUID != 0 || cfg.PGID != 0 {
		t.Errorf("PUID/PGID = %d/%d, want 0/0 — root is a real uid", cfg.PUID, cfg.PGID)
	}
}

// A missing config file is normal on first boot and must not be an error.
func TestMissingConfigFileIsFine(t *testing.T) {
	original := ConfigFile
	ConfigFile = filepath.Join(t.TempDir(), "absent.json")
	t.Cleanup(func() { ConfigFile = original })

	t.Setenv("MAX_CONCURRENT_JOBS", "3")
	if cfg := Load(); cfg.MaxConcurrentJobs != 3 {
		t.Errorf("MaxConcurrentJobs = %d, want 3", cfg.MaxConcurrentJobs)
	}
}
