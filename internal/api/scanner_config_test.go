package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/jobs"
	"github.com/Vasteva/MediaConverter/internal/scanner"
	"github.com/gofiber/fiber/v2"
)

// newTestApp wires a real Manager and Scanner against temp-dir files, so
// RegisterRoutes' handlers run against the same objects a live server would
// use rather than mocks. Returns the app, a valid auth token, and the temp
// directory used as both SourceDir and DestDir, for tests that need to
// construct a path known to be inside (or outside) the sandbox.
func newTestApp(t *testing.T) (*fiber.App, string, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		AdminPassword: "test-password",
		IsInitialized: true,
		SourceDir:     dir,
		DestDir:       dir,
	}

	jm, err := jobs.NewManager(cfg, nil, filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("jobs.NewManager: %v", err)
	}

	scannerCfg := &scanner.ScannerConfig{
		Mode:              scanner.ScanModeManual,
		ProcessedFilePath: filepath.Join(dir, "processed.json"),
	}
	scannerCfg.Validate()
	fs, err := scanner.NewScanner(scannerCfg, jm, filepath.Join(dir, "scanner_config.json"))
	if err != nil {
		t.Fatalf("scanner.NewScanner: %v", err)
	}
	t.Cleanup(fs.Stop)

	app := fiber.New()
	RegisterRoutes(app, jm, fs, cfg)
	return app, GenerateToken(cfg.AdminPassword), dir
}

// TestPostScannerConfigRejectsLowScanInterval covers #36: the API must
// reject a ScanIntervalSec below the floor rather than silently repairing
// it, so a bad client input is surfaced instead of hidden.
//
// Enabled is deliberately left false: UpdateConfig only calls Start() (and
// so only spawns periodicScan, the goroutine that actually panicked on
// time.NewTicker(0)) when the posted config is enabled, and repeatedly
// starting/stopping a live scanner across subtests here would exercise the
// scanner's own pre-existing config-access race (#43), a separate, already-
// tracked bug this test isn't the place to trip over. That periodicScan
// itself never receives a bad interval is proven directly and
// deterministically by TestScannerConfigValidateFloorsScanInterval instead.
func TestPostScannerConfigRejectsLowScanInterval(t *testing.T) {
	app, token, _ := newTestApp(t)

	cases := []struct {
		name       string
		interval   int
		wantStatus int
	}{
		{"zero (cleared field) is left to Validate's silent default", 0, 200},
		{"below the floor is rejected", 30, 400},
		{"negative is rejected", -5, 400},
		{"exactly the floor is accepted", scanner.MinScanIntervalSec, 200},
		{"comfortably above the floor is accepted", 900, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(scanner.ScannerConfig{
				Mode:            scanner.ScanModePeriodic,
				ScanIntervalSec: tc.interval,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/scanner/config", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("ScanIntervalSec=%d: status = %d, want %d", tc.interval, resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
