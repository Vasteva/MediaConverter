package ai

import (
	"context"
	"fmt"
)

// Provider defines the interface for AI backends
type Provider interface {
	// Analyze asks the AI to analyze a prompt (optionally with context)
	Analyze(ctx context.Context, prompt string) (string, error)
	// GetName returns the provider name
	GetName() string
}

// Config holds settings for AI providers
type AIConfig struct {
	Provider string
	APIKey   string
	Endpoint string
	Model    string
}

// NewProvider creates a new AI provider based on configuration
func NewProvider(cfg AIConfig) (Provider, error) {
	switch cfg.Provider {
	case "gemini":
		return NewGeminiProvider(cfg.APIKey, cfg.Model), nil
	case "openai":
		return NewOpenAIProvider(cfg.APIKey, cfg.Endpoint, cfg.Model), nil
	case "claude":
		return NewClaudeProvider(cfg.APIKey, cfg.Model), nil
	case "ollama":
		return NewOllamaProvider(cfg.Endpoint, cfg.Model), nil
	case "none", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}
}
