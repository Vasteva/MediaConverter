package api

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestGenerateToken(t *testing.T) {
	password := "testpass"
	token := GenerateToken(password)

	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Verify manually
	salt := time.Now().Format("2006-01-02")
	hash := sha256.Sum256([]byte(password + salt))
	expected := fmt.Sprintf("%x", hash)

	if token != expected {
		t.Errorf("expected token %s, got %s", expected, token)
	}
}

func TestValidateToken(t *testing.T) {
	password := "testpass"
	validToken := GenerateToken(password)

	tests := []struct {
		name     string
		token    string
		password string
		want     bool
	}{
		{
			name:     "valid token",
			token:    validToken,
			password: password,
			want:     true,
		},
		{
			name:     "invalid token",
			token:    "wrongtoken",
			password: password,
			want:     false,
		},
		{
			name:     "empty password",
			token:    validToken,
			password: "",
			want:     false,
		},
		{
			name:     "yesterday's token",
			token:    getYesterdaysToken(password),
			password: password,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateToken(tt.token, tt.password); got != tt.want {
				t.Errorf("validateToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func getYesterdaysToken(password string) string {
	salt := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	hash := sha256.Sum256([]byte(password + salt))
	return fmt.Sprintf("%x", hash)
}
