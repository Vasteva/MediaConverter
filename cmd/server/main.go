package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"

	"github.com/Vasteva/MediaConverter"
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
	jobManager, err := jobs.NewManager(cfg, aiProvider)
	if err != nil {
		log.Fatalf("Failed to initialize job manager: %v", err)
	}
	go jobManager.Start()

	// Initialize file scanner
	watchDirsFile := os.Getenv("SCANNER_CONFIG_FILE")
	if watchDirsFile == "" {
		watchDirsFile = "./scanner-config.json"
	}

	scannerCfg, err := scanner.LoadScannerConfig(cfg, watchDirsFile)
	if err != nil {
		log.Printf("Warning: Failed to load scanner config: %v", err)
		log.Println("Scanner will be disabled")
		scannerCfg = &scanner.ScannerConfig{Enabled: false}
	}

	fileScanner, err := scanner.NewScanner(scannerCfg, jobManager)
	if err != nil {
		log.Printf("Warning: Failed to initialize scanner: %v", err)
	} else if scannerCfg.Enabled {
		if err := fileScanner.Start(); err != nil {
			log.Printf("Warning: Failed to start scanner: %v", err)
		}
	}

	// Initialize Fiber app
	app := fiber.New(fiber.Config{
		AppName: "Vastiva v1.0.0",
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New())

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
