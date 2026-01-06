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
}

func Load() *Config {
	return &Config{
		Port:              getEnv("PORT", "8080"),
		SourceDir:         getEnv("SOURCE_DIR", "/storage"),
		DestDir:           getEnv("DEST_DIR", "/output"),
		GPUVendor:         getEnv("GPU_VENDOR", "cpu"),
		QualityPreset:     getEnv("QUALITY_PRESET", "medium"),
		CRF:               getEnvInt("CRF", 23),
		MaxConcurrentJobs: getEnvInt("MAX_CONCURRENT_JOBS", 2),
		AIProvider:        getEnv("AI_PROVIDER", "none"),
		AIApiKey:          getEnv("AI_API_KEY", ""),
		AIEndpoint:        getEnv("AI_ENDPOINT", ""),
		AIModel:           getEnv("AI_MODEL", ""),
		AdminPassword:     getEnv("ADMIN_PASSWORD", ""),
		LicenseKey:        getEnv("LICENSE_KEY", ""),
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
