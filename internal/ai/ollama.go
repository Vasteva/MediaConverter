package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OllamaProvider struct {
	Endpoint string
	Model    string
}

func NewOllamaProvider(endpoint, model string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3"
	}
	return &OllamaProvider{Endpoint: endpoint, Model: model}
}

func (p *OllamaProvider) GetName() string {
	return "ollama"
}

func (p *OllamaProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/api/generate", p.Endpoint)

	payload := map[string]interface{}{
		"model":  p.Model,
		"prompt": prompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama api error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Response string `json:"response"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Response, nil
}
