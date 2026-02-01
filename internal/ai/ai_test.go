package ai

import "testing"

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{"gemini", "gemini", false},
		{"openai", "openai", false},
		{"claude", "claude", false},
		{"ollama", "ollama", false},
		{"none", "none", false},
		{"empty", "", false},
		{"unsupported", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProvider(AIConfig{Provider: tt.provider})
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
