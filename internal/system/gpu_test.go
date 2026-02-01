package system

import "testing"

func TestContains(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"Hello World", "Hello", true},
		{"Hello World", "world", true}, // Case-insensitive in our implementation
		{"Hello World", "foo", false},
		{"Intel GPU", "Intel", true},
		{"AMD Radeon", "i915", false},
	}
	for _, tt := range tests {
		if got := contains(tt.s, tt.substr); got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestDetectGPU(t *testing.T) {
	// This might return anything depending on the environment,
	// but we can at least ensure it doesn't crash.
	vendor := DetectGPU()
	if vendor == "" {
		t.Error("DetectGPU returned empty string")
	}
}
