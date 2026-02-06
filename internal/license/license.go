package license

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Validate checks if the provided license key is valid for a premium subscription.
// For the purpose of this demonstration, any key starting with "VASTIVA-PRO-"
// that ends with a correct checksum is considered valid.
func Validate(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	// Simple check: start with prefix
	if !strings.HasPrefix(key, "VASTIVA-PRO-") {
		return false
	}

	parts := strings.Split(key, "-")
	if len(parts) != 4 {
		return false
	}

	// Example: VASTIVA-PRO-USERID-CHECKSUM
	// In a real app, this would check a remote server or use asymmetric signatures.
	// Here we just check if the last part is the first 4 chars of a hash of the previous parts.
	payload := parts[0] + parts[1] + parts[2]
	expected := generateChecksum(payload)

	return strings.EqualFold(parts[3], expected)
}

// Generate creates a valid license key for a given user ID
func Generate(userID string) string {
	userID = strings.ToUpper(strings.TrimSpace(userID))
	if userID == "" {
		userID = "USER"
	}
	prefix := "VASTIVA-PRO-" + userID
	checksum := generateChecksum("VASTIVA" + "PRO" + userID)
	return prefix + "-" + checksum
}

func generateChecksum(payload string) string {
	h := sha256.New()
	h.Write([]byte(payload + "salt-secret"))
	return fmt.Sprintf("%x", h.Sum(nil))[:4]
}

// GetPlanName returns the name of the plan based on the key
func GetPlanName(key string) string {
	if Validate(key) {
		return "Vastiva Pro"
	}
	return "Standard"
}
