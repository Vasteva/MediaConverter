package api

import (
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/gofiber/fiber/v2"
)

// AuthMiddleware creates a middleware that checks for a valid session token.
// See SessionStore for why tokens are opaque and validated against a
// server-side store rather than derived from the admin password (#50).
func AuthMiddleware(cfg *config.Config, sessions *SessionStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// One snapshot for the whole request: cfg is shared, unsynchronized,
		// with every HTTP handler that writes it and every job-worker
		// goroutine that reads it (#43).
		snap := cfg.Snapshot()

		// Skip for health check, login, and setup status
		path := c.Path()
		if path == "/api/health" || path == "/api/login" || path == "/api/setup/status" {
			return c.Next()
		}

		// If not initialized, allow all setup routes
		if !snap.IsInitialized && (path == "/api/setup/probes" || path == "/api/setup/complete" || path == "/api/setup/test-ai") {
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

		if !sessions.Valid(token) {
			return c.Status(401).JSON(fiber.Map{"error": "Unauthorized: Invalid token"})
		}

		// Stashed for handlers that need to act on the caller's own token —
		// currently just POST /api/logout, to revoke it.
		c.Locals("authToken", token)

		return c.Next()
	}
}
