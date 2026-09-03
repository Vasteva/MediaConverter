package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set test environment variables
	t.Setenv("PORT", "9090")
	t.Setenv("SOURCE_DIR", "/tmp/source")
	t.Setenv("DEST_DIR", "/tmp/dest")
	t.Setenv("GPU_VENDOR", "nvidia")
	t.Setenv("QUALITY_PRESET", "fast")
	t.Setenv("CRF", "18")
	t.Setenv("MAX_CONCURRENT_JOBS", "4")
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("ADMIN_PASSWORD", "supersecret")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("expected Port 9090, got %s", cfg.Port)
	}
	if cfg.SourceDir != "/tmp/source" {
		t.Errorf("expected SourceDir /tmp/source, got %s", cfg.SourceDir)
	}
	if cfg.GPUVendor != "nvidia" {
		t.Errorf("expected GPUVendor nvidia, got %s", cfg.GPUVendor)
	}
	if cfg.CRF != 18 {
		t.Errorf("expected CRF 18, got %d", cfg.CRF)
	}
	if cfg.MaxConcurrentJobs != 4 {
		t.Errorf("expected MaxConcurrentJobs 4, got %d", cfg.MaxConcurrentJobs)
	}
	if cfg.AIProvider != "openai" {
		t.Errorf("expected AIProvider openai, got %s", cfg.AIProvider)
	}
	if cfg.AdminPassword != "supersecret" {
		t.Errorf("expected AdminPassword supersecret, got %s", cfg.AdminPassword)
	}
}

func TestGetEnvDefaults(t *testing.T) {
	// Clear relevant env vars
	os.Unsetenv("PORT")
	os.Unsetenv("SCANNER_ENABLED")

	cfg := Load()

	if cfg.Port != "8080" { // Default value
		t.Errorf("expected default Port 8080, got %s", cfg.Port)
	}
	if cfg.ScannerEnabled != false {
		t.Errorf("expected default ScannerEnabled false, got %v", cfg.ScannerEnabled)
	}
}

// TestScheduleAlwaysMarshalsAllowedDaysAsArray covers the crash that blanked
// the settings page.
//
// A nil []int marshals to JSON null, and the zero-value schedule — any config
// that never had one set — hit that path. The settings UI calls
// schedule.allowedDays.includes(...) directly, so null threw a TypeError as
// soon as the day picker rendered, unmounting the page.
func TestScheduleAlwaysMarshalsAllowedDaysAsArray(t *testing.T) {
	cases := []struct {
		name string
		in   ProcessingSchedule
		want string
	}{
		{
			name: "zero value",
			in:   ProcessingSchedule{},
			want: `"allowedDays":[]`,
		},
		{
			name: "explicitly nil",
			in:   ProcessingSchedule{Enabled: true, AllowedDays: nil},
			want: `"allowedDays":[]`,
		},
		{
			name: "empty slice",
			in:   ProcessingSchedule{AllowedDays: []int{}},
			want: `"allowedDays":[]`,
		},
		{
			name: "populated is untouched",
			in:   ProcessingSchedule{AllowedDays: []int{1, 2, 3}},
			want: `"allowedDays":[1,2,3]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("got %s, want it to contain %s", data, tc.want)
			}
			if strings.Contains(string(data), "null") {
				t.Errorf("response contains null: %s", data)
			}
		})
	}
}

// The schedule is marshaled as part of the whole config too — via the API
// response and via Save to /data/config.json.
func TestConfigMarshalsScheduleWithoutNull(t *testing.T) {
	data, err := json.Marshal(&Config{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"allowedDays":[]`) {
		t.Errorf("nested schedule should emit an array, got: %s", data)
	}
}

// A persisted config written before this fix contains null; loading it and
// marshaling it again must still produce an array.
func TestScheduleRoundTripFromNull(t *testing.T) {
	var s ProcessingSchedule
	if err := json.Unmarshal([]byte(`{"enabled":true,"allowedDays":null}`), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if s.AllowedDays != nil {
		t.Fatal("precondition: unmarshaling null should leave the slice nil")
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"allowedDays":[]`) {
		t.Errorf("a stale null config must still serialise as an array, got: %s", data)
	}
}
