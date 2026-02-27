package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Vasteva/MediaConverter/internal/license"
	"github.com/Vasteva/MediaConverter/internal/system"
)

const currentSchemaVersion = 1

type ProcessingSchedule struct {
	Enabled     bool   `json:"enabled"`
	StartHour   int    `json:"startHour"`   // 0–23
	EndHour     int    `json:"endHour"`     // 0–23
	AllowedDays []int  `json:"allowedDays"` // 0=Sun…6=Sat; empty = all days
	Timezone    string `json:"timezone"`    // IANA e.g. "America/New_York"
}

type Config struct {
	SchemaVersion int `json:"schemaVersion"`

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

	// Subtitles
	SubtitleMode     string `json:"subtitleMode"`     // "always", "selective", "never"
	SubtitleLang     string `json:"subtitleLang"`     // ISO 639-1, e.g. "en"
	SubtitleAPIKey   string `json:"subtitleApiKey"`   // OpenSubtitles API (app) key
	SubtitleUsername string `json:"subtitleUsername"` // OpenSubtitles account username
	SubtitlePassword string `json:"subtitlePassword"` // OpenSubtitles account password

	// Processing
	VerifyOutput bool `json:"verifyOutput"` // Default: false (Pro only)
	DeleteSource bool `json:"deleteSource"` // Default: false

	// Scheduling
	Schedule ProcessingSchedule `json:"schedule"`

	// State
	IsPremium     bool `json:"-"`
	IsInitialized bool `json:"-"`
}

const ConfigFile = "/data/config.json"

func Load() *Config {
	// Default values
	cfg := &Config{
		SchemaVersion:        currentSchemaVersion,
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
		SubtitleMode:         getEnv("SUBTITLE_MODE", "selective"),
		SubtitleLang:         getEnv("SUBTITLE_LANG", "en"),
		SubtitleAPIKey:       getEnv("SUBTITLE_API_KEY", ""),
		SubtitleUsername:     getEnv("SUBTITLE_USERNAME", ""),
		SubtitlePassword:     getEnv("SUBTITLE_PASSWORD", ""),
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

	importJSON := &Config{}
	if err := json.Unmarshal(data, importJSON); err != nil {
		return err
	}

	// Detect which boolean keys are actually present in the JSON so we can
	// distinguish an explicit false from a missing field (Go zero-value for bool).
	// Without this, an old config.json without a key would silently override env vars.
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(data, &rawFields)
	hasBool := func(key string) bool {
		_, ok := rawFields[key]
		return ok
	}

	// Warn if the on-disk schema is older than the current version.
	if importJSON.SchemaVersion == 0 {
		log.Printf("[Config] Warning: config file has no schemaVersion; treating as version 0. Current version is %d.", currentSchemaVersion)
	} else if importJSON.SchemaVersion < currentSchemaVersion {
		log.Printf("[Config] Warning: config file schema version %d is older than current version %d. Some defaults may apply.", importJSON.SchemaVersion, currentSchemaVersion)
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
	if hasBool("scannerEnabled") {
		c.ScannerEnabled = importJSON.ScannerEnabled
	}
	if importJSON.ScannerMode != "" {
		c.ScannerMode = importJSON.ScannerMode
	}
	if importJSON.ScannerIntervalSec != 0 {
		c.ScannerIntervalSec = importJSON.ScannerIntervalSec
	}
	if hasBool("scannerAutoCreate") {
		c.ScannerAutoCreate = importJSON.ScannerAutoCreate
	}
	if importJSON.ScannerProcessedFile != "" {
		c.ScannerProcessedFile = importJSON.ScannerProcessedFile
	}

	if hasBool("verifyOutput") {
		c.VerifyOutput = importJSON.VerifyOutput
	}
	if hasBool("deleteSource") {
		c.DeleteSource = importJSON.DeleteSource
	}

	if importJSON.SubtitleMode != "" {
		c.SubtitleMode = importJSON.SubtitleMode
	}
	if importJSON.SubtitleLang != "" {
		c.SubtitleLang = importJSON.SubtitleLang
	}
	if importJSON.SubtitleAPIKey != "" {
		c.SubtitleAPIKey = importJSON.SubtitleAPIKey
	}
	if importJSON.SubtitleUsername != "" {
		c.SubtitleUsername = importJSON.SubtitleUsername
	}
	if importJSON.SubtitlePassword != "" {
		c.SubtitlePassword = importJSON.SubtitlePassword
	}

	if _, ok := rawFields["schedule"]; ok {
		c.Schedule = importJSON.Schedule
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

	tmp := ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigFile)
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
	return os.WriteFile(initFile, []byte(time.Now().Format(time.RFC3339)), 0600)
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
