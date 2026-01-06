package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rwurtz/vastiva/internal/config"
	"github.com/rwurtz/vastiva/internal/jobs"
)

func RegisterRoutes(app *fiber.App, jm *jobs.Manager, cfg *config.Config) {
	api := app.Group("/api")

	// Health check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "time": time.Now()})
	})

	// Jobs
	api.Get("/jobs", func(c *fiber.Ctx) error {
		return c.JSON(jm.GetAllJobs())
	})

	api.Post("/jobs", func(c *fiber.Ctx) error {
		var req struct {
			Type        jobs.JobType `json:"type"`
			SourcePath  string       `json:"sourcePath"`
			DestPath    string       `json:"destinationPath"`
			Priority    int          `json:"priority"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		job := &jobs.Job{
			ID:              generateID(),
			Type:            req.Type,
			SourcePath:      req.SourcePath,
			DestinationPath: req.DestPath,
			Status:          jobs.StatusPending,
			Priority:        req.Priority,
			CreatedAt:       time.Now(),
		}
		jm.AddJob(job)
		return c.Status(201).JSON(job)
	})

	api.Get("/jobs/:id", func(c *fiber.Ctx) error {
		job := jm.GetJob(c.Params("id"))
		if job == nil {
			return c.Status(404).JSON(fiber.Map{"error": "Job not found"})
		}
		return c.JSON(job)
	})

	api.Delete("/jobs/:id", func(c *fiber.Ctx) error {
		if jm.CancelJob(c.Params("id")) {
			return c.JSON(fiber.Map{"success": true})
		}
		return c.Status(404).JSON(fiber.Map{"error": "Job not found"})
	})

	// Config
	api.Get("/config", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"sourceDir":     cfg.SourceDir,
			"destDir":       cfg.DestDir,
			"gpuVendor":     cfg.GPUVendor,
			"qualityPreset": cfg.QualityPreset,
			"crf":           cfg.CRF,
			"aiProvider":    cfg.AIProvider,
		})
	})
}

func generateID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(6)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
