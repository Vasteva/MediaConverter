package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"

	"github.com/rwurtz/vastiva/internal/api"
	"github.com/rwurtz/vastiva/internal/config"
	"github.com/rwurtz/vastiva/internal/jobs"
)

func main() {
	// Load environment variables
	_ = godotenv.Load()

	// Initialize configuration
	cfg := config.Load()

	// Initialize job manager
	jobManager := jobs.NewManager(cfg.MaxConcurrentJobs)
	go jobManager.Start()

	// Initialize Fiber app
	app := fiber.New(fiber.Config{
		AppName: "Vastiva v1.0.0",
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New())

	// API routes
	api.RegisterRoutes(app, jobManager, cfg)

	// Serve static frontend files
	app.Static("/", "./web/dist")

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down gracefully...")
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
