package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestPostJobsValidatesDestinationPath covers #37: destinationPath used to
// be filepath.Clean'd only, with no bound on where the result could actually
// land — an arbitrary file write. It must now be confined the same way
// sourcePath already was.
func TestPostJobsValidatesDestinationPath(t *testing.T) {
	app, token, _ := newTestApp(t)
	outsideDir := t.TempDir() // a real directory, but not SourceDir or DestDir

	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{
			name: "destination outside both allowed directories is rejected",
			body: map[string]any{
				"type":            "optimize",
				"sourcePath":      "movie.mkv",
				"destinationPath": filepath.Join(outsideDir, "evil.mkv"),
			},
			wantStatus: 403,
		},
		{
			name: "no destination given still creates the job normally",
			body: map[string]any{
				"type":       "optimize",
				"sourcePath": "movie.mkv",
			},
			wantStatus: 201,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// TestPostScannerQueueValidatesPath covers #37: POST /api/scanner/queue
// passed each path straight to Scanner.QueueFile with no validation at all.
// The endpoint reports per-path failures in a 200 rather than an HTTP error
// status, since it's a batch operation — so the fix is verified in the
// response body, not the status code.
func TestPostScannerQueueValidatesPath(t *testing.T) {
	app, token, dir := newTestApp(t)

	inside := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "evil.mkv")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"paths": []string{inside, outside}})
	req := httptest.NewRequest(http.MethodPost, "/api/scanner/queue", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	var result struct {
		Queued int      `json:"queued"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("parsing response body: %v", err)
	}

	if result.Queued != 1 {
		t.Errorf("queued = %d, want 1 (only the in-sandbox path)", result.Queued)
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors = %v, want exactly one entry for the out-of-sandbox path", result.Errors)
	}
}
