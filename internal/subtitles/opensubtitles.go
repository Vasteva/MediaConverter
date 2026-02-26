package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	osBaseURL   = "https://api.opensubtitles.com/api/v1"
	osUserAgent = "VastivaMediaConverter v1.0"
)

// Downloader fetches subtitles from the OpenSubtitles REST API v1.
type Downloader struct {
	APIKey   string
	Username string
	Password string
	Language string // ISO 639-1, e.g. "en"
}

// NewDownloader creates a configured Downloader. Language defaults to "en" if empty.
func NewDownloader(apiKey, username, password, language string) *Downloader {
	if language == "" {
		language = "en"
	}
	return &Downloader{
		APIKey:   apiKey,
		Username: username,
		Password: password,
		Language: language,
	}
}

// ParseTitle extracts a best-effort title and year from a media filename.
// Handles both AI-cleaned ("The Matrix (1999).mkv") and raw ("The.Matrix.1999.1080p.BluRay.mkv") forms.
func ParseTitle(filename string) (title, year string) {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	// AI-cleaned: "Some Title (2001)"
	if m := regexp.MustCompile(`^(.+?)\s*\((\d{4})\)\s*$`).FindStringSubmatch(base); m != nil {
		return strings.TrimSpace(m[1]), m[2]
	}

	// Raw: year buried in filename
	yearRe := regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	if loc := yearRe.FindStringIndex(base); loc != nil {
		year = base[loc[0]:loc[1]]
		raw := base[:loc[0]]
		raw = regexp.MustCompile(`[.\-_]+`).ReplaceAllString(raw, " ")
		// Remove quality/format tokens and everything after them
		raw = regexp.MustCompile(`(?i)\s*(1080p|720p|4k|2160p|hdr|sdr|blu.?ray|web.?rip|web.?dl|hdtv|hevc|h\.?265|x265|h\.?264|x264|avc|aac|dts|flac|ac3|remux|repack|proper).*`).ReplaceAllString(raw, "")
		return strings.TrimSpace(raw), year
	}

	// Fallback: clean separators and return whole base
	return strings.TrimSpace(regexp.MustCompile(`[.\-_]+`).ReplaceAllString(base, " ")), ""
}

// Download searches for and downloads the best matching subtitle for the given media file.
// Returns the SRT content as a string, or an error if nothing was found.
func (d *Downloader) Download(ctx context.Context, videoPath string) (string, error) {
	if d.APIKey == "" {
		return "", fmt.Errorf("OpenSubtitles API key not configured")
	}

	title, year := ParseTitle(videoPath)
	if title == "" {
		return "", fmt.Errorf("could not parse title from filename %q", filepath.Base(videoPath))
	}

	token, err := d.login(ctx)
	if err != nil {
		return "", fmt.Errorf("OpenSubtitles login failed: %v", err)
	}

	fileID, err := d.search(ctx, token, title, year)
	if err != nil {
		return "", err
	}

	dlURL, err := d.requestDownload(ctx, token, fileID)
	if err != nil {
		return "", err
	}

	return d.fetchContent(ctx, dlURL)
}

// login authenticates with username/password and returns a bearer token.
// Returns an empty string (anonymous mode) when credentials are not configured.
func (d *Downloader) login(ctx context.Context) (string, error) {
	if d.Username == "" || d.Password == "" {
		return "", nil
	}

	payload, _ := json.Marshal(map[string]string{
		"username": d.Username,
		"password": d.Password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", osBaseURL+"/login", bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	d.applyHeaders(req, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// search finds subtitles for the given title/year and returns the best file_id.
func (d *Downloader) search(ctx context.Context, token, title, year string) (int, error) {
	params := url.Values{}
	params.Set("query", title)
	params.Set("languages", d.Language)
	if year != "" {
		params.Set("year", year)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", osBaseURL+"/subtitles?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}
	d.applyHeaders(req, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("search status %d: %s", resp.StatusCode, b)
	}

	var out struct {
		TotalCount int `json:"total_count"`
		Data       []struct {
			Attributes struct {
				DownloadCount int `json:"download_count"`
				Files         []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if len(out.Data) == 0 {
		return 0, fmt.Errorf("no subtitles found for %q (year %s, language %s)", title, year, d.Language)
	}

	// Pick the entry with the highest community download count
	best := out.Data[0]
	for _, item := range out.Data[1:] {
		if item.Attributes.DownloadCount > best.Attributes.DownloadCount {
			best = item
		}
	}
	if len(best.Attributes.Files) == 0 {
		return 0, fmt.Errorf("subtitle entry has no files")
	}
	return best.Attributes.Files[0].FileID, nil
}

// requestDownload exchanges a file_id for a temporary download URL.
func (d *Downloader) requestDownload(ctx context.Context, token string, fileID int) (string, error) {
	reqPayload, _ := json.Marshal(map[string]int{"file_id": fileID})
	req, err := http.NewRequestWithContext(ctx, "POST", osBaseURL+"/download", bytes.NewBuffer(reqPayload))
	if err != nil {
		return "", err
	}
	d.applyHeaders(req, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download request status %d: %s", resp.StatusCode, b)
	}

	var out struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Link == "" {
		return "", fmt.Errorf("download response contained no link (daily quota may be exhausted)")
	}
	return out.Link, nil
}

// fetchContent retrieves the subtitle file content from a download URL.
func (d *Downloader) fetchContent(ctx context.Context, dlURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", osUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("content fetch status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *Downloader) applyHeaders(req *http.Request, token string) {
	req.Header.Set("Api-Key", d.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", osUserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}
