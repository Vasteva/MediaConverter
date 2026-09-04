package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Vasteva/MediaConverter/internal/config"
)

// TestLoadScannerConfigMigratesResolutionFilter covers #41: skipHighResolution
// and resolutionHeightThreshold moved from ScannerConfig to the system
// Config, so an existing install's scanner_config.json needs its values
// carried over rather than silently dropped.
func TestLoadScannerConfigMigratesResolutionFilter(t *testing.T) {
	dir := t.TempDir()
	scannerFile := filepath.Join(dir, "scanner_config.json")
	configFile := filepath.Join(dir, "config.json")

	orig := config.ConfigFile
	config.ConfigFile = configFile
	t.Cleanup(func() { config.ConfigFile = orig })

	legacy := []byte(`{"mode":"manual","skipHighResolution":true,"resolutionHeightThreshold":2160}`)
	if err := os.WriteFile(scannerFile, legacy, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg := &config.Config{}
	scannerCfg, err := LoadScannerConfig(cfg, scannerFile)
	if err != nil {
		t.Fatalf("LoadScannerConfig: %v", err)
	}
	if scannerCfg.Mode != ScanModeManual {
		t.Errorf("expected parsed mode to survive migration, got %q", scannerCfg.Mode)
	}
	if !cfg.SkipHighResolution || cfg.ResolutionHeightThreshold != 2160 {
		t.Fatalf("expected migration into system config, got SkipHighResolution=%v ResolutionHeightThreshold=%d",
			cfg.SkipHighResolution, cfg.ResolutionHeightThreshold)
	}

	var savedOnDisk struct {
		SkipHighResolution        bool `json:"skipHighResolution"`
		ResolutionHeightThreshold int  `json:"resolutionHeightThreshold"`
	}
	saved, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("reading migrated system config: %v", err)
	}
	if err := json.Unmarshal(saved, &savedOnDisk); err != nil {
		t.Fatalf("parsing migrated system config: %v", err)
	}
	if !savedOnDisk.SkipHighResolution || savedOnDisk.ResolutionHeightThreshold != 2160 {
		t.Errorf("expected persisted system config to hold the migrated value, got %+v", savedOnDisk)
	}

	var rewritten map[string]any
	rewrittenData, err := os.ReadFile(scannerFile)
	if err != nil {
		t.Fatalf("reading rewritten scanner file: %v", err)
	}
	if err := json.Unmarshal(rewrittenData, &rewritten); err != nil {
		t.Fatalf("parsing rewritten scanner file: %v", err)
	}
	if _, present := rewritten["skipHighResolution"]; present {
		t.Errorf("expected legacy keys stripped from %s after migration, got: %s", scannerFile, rewrittenData)
	}

	// A second load must not re-migrate — the legacy file no longer carries
	// the keys, so a value someone has since changed via Settings survives.
	cfg2 := &config.Config{SkipHighResolution: false, ResolutionHeightThreshold: 1080}
	if _, err := LoadScannerConfig(cfg2, scannerFile); err != nil {
		t.Fatalf("second LoadScannerConfig: %v", err)
	}
	if cfg2.SkipHighResolution {
		t.Error("second load re-migrated from an already-stripped scanner file, would clobber a since-changed Settings value")
	}
}

// TestLoadScannerConfigNoMigrationWhenNoLegacyKeys covers the common case: a
// scanner_config.json that never had the fields (new install, or already
// migrated) shouldn't touch the system config file at all.
func TestLoadScannerConfigNoMigrationWhenNoLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	scannerFile := filepath.Join(dir, "scanner_config.json")
	configFile := filepath.Join(dir, "config.json")

	orig := config.ConfigFile
	config.ConfigFile = configFile
	t.Cleanup(func() { config.ConfigFile = orig })

	if err := os.WriteFile(scannerFile, []byte(`{"mode":"manual"}`), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg := &config.Config{SkipHighResolution: true, ResolutionHeightThreshold: 720}
	if _, err := LoadScannerConfig(cfg, scannerFile); err != nil {
		t.Fatalf("LoadScannerConfig: %v", err)
	}
	if !cfg.SkipHighResolution || cfg.ResolutionHeightThreshold != 720 {
		t.Errorf("expected system config untouched with no legacy keys present, got SkipHighResolution=%v ResolutionHeightThreshold=%d",
			cfg.SkipHighResolution, cfg.ResolutionHeightThreshold)
	}
	if _, err := os.Stat(configFile); err == nil {
		t.Error("expected no system config file to be written when there is nothing to migrate")
	}
}
