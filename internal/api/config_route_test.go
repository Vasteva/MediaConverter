package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPostConfigExposesPreviouslyUnreachableFields covers #51: several
// Config fields — MaxConcurrentJobs, ReplaceInPlace, HoldingDir, PUID, PGID,
// GPUVendor, SavingsFloor, DensityFloor — could only ever be set via an env
// var or config.json at startup, with no way to change them through the API
// (and so no way through the Settings UI either) once the process was
// running. This round-trips each through POST then GET and confirms the
// change stuck.
func TestPostConfigExposesPreviouslyUnreachableFields(t *testing.T) {
	app, token, _ := newTestApp(t)

	body, _ := json.Marshal(map[string]any{
		"gpuVendor":         "nvidia",
		"maxConcurrentJobs": 4,
		"replaceInPlace":    true,
		"holdingDir":        "/storage/.held",
		"puid":              1000,
		"pgid":              1000,
		"savingsFloor":      0.2,
		"densityFloor":      0.1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/config: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var postOut struct {
		Success         bool `json:"success"`
		RestartRequired bool `json:"restartRequired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postOut); err != nil {
		t.Fatalf("decoding POST response: %v", err)
	}
	if !postOut.RestartRequired {
		t.Error("expected restartRequired = true when maxConcurrentJobs was included in the request")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}

	var got struct {
		GPUVendor         string  `json:"gpuVendor"`
		MaxConcurrentJobs int     `json:"maxConcurrentJobs"`
		ReplaceInPlace    bool    `json:"replaceInPlace"`
		HoldingDir        string  `json:"holdingDir"`
		PUID              int     `json:"puid"`
		PGID              int     `json:"pgid"`
		SavingsFloor      float64 `json:"savingsFloor"`
		DensityFloor      float64 `json:"densityFloor"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding GET response: %v", err)
	}

	if got.GPUVendor != "nvidia" {
		t.Errorf("gpuVendor = %q, want nvidia", got.GPUVendor)
	}
	if got.MaxConcurrentJobs != 4 {
		t.Errorf("maxConcurrentJobs = %d, want 4", got.MaxConcurrentJobs)
	}
	if !got.ReplaceInPlace {
		t.Error("replaceInPlace = false, want true")
	}
	if got.HoldingDir != "/storage/.held" {
		t.Errorf("holdingDir = %q, want /storage/.held", got.HoldingDir)
	}
	if got.PUID != 1000 {
		t.Errorf("puid = %d, want 1000", got.PUID)
	}
	if got.PGID != 1000 {
		t.Errorf("pgid = %d, want 1000", got.PGID)
	}
	if got.SavingsFloor != 0.2 {
		t.Errorf("savingsFloor = %v, want 0.2", got.SavingsFloor)
	}
	if got.DensityFloor != 0.1 {
		t.Errorf("densityFloor = %v, want 0.1", got.DensityFloor)
	}
}

// TestPostConfigRestartRequiredOnlyForMaxConcurrentJobs confirms the flag
// isn't set on an unrelated, immediately-effective field — it exists
// specifically so the one field that needs a restart doesn't look
// identical to every other field's instant effect (#51).
func TestPostConfigRestartRequiredOnlyForMaxConcurrentJobs(t *testing.T) {
	app, token, _ := newTestApp(t)

	body, _ := json.Marshal(map[string]any{"puid": 1000})
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	var out struct {
		RestartRequired bool `json:"restartRequired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if out.RestartRequired {
		t.Error("restartRequired = true for a puid-only update, want false")
	}
}

// TestPostConfigValidatesNewFields covers the validation added alongside
// each newly-exposed field, mirroring the existing CRF range check.
func TestPostConfigValidatesNewFields(t *testing.T) {
	app, token, _ := newTestApp(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"gpuVendor unknown value", map[string]any{"gpuVendor": "quantum"}},
		{"maxConcurrentJobs zero", map[string]any{"maxConcurrentJobs": 0}},
		{"maxConcurrentJobs negative", map[string]any{"maxConcurrentJobs": -1}},
		{"puid below -1", map[string]any{"puid": -2}},
		{"pgid below -1", map[string]any{"pgid": -2}},
		{"savingsFloor negative", map[string]any{"savingsFloor": -0.1}},
		{"savingsFloor above 1", map[string]any{"savingsFloor": 1.1}},
		{"densityFloor negative", map[string]any{"densityFloor": -0.1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
		})
	}
}
