package ai

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// httpClient is shared by every provider's HTTP calls. http.DefaultClient has
// no Timeout at all, so an unresponsive endpoint — most concretely a local
// Ollama instance that has hung — blocked its caller forever; on the job
// path (#49) that meant a stuck worker, not just a stuck request. 120s is
// generous enough for a slow local model's chat completion or a Whisper
// transcription request, while still bounding the wait to something finite.
var httpClient = &http.Client{Timeout: 120 * time.Second}

// Provider defines the interface for AI backends
type Provider interface {
	// Analyze asks the AI to analyze a prompt (optionally with context)
	Analyze(ctx context.Context, prompt string) (string, error)
	// Transcribe converts audio/video to text (SRT format preferred)
	Transcribe(ctx context.Context, audioPath string) (string, error)
	// VerifyMedia compares original and converted media frames for integrity
	VerifyMedia(ctx context.Context, originalPaths, convertedPaths []string) (bool, error)
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
