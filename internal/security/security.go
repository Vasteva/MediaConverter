package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath ensures a given path is within one of the allowed base directories
func ValidatePath(path string, allowedBases ...string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Clean the path to resolve any .. or other tricks
	cleanPath := filepath.Clean(path)

	// If the path is relative, we assume it's relative to the first allowed base
	// or we should reject it if we want strictness.
	// For this app, let's be strict: if it's not absolute, we check it against bases.

	for _, base := range allowedBases {
		if base == "" {
			continue
		}

		absBase, err := filepath.Abs(base)
		if err != nil {
			continue
		}

		// Calculate the absolute path of the target
		var absTarget string
		if filepath.IsAbs(cleanPath) {
			absTarget = cleanPath
		} else {
			absTarget = filepath.Join(absBase, cleanPath)
		}

		// Ensure the target is actually inside the base
		if strings.HasPrefix(absTarget, absBase) {
			return absTarget, nil
		}
	}

	return "", fmt.Errorf("access denied: path %s is outside allowed directories", path)
}

// MaskKey hides most segments of a sensitive key
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "...." + key[len(key)-4:]
}
