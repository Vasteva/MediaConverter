package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vasteva/MediaConverter/internal/ai"
	"github.com/Vasteva/MediaConverter/internal/ai/search"
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/jobs"
	"github.com/Vasteva/MediaConverter/internal/license"
	"github.com/Vasteva/MediaConverter/internal/scanner"
	"github.com/Vasteva/MediaConverter/internal/security"
	"github.com/Vasteva/MediaConverter/internal/system"
	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(app *fiber.App, jm *jobs.Manager, fs *scanner.Scanner, cfg *config.Config) {
	if fs != nil {
		jm.OnJobComplete = fs.CompleteProcessed
	}

	api := app.Group("/api", AuthMiddleware(cfg))
	RegisterFSRoutes(api)

	// Setup Wizard
	setup := api.Group("/setup")
	setup.Get("/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"isInitialized": cfg.IsInitialized,
		})
	})

	setup.Get("/probes", func(c *fiber.Ctx) error {
		probes := fiber.Map{
			"gpu": system.DetectGPU(),
		}

		// Check for binaries
		_, err := exec.LookPath("ffmpeg")
		probes["ffmpeg"] = err == nil

		_, err = exec.LookPath("makemkvcon")
		probes["makemkv"] = err == nil

		return c.JSON(probes)
	})

	setup.Post("/complete", func(c *fiber.Ctx) error {
		var req struct {
			AdminPassword string `json:"adminPassword"`
			AIProvider    string `json:"aiProvider"`
			AIApiKey      string `json:"aiApiKey"`
			LicenseKey    string `json:"licenseKey"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		if req.AdminPassword != "" {
			cfg.AdminPassword = req.AdminPassword
		}
		if req.AIProvider != "" {
			cfg.AIProvider = req.AIProvider
		}
		if req.AIApiKey != "" {
			cfg.AIApiKey = req.AIApiKey
		}
		if req.LicenseKey != "" {
			cfg.LicenseKey = req.LicenseKey
			cfg.IsPremium = license.Validate(req.LicenseKey)
		}

		if err := cfg.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
			// Don't fail the request, but log it
		}

		if err := cfg.MarkInitialized(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(fiber.Map{"success": true})
	})

	// Login
	api.Post("/login", func(c *fiber.Ctx) error {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}

		// Check if password is configured
		if cfg.AdminPassword == "" {
			return c.Status(500).JSON(fiber.Map{"error": "Admin password not configured"})
		}

		// Validate password
		if req.Password != cfg.AdminPassword {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid password"})
		}

		// Generate and return token
		token := GenerateToken(cfg.AdminPassword)
		return c.JSON(fiber.Map{
			"success": true,
			"token":   token,
		})
	})

	// Dashboard Stats
	api.Get("/dashboard/stats", func(c *fiber.Ctx) error {
		processed := fs.GetProcessedFiles()
		stats := system.DashboardStats{}

		for _, f := range processed {
			if f.InputSize > 0 && f.OutputSize > 0 {
				saved := f.InputSize - f.OutputSize
				if saved > 0 {
					stats.TotalStorageSaved += saved
				}
			}
			if f.AISubtitles {
				stats.TotalSubtitlesCreated++
			}
			if f.AIUpscale {
				stats.TotalUpscales++
			}
			if f.AICleaned {
				stats.TotalCleaned++
			}
			if f.AISubtitles || f.AIUpscale || f.AICleaned {
				stats.TotalAIJobs++
			}
		}

		// Calculate efficiency (rough score out of 100 based on compression and AI usage)
		if len(processed) > 0 {
			stats.EfficiencyScore = 85.0 + (float64(stats.TotalAIJobs) * 2.0)
			if stats.EfficiencyScore > 100 {
				stats.EfficiencyScore = 100
			}
		}

		return c.JSON(stats)
	})

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

		// Security: Validate paths to prevent arbitrary file access
		sourcePath, err := security.ValidatePath(req.SourcePath, cfg.SourceDir)
		if err != nil {
			return c.Status(403).JSON(fiber.Map{"error": err.Error()})
		}

		destPath := req.DestPath
		if destPath != "" {
			// If destination is specified, clean it
			destPath = filepath.Clean(destPath)
			// Check if it's a directory - if so, use source filename
			if info, err := os.Stat(destPath); err == nil && info.IsDir() {
				destPath = filepath.Join(destPath, filepath.Base(sourcePath))
			}
		} else {
			// Default: same directory as source with _optimized suffix
			sourceDir := filepath.Dir(sourcePath)
			sourceExt := filepath.Ext(sourcePath)
			sourceBase := strings.TrimSuffix(filepath.Base(sourcePath), sourceExt)
			destPath = filepath.Join(sourceDir, sourceBase+"_optimized"+sourceExt)
		}

		job := &jobs.Job{
			ID:              generateID(),
			Type:            req.Type,
			SourcePath:      sourcePath,
			DestinationPath: destPath,
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
			"aiApiKey":      security.MaskKey(cfg.AIApiKey),
			"aiEndpoint":    cfg.AIEndpoint,
			"aiModel":       cfg.AIModel,
			"licenseKey":    security.MaskKey(cfg.LicenseKey),
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

		// Only update keys if they aren't masked patterns
		if req.AIApiKey != "" && !strings.Contains(req.AIApiKey, "....") {
			cfg.AIApiKey = req.AIApiKey
		}
		if req.AIEndpoint != "" {
			cfg.AIEndpoint = req.AIEndpoint
		}
		if req.AIModel != "" {
			cfg.AIModel = req.AIModel
		}

		if req.LicenseKey != "" && !strings.Contains(req.LicenseKey, "....") {
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

		if err := cfg.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}

		return c.JSON(fiber.Map{"success": true})
	})

	// Test AI Connection
	api.Post("/ai/test", func(c *fiber.Ctx) error {
		var req struct {
			Provider string `json:"provider"`
			APIKey   string `json:"apiKey"`
			Endpoint string `json:"endpoint"`
			Model    string `json:"model"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		if req.Provider == "none" || req.Provider == "" {
			return c.JSON(fiber.Map{"success": true, "message": "AI disabled"})
		}

		// Check if the API key is masked (e.g. sent from UI without change)
		apiKey := req.APIKey
		if strings.Contains(apiKey, "....") && len(apiKey) > 8 {
			// If it looks masked, check if it matches the current masked key
			// If so, rely on the stored config key
			if apiKey == security.MaskKey(cfg.AIApiKey) {
				apiKey = cfg.AIApiKey
			}
		}

		// Create temporary provider
		provider, err := ai.NewProvider(ai.AIConfig{
			Provider: req.Provider,
			APIKey:   apiKey,
			Endpoint: req.Endpoint,
			Model:    req.Model,
		})
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		// Test connection
		ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
		defer cancel()

		resp, err := provider.Analyze(ctx, "Reply with 'OK' if you can receive this message.")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Connection failed: %v", err)})
		}

		return c.JSON(fiber.Map{
			"success": true,
			"message": "Connection successful!",
			"reply":   resp,
		})
	})

	// Scanner Config
	// Scanner Status
	api.Get("/scanner/status", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}
		return c.JSON(fs.GetStatus())
	})

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

		// Security: Validate watch directories
		for i, dir := range newCfg.WatchDirectories {
			validPath, err := security.ValidatePath(dir.Path, cfg.SourceDir)
			if err != nil {
				return c.Status(403).JSON(fiber.Map{"error": fmt.Sprintf("Watch directory %d: %v", i, err)})
			}
			newCfg.WatchDirectories[i].Path = validPath
		}

		// Security: Validate output directory
		if newCfg.OutputDirectory != "" {
			validOutput, err := security.ValidatePath(newCfg.OutputDirectory, cfg.DestDir)
			if err != nil {
				return c.Status(403).JSON(fiber.Map{"error": fmt.Sprintf("Output directory: %v", err)})
			}
			newCfg.OutputDirectory = validOutput
		}

		if err := fs.UpdateConfig(&newCfg); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(fiber.Map{"success": true})
	})

	// Trigger Manual Scan
	api.Post("/scanner/scan", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}

		// Run scan asynchronously to avoid blocking
		go func() {
			if err := fs.ScanAll(); err != nil {
				log.Printf("[Scanner] Manual scan failed: %v", err)
			}
		}()

		return c.JSON(fiber.Map{"success": true, "message": "Scan started"})
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
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			// Fallback to less secure but functional method
			b[i] = letters[i%len(letters)]
			continue
		}
		b[i] = letters[idx.Int64()]
	}
	return string(b)
}
