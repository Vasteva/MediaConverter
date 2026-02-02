package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Vasteva/MediaConverter/internal/license"
	"github.com/Vasteva/MediaConverter/internal/system"
)

type Config struct {
	// Server
	Port string `json:"port"`

	// Paths
	SourceDir string `json:"sourceDir"`
	DestDir   string `json:"destDir"`

	// Encoding
	GPUVendor     string `json:"gpuVendor"`
	QualityPreset string `json:"qualityPreset"`
	CRF           int    `json:"crf"`

	// Jobs
	MaxConcurrentJobs int `json:"maxConcurrentJobs"`

	// AI
	AIProvider string `json:"aiProvider"`
	AIApiKey   string `json:"aiApiKey"`
	AIEndpoint string `json:"aiEndpoint"`
	AIModel    string `json:"aiModel"`

	// Auth
	AdminPassword string `json:"adminPassword"`
	LicenseKey    string `json:"licenseKey"`

	// Scanner
	ScannerEnabled       bool   `json:"scannerEnabled"`
	ScannerMode          string `json:"scannerMode"`
	ScannerIntervalSec   int    `json:"scannerIntervalSec"`
	ScannerAutoCreate    bool   `json:"scannerAutoCreate"`
	ScannerProcessedFile string `json:"scannerProcessedFile"`

	// State
	IsPremium     bool `json:"-"`
	IsInitialized bool `json:"-"`
}

const ConfigFile = "/data/config.json"

func Load() *Config {
	// Default values
	cfg := &Config{
		Port:                 getEnv("PORT", "8080"),
		SourceDir:            getEnv("SOURCE_DIR", "/storage"),
		DestDir:              getEnv("DEST_DIR", "/output"),
		GPUVendor:            getEnv("GPU_VENDOR", "auto"),
		QualityPreset:        getEnv("QUALITY_PRESET", "medium"),
		CRF:                  getEnvInt("CRF", 23),
		MaxConcurrentJobs:    getEnvInt("MAX_CONCURRENT_JOBS", 2),
		AIProvider:           getEnv("AI_PROVIDER", "none"),
		AIApiKey:             getEnv("AI_API_KEY", ""),
		AIEndpoint:           getEnv("AI_ENDPOINT", ""),
		AIModel:              getEnv("AI_MODEL", ""),
		AdminPassword:        getEnv("ADMIN_PASSWORD", ""),
		LicenseKey:           getEnv("LICENSE_KEY", ""),
		ScannerEnabled:       getEnvBool("SCANNER_ENABLED", false),
		ScannerMode:          getEnv("SCANNER_MODE", "manual"),
		ScannerIntervalSec:   getEnvInt("SCANNER_INTERVAL_SEC", 300),
		ScannerAutoCreate:    getEnvBool("SCANNER_AUTO_CREATE", true),
		ScannerProcessedFile: getEnv("SCANNER_PROCESSED_FILE", "/data/processed.json"),
	}

	if cfg.GPUVendor == "auto" || cfg.GPUVendor == "" {
		cfg.GPUVendor = system.DetectGPU()
	}

	// Override with values from disk if available
	if err := cfg.loadFromDisk(); err != nil && !os.IsNotExist(err) {
		// Log error but continue
	}

	cfg.IsPremium = license.Validate(cfg.LicenseKey)
	cfg.IsInitialized = checkInitialized(cfg.ScannerProcessedFile)

	return cfg
}

func (c *Config) loadFromDisk() error {
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return err
	}
	// We decode into a temporary struct to only override non-empty values or we just overwrite everything?
	// For simplicity, let's just overwrite. The disk config is the source of truth for changes.
	// However, we must be careful not to zero out environment variables if the json is partial.
	// But usually if we save, we save the whole struct.
	// Let's unmarshal directly into c.
	importJSON := &Config{}
	if err := json.Unmarshal(data, importJSON); err != nil {
		return err
	}

	// Apply overrides
	// Note: Strings will be overwritten if they are empty in JSON? No, Unmarshal does that.
	// But if we generated the JSON from this struct, it has all fields.
	// Let's assume the JSON contains the user's preferred state.

	if importJSON.Port != "" {
		c.Port = importJSON.Port
	}
	if importJSON.SourceDir != "" {
		c.SourceDir = importJSON.SourceDir
	}
	if importJSON.DestDir != "" {
		c.DestDir = importJSON.DestDir
	}
	if importJSON.GPUVendor != "" && importJSON.GPUVendor != "cpu" && importJSON.GPUVendor != "auto" {
		// Only use saved GPU if it's an explicit choice (nvidia, intel, amd)
		c.GPUVendor = importJSON.GPUVendor
	}
	// If saved config was cpu/auto, we keep the auto-detected value from runtime
	if importJSON.QualityPreset != "" {
		c.QualityPreset = importJSON.QualityPreset
	}
	if importJSON.CRF != 0 {
		c.CRF = importJSON.CRF
	}
	if importJSON.MaxConcurrentJobs != 0 {
		c.MaxConcurrentJobs = importJSON.MaxConcurrentJobs
	}

	if importJSON.AIProvider != "" {
		c.AIProvider = importJSON.AIProvider
	}
	if importJSON.AIApiKey != "" {
		c.AIApiKey = importJSON.AIApiKey
	}
	if importJSON.AIEndpoint != "" {
		c.AIEndpoint = importJSON.AIEndpoint
	}
	if importJSON.AIModel != "" {
		c.AIModel = importJSON.AIModel
	}

	if importJSON.AdminPassword != "" {
		c.AdminPassword = importJSON.AdminPassword
	}
	if importJSON.LicenseKey != "" {
		c.LicenseKey = importJSON.LicenseKey
	}

	// Scanner
	// Booleans are tricky because false is zero value.
	// To handle this properly we'd need pointers or a map, but for now let's say
	// if the config file exists, we trust it fully for simple types if we assume it was written by Save().
	// But Unmarshal over 'c' would zero out fields missing in JSON.
	// So we strictly copy meaningful values.
	// For booleans, we might need to assume if it's in the file, we take it.
	// But we can't know if it's "in the file" vs "false" without using map.
	// Let's just create a custom save/load for critical paths or rely on the main config update flow.
	// Given the context (Admin Password issue), ensuring AdminPassword persistence is key.

	c.ScannerEnabled = importJSON.ScannerEnabled
	if importJSON.ScannerMode != "" {
		c.ScannerMode = importJSON.ScannerMode
	}
	if importJSON.ScannerIntervalSec != 0 {
		c.ScannerIntervalSec = importJSON.ScannerIntervalSec
	}
	c.ScannerAutoCreate = importJSON.ScannerAutoCreate
	if importJSON.ScannerProcessedFile != "" {
		c.ScannerProcessedFile = importJSON.ScannerProcessedFile
	}

	return nil
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Ensure /data exists
	dir := filepath.Dir(ConfigFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(ConfigFile, data, 0644)
}

func checkInitialized(processedFile string) bool {
	dir := filepath.Dir(processedFile)
	initFile := filepath.Join(dir, ".initialized")
	_, err := os.Stat(initFile)
	return err == nil
}

// MarkInitialized creates the .initialized file
func (c *Config) MarkInitialized() error {
	dir := filepath.Dir(c.ScannerProcessedFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	initFile := filepath.Join(dir, ".initialized")
	c.IsInitialized = true
	return os.WriteFile(initFile, []byte(time.Now().Format(time.RFC3339)), 0644)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}
