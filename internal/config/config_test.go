package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set test environment variables
	t.Setenv("PORT", "9090")
	t.Setenv("SOURCE_DIR", "/tmp/source")
	t.Setenv("DEST_DIR", "/tmp/dest")
	t.Setenv("GPU_VENDOR", "nvidia")
	t.Setenv("QUALITY_PRESET", "fast")
	t.Setenv("CRF", "18")
	t.Setenv("MAX_CONCURRENT_JOBS", "4")
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("ADMIN_PASSWORD", "supersecret")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("expected Port 9090, got %s", cfg.Port)
	}
	if cfg.SourceDir != "/tmp/source" {
		t.Errorf("expected SourceDir /tmp/source, got %s", cfg.SourceDir)
	}
	if cfg.GPUVendor != "nvidia" {
		t.Errorf("expected GPUVendor nvidia, got %s", cfg.GPUVendor)
	}
	if cfg.CRF != 18 {
		t.Errorf("expected CRF 18, got %d", cfg.CRF)
	}
	if cfg.MaxConcurrentJobs != 4 {
		t.Errorf("expected MaxConcurrentJobs 4, got %d", cfg.MaxConcurrentJobs)
	}
	if cfg.AIProvider != "openai" {
		t.Errorf("expected AIProvider openai, got %s", cfg.AIProvider)
	}
	if cfg.AdminPassword != "supersecret" {
		t.Errorf("expected AdminPassword supersecret, got %s", cfg.AdminPassword)
	}
}

func TestGetEnvDefaults(t *testing.T) {
	// Clear relevant env vars
	os.Unsetenv("PORT")
	os.Unsetenv("SCANNER_ENABLED")

	cfg := Load()

	if cfg.Port != "8080" { // Default value
		t.Errorf("expected default Port 8080, got %s", cfg.Port)
	}
	if cfg.ScannerEnabled != false {
		t.Errorf("expected default ScannerEnabled false, got %v", cfg.ScannerEnabled)
	}
}
