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
	h := sha256.New()
	h.Write([]byte(payload + "salt-secret"))
	expected := fmt.Sprintf("%x", h.Sum(nil))[:4]

	return strings.EqualFold(parts[3], expected)
}

// GetPlanName returns the name of the plan based on the key
func GetPlanName(key string) string {
	if Validate(key) {
		return "Vastiva Pro"
	}
	return "Standard"
}
