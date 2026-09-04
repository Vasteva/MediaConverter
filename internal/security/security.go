package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath ensures a given path is within one of the allowed base
// directories, resolving symlinks on both sides first.
//
// filepath.Clean only rewrites the path string — it never touches the
// filesystem — so a lexically-contained path can still point outside the
// sandbox via a symlink: a file inside an allowed directory whose target is
// elsewhere, or the allowed directory itself being a symlink (an NFS mount
// point, for instance). Resolving both the target and each base with
// resolveSymlinks before comparing closes that gap; the containment check
// itself is unchanged.
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
		resolvedBase, err := resolveSymlinks(absBase)
		if err != nil {
			// Can't establish where this base actually points — treat it as
			// unusable rather than falling back to trusting it unresolved.
			continue
		}

		// Calculate the absolute path of the target
		var absTarget string
		if filepath.IsAbs(cleanPath) {
			absTarget = cleanPath
		} else {
			absTarget = filepath.Join(absBase, cleanPath)
		}

		resolvedTarget, err := resolveSymlinks(absTarget)
		if err != nil {
			continue
		}

		// Ensure the target is actually inside the base
		if resolvedTarget == resolvedBase || strings.HasPrefix(resolvedTarget, resolvedBase+string(filepath.Separator)) {
			return resolvedTarget, nil
		}
	}

	return "", fmt.Errorf("access denied: path %s is outside allowed directories", path)
}

// resolveSymlinks resolves path to its real, symlink-free form.
//
// filepath.EvalSymlinks requires every component to exist, which a
// destination file being created for the first time never does. This walks
// up to the nearest existing ancestor, resolves that, and rejoins the
// remaining (not-yet-existing) components onto it — so a brand new output
// file is still checked against where its parent directory actually points,
// not just its lexical path, without requiring the file itself to exist yet.
func resolveSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	dir, base := filepath.Split(path)
	dir = filepath.Clean(dir)
	if dir == path {
		// Reached the root without finding anything that exists.
		return "", err
	}

	resolvedDir, dirErr := resolveSymlinks(dir)
	if dirErr != nil {
		return "", dirErr
	}
	return filepath.Join(resolvedDir, base), nil
}

// MaskKey hides most segments of a sensitive key
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "...." + key[len(key)-4:]
}
