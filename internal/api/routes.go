package api

import (
	"log"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rwurtz/vastiva/internal/ai"
	"github.com/rwurtz/vastiva/internal/ai/search"
	"github.com/rwurtz/vastiva/internal/config"
	"github.com/rwurtz/vastiva/internal/jobs"
	"github.com/rwurtz/vastiva/internal/license"
	"github.com/rwurtz/vastiva/internal/scanner"
	"github.com/rwurtz/vastiva/internal/system"
)

func RegisterRoutes(app *fiber.App, jm *jobs.Manager, fs *scanner.Scanner, cfg *config.Config) {
	api := app.Group("/api")

	// System Stats
	api.Get("/stats", func(c *fiber.Ctx) error {
		return c.JSON(system.GetStats())
	})

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
			Type            jobs.JobType `json:"type"`
			SourcePath      string       `json:"sourcePath"`
			DestPath        string       `json:"destinationPath"`
			Priority        int          `json:"priority"`
			CreateSubtitles bool         `json:"createSubtitles"`
			Upscale         bool         `json:"upscale"`
			Resolution      string       `json:"resolution"`
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
			CreateSubtitles: req.CreateSubtitles,
			Upscale:         req.Upscale,
			Resolution:      req.Resolution,
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
			"aiApiKey":      cfg.AIApiKey,
			"aiEndpoint":    cfg.AIEndpoint,
			"aiModel":       cfg.AIModel,
			"licenseKey":    cfg.LicenseKey,
			"isPremium":     cfg.IsPremium,
			"planName":      license.GetPlanName(cfg.LicenseKey),
		})
	})

	api.Post("/config", func(c *fiber.Ctx) error {
		var req struct {
			QualityPreset string `json:"qualityPreset"`
			CRF           int    `json:"crf"`
			AIProvider    string `json:"aiProvider"`
			AIApiKey      string `json:"aiApiKey"`
			AIEndpoint    string `json:"aiEndpoint"`
			AIModel       string `json:"aiModel"`
			LicenseKey    string `json:"licenseKey"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		// Update config
		if req.QualityPreset != "" {
			cfg.QualityPreset = req.QualityPreset
		}
		if req.CRF != 0 {
			cfg.CRF = req.CRF
		}
		if req.AIProvider != "" {
			cfg.AIProvider = req.AIProvider
		}
		cfg.AIApiKey = req.AIApiKey
		cfg.AIEndpoint = req.AIEndpoint
		cfg.AIModel = req.AIModel

		if req.LicenseKey != "" {
			cfg.LicenseKey = req.LicenseKey
			cfg.IsPremium = license.Validate(req.LicenseKey)
		}

		// Re-initialize AI provider in manager
		newAI, err := ai.NewProvider(ai.AIConfig{
			Provider: cfg.AIProvider,
			APIKey:   cfg.AIApiKey,
			Endpoint: cfg.AIEndpoint,
			Model:    cfg.AIModel,
		})
		if err == nil {
			jm.UpdateAIProvider(newAI)
		} else {
			log.Printf("Error updating AI provider: %v", err)
		}

		log.Printf("Configuration updated: AI Provider=%s, Premium=%v", cfg.AIProvider, cfg.IsPremium)
		return c.JSON(fiber.Map{"success": true})
	})

	// Scanner Config
	api.Get("/scanner/config", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}
		return c.JSON(fs.GetConfig())
	})

	api.Post("/scanner/config", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}

		var newCfg scanner.ScannerConfig
		if err := c.BodyParser(&newCfg); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		if err := fs.UpdateConfig(&newCfg); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(fiber.Map{"success": true})
	})

	// AI Search
	api.Get("/search", func(c *fiber.Ctx) error {
		query := c.Query("q")
		if query == "" {
			return c.Status(400).JSON(fiber.Map{"error": "Query is required"})
		}

		if !cfg.IsPremium {
			return c.Status(403).JSON(fiber.Map{"error": "AI Search is a premium feature"})
		}

		aiProv := jm.GetAI()
		if aiProv == nil {
			return c.Status(500).JSON(fiber.Map{"error": "AI provider not configured"})
		}

		// 1. Get all processed files
		files := fs.GetProcessedFiles()
		searchItems := make([]search.MediaItem, len(files))
		for i, f := range files {
			searchItems[i] = search.MediaItem{
				ID:    f.JobID,
				Title: filepath.Base(f.Path),
				Path:  f.Path,
			}
		}

		// 2. Perform AI match
		searcher := search.NewSearcher(aiProv)
		matchingIDs, err := searcher.Match(c.Context(), query, searchItems)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		// 3. Map back to ProcessedFile objects
		results := []scanner.ProcessedFile{}
		idMap := make(map[string]scanner.ProcessedFile)
		for _, f := range files {
			idMap[f.JobID] = f
		}

		for _, id := range matchingIDs {
			if f, ok := idMap[id]; ok {
				results = append(results, f)
			}
		}

		return c.JSON(results)
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
