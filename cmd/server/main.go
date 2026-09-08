package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"

	vastiva "github.com/Vasteva/MediaConverter"
	"github.com/Vasteva/MediaConverter/internal/ai"
	"github.com/Vasteva/MediaConverter/internal/api"
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/jobs"
	"github.com/Vasteva/MediaConverter/internal/scanner"
)

func main() {
	// Load environment variables
	_ = godotenv.Load()

	// Initialize configuration
	cfg := config.Load()

	// Warn if configured directories are not accessible (e.g. missing volume mounts)
	for _, dir := range []struct{ name, path string }{
		{"SOURCE_DIR", cfg.SourceDir},
		{"DEST_DIR", cfg.DestDir},
	} {
		if _, err := os.Stat(dir.path); err != nil {
			log.Printf("WARNING: %s (%s) is not accessible: %v — check your volume mounts", dir.name, dir.path, err)
		}
	}

	// Initialize AI Provider
	aiProvider, err := ai.NewProvider(ai.AIConfig{
		Provider: cfg.AIProvider,
		APIKey:   cfg.AIApiKey,
		Endpoint: cfg.AIEndpoint,
		Model:    cfg.AIModel,
	})
	if err != nil {
		log.Printf("Warning: Failed to initialize AI provider: %v", err)
	} else if aiProvider != nil {
		log.Printf("Initialized AI provider: %s", aiProvider.GetName())
	}

	// Initialize job manager
	jobsFile := os.Getenv("JOBS_FILE")
	if jobsFile == "" {
		jobsFile = "/data/jobs.json"
	}
	jobManager, err := jobs.NewManager(cfg, aiProvider, jobsFile)
	if err != nil {
		log.Fatalf("Failed to initialize job manager: %v", err)
	}
	go jobManager.Start()
	go jobManager.RequeuePendingJobs() // Requeue any pending jobs from previous session

	// Initialize file scanner
	watchDirsFile := os.Getenv("SCANNER_CONFIG_FILE")
	if watchDirsFile == "" {
		watchDirsFile = "/data/scanner_config.json"
	}

	scannerCfg, err := scanner.LoadScannerConfig(cfg, watchDirsFile)
	if err != nil {
		log.Printf("Warning: Failed to load scanner config: %v", err)
		log.Println("Scanner will be disabled")
		scannerCfg = &scanner.ScannerConfig{Enabled: false}
	}

	fileScanner, err := scanner.NewScanner(scannerCfg, jobManager, watchDirsFile)
	if err != nil {
		log.Printf("Warning: Failed to initialize scanner: %v", err)
	} else if scannerCfg.Enabled {
		if err := fileScanner.Start(); err != nil {
			log.Printf("Warning: Failed to start scanner: %v", err)
		}
	}

	// Initialize Fiber app
	//
	// Without TrustedProxies, c.IP() behind a reverse proxy (Traefik, in the
	// documented deployment) always returns the proxy's own address, since
	// that's the actual TCP peer — every client collapses into one IP as far
	// as the login rate limiter is concerned, so it protects nobody (#50).
	// TRUSTED_PROXY_CIDRS opts into resolving the real client IP from
	// X-Forwarded-For instead, but only when the proxy is trusted — Fiber
	// falls back to the raw peer address for any connection that doesn't
	// come from one of these addresses/ranges, which matters here because
	// the container's port is also published directly (docker-compose.yml),
	// so the header can't simply be trusted unconditionally: a direct
	// connection could set X-Forwarded-For itself to spoof a fresh IP on
	// every request and bypass the limiter entirely. Left unset (the
	// default), behavior is unchanged from before this fix.
	trustedProxies := splitAndTrim(os.Getenv("TRUSTED_PROXY_CIDRS"))
	app := fiber.New(fiber.Config{
		AppName:                 "Vastiva v1.0.0",
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: len(trustedProxies) > 0,
		TrustedProxies:          trustedProxies,
	})

	// Middleware
	app.Use(logger.New())

	// Configure CORS with allowed origins from environment
	corsOrigins := os.Getenv("CORS_ORIGINS")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:5173,http://localhost:3000" // Dev defaults
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: true,
	}))

	// API routes
	api.RegisterRoutes(app, jobManager, fileScanner, cfg)

	// Serve static files from embedded FS
	app.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(vastiva.StaticFS),
		PathPrefix: "web/dist",
		Browse:     false,
		Index:      "index.html",
	}))

	// Fallback to index.html for SPA routing
	app.Get("*", func(c *fiber.Ctx) error {
		file, err := vastiva.StaticFS.ReadFile("web/dist/index.html")
		if err != nil {
			return c.SendStatus(http.StatusNotFound)
		}
		c.Set("Content-Type", "text/html")
		c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		return c.Send(file)
	})

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down gracefully...")
		if fileScanner != nil {
			fileScanner.Stop()
		}
		jobManager.Stop()
		_ = app.Shutdown()
	}()

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Vastiva starting on :%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// splitAndTrim splits a comma-separated list and drops empty/whitespace
// entries, so an unset or empty env var yields an empty (not nil-vs-empty
// ambiguous) slice rather than a single blank string.
func splitAndTrim(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
