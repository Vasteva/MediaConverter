package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func (p *ClaudeProvider) VerifyMedia(ctx context.Context, originalPaths, convertedPaths []string) (bool, error) {
	if len(originalPaths) != len(convertedPaths) {
		return false, fmt.Errorf("mismatched number of images for verification")
	}

	url := "https://api.anthropic.com/v1/messages"

	var content []interface{}

	// Text prompt
	content = append(content, map[string]interface{}{
		"type": "text",
		"text": "I will provide 5 pairs of images. The first of each pair is the original frame, the second is the converted frame. Verify that the visual content is preserved and no significant corruption (green blocks, heavy static, distortion) has occurred. Reply with 'TRUE' if all frames look visually valid and similar. Reply 'FALSE' if there is clear corruption.",
	})

	for i := 0; i < len(originalPaths); i++ {
		// Original
		origBytes, err := os.ReadFile(originalPaths[i])
		if err != nil {
			return false, err
		}
		content = append(content, map[string]interface{}{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": "image/jpeg",
				"data":       base64.StdEncoding.EncodeToString(origBytes),
			},
		})

		// Converted
		convBytes, err := os.ReadFile(convertedPaths[i])
		if err != nil {
			return false, err
		}
		content = append(content, map[string]interface{}{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": "image/jpeg",
				"data":       base64.StdEncoding.EncodeToString(convBytes),
			},
		})
	}

	payload := map[string]interface{}{
		"model":      p.Model,
		"max_tokens": 100,
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": content,
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("claude api error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}

	if len(result.Content) > 0 {
		text := strings.TrimSpace(strings.ToUpper(result.Content[0].Text))
		return strings.Contains(text, "TRUE"), nil
	}

	return false, fmt.Errorf("no response from claude")
}
