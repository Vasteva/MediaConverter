package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIProvider struct {
	APIKey   string
	Endpoint string
	Model    string
}

func NewOpenAIProvider(apiKey, endpoint, model string) *OpenAIProvider {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIProvider{APIKey: apiKey, Endpoint: endpoint, Model: model}
}

func (p *OpenAIProvider) GetName() string {
	return "openai"
}

func (p *OpenAIProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/chat/completions", p.Endpoint)

	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": prompt,
			},
		},
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai api error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no response from openai")
}
