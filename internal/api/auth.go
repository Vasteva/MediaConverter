package api

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/gofiber/fiber/v2"
)

// AuthMiddleware creates a middleware that checks for a valid session token
func AuthMiddleware(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip for health check, login, and setup status
		path := c.Path()
		if path == "/api/health" || path == "/api/login" || path == "/api/setup/status" {
			return c.Next()
		}

		// If not initialized, allow all setup routes
		if !cfg.IsInitialized && (path == "/api/setup/probes" || path == "/api/setup/complete") {
			return c.Next()
		}

		// Check for Authorization header
		auth := c.Get("Authorization")
		if auth == "" {
			return c.Status(401).JSON(fiber.Map{"error": "Unauthorized: Missing token"})
		}

		token := ""
		if len(auth) > 7 && auth[:7] == "Bearer " {
			token = auth[7:]
		} else {
			token = auth
		}

		// Validate token
		// For simplicity without a database, we compare it against a hash of the admin password
		// In a real production app, you'd use JWT or a proper session store.
		if !validateToken(token, cfg.AdminPassword) {
			return c.Status(401).JSON(fiber.Map{"error": "Unauthorized: Invalid token"})
		}

		return c.Next()
	}
}

// GenerateToken creates a simple token for the session
func GenerateToken(password string) string {
	// A simple token could be a hash of the password + today's date
	// This makes it valid for the current day only.
	salt := time.Now().Format("2006-01-02")
	hash := sha256.Sum256([]byte(password + salt))
	return fmt.Sprintf("%x", hash)
}

func validateToken(token, password string) bool {
	if password == "" {
		// If no password set, we might want to allow all or reject all.
		// For security in a public repo, let's reject if password is empty but auth is requested.
		return false
	}

	// Token is valid if it matches today's or yesterday's hash (to handle day crossovers)
	salt1 := time.Now().Format("2006-01-02")
	hash1 := sha256.Sum256([]byte(password + salt1))
	if token == fmt.Sprintf("%x", hash1) {
		return true
	}

	salt2 := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	hash2 := sha256.Sum256([]byte(password + salt2))
	if token == fmt.Sprintf("%x", hash2) {
		return true
	}

	return false
}
