package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server
	Port string

	// Paths
	SourceDir string
	DestDir   string

	// Encoding
	GPUVendor     string // "nvidia", "intel", "amd", "cpu"
	QualityPreset string // "fast", "medium", "slow"
	CRF           int

	// Jobs
	MaxConcurrentJobs int

	// AI
	AIProvider string // "gemini", "openai", "claude", "ollama", "none"
	AIApiKey   string
	AIEndpoint string
	AIModel    string

	// Auth
	AdminPassword string
	LicenseKey    string

	// Scanner
	ScannerEnabled       bool
	ScannerMode          string // "manual", "startup", "periodic", "watch", "hybrid"
	ScannerIntervalSec   int
	ScannerAutoCreate    bool
	ScannerProcessedFile string
}

func Load() *Config {
	return &Config{
		Port:                 getEnv("PORT", "8080"),
		SourceDir:            getEnv("SOURCE_DIR", "/storage"),
		DestDir:              getEnv("DEST_DIR", "/output"),
		GPUVendor:            getEnv("GPU_VENDOR", "cpu"),
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
