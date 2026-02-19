package ai

import (
	"context"
	"fmt"
)

func (p *OllamaProvider) VerifyMedia(ctx context.Context, originalPaths, convertedPaths []string) (bool, error) {
	return false, fmt.Errorf("VerifyMedia not implemented for Ollama")
}
