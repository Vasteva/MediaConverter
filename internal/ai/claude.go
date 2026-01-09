package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ClaudeProvider struct {
	APIKey string
	Model  string
}

func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	if model == "" {
		model = "claude-3-5-sonnet-20240620"
	}
	return &ClaudeProvider{APIKey: apiKey, Model: model}
}

func (p *ClaudeProvider) GetName() string {
	return "claude"
}

func (p *ClaudeProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	url := "https://api.anthropic.com/v1/messages"

	payload := map[string]interface{}{
		"model":      p.Model,
		"max_tokens": 1024,
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
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude api error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}

	return "", fmt.Errorf("no response from claude")
}
