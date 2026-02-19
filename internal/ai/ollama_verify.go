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

func (p *OllamaProvider) VerifyMedia(ctx context.Context, originalPaths, convertedPaths []string) (bool, error) {
	if len(originalPaths) != len(convertedPaths) {
		return false, fmt.Errorf("mismatched number of images for verification")
	}

	url := fmt.Sprintf("%s/api/generate", p.Endpoint)

	var images []string
	for _, path := range originalPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		images = append(images, base64.StdEncoding.EncodeToString(data))
	}
	for _, path := range convertedPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		images = append(images, base64.StdEncoding.EncodeToString(data))
	}

	prompt := "I am providing 5 pairs of images. The first 5 are original frames, the next 5 are the corresponding converted frames. Verify that the visual content is preserved and no significant corruption (green blocks, heavy static, distortion) has occurred. Reply with 'TRUE' if all frames look visually valid and similar. Reply 'FALSE' if there is clear corruption."

	payload := map[string]interface{}{
		"model":  p.Model,
		"prompt": prompt,
		"images": images,
		"stream": false,
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama api error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Response string `json:"response"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}

	text := strings.TrimSpace(strings.ToUpper(result.Response))
	return strings.Contains(text, "TRUE"), nil
}
