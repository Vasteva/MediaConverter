package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
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
	"github.com/Vasteva/MediaConverter/internal/util"
	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(app *fiber.App, jm *jobs.Manager, fs *scanner.Scanner, cfg *config.Config) {
	if fs != nil {
		jm.OnJobComplete = fs.CompleteProcessed
		jm.OnOutputClaimed = fs.MarkOutputInFlight
		jm.OnOutputReleased = fs.ReleaseOutput
	}

	// One store for every issued token — login sessions and short-lived SSE
	// tokens alike (#50). See SessionStore for why.
	sessions := NewSessionStore()

	// Wire SSE broadcaster so every job state change is pushed to connected clients.
	broadcaster := NewSSEBroadcaster()
	if jm != nil {
		jm.OnJobUpdate = broadcaster.Broadcast
	}
	RegisterSSERoute(app, broadcaster, jm, sessions)

	api := app.Group("/api", AuthMiddleware(cfg, sessions))
	RegisterFSRoutes(api, cfg)

	// Setup Wizard
	setup := api.Group("/setup")
	setup.Get("/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"isInitialized": cfg.Snapshot().IsInitialized,
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
			AdminPassword    string `json:"adminPassword"`
			AIProvider       string `json:"aiProvider"`
			AIApiKey         string `json:"aiApiKey"`
			AIEndpoint       string `json:"aiEndpoint"`
			AIModel          string `json:"aiModel"`
			LicenseKey       string `json:"licenseKey"`
			GPUVendor        string `json:"gpuVendor"`
			QualityPreset    string `json:"qualityPreset"`
			CRF              int    `json:"crf"`
			SubtitleMode     string `json:"subtitleMode"`
			SubtitleLang     string `json:"subtitleLang"`
			SubtitleAPIKey   string `json:"subtitleApiKey"`
			SubtitleUsername string `json:"subtitleUsername"`
			SubtitlePassword string `json:"subtitlePassword"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		// One WithLock for every field this request touches, so a concurrent
		// reader's Snapshot() sees either all of these changes or none of
		// them — never, say, the new AdminPassword with the old IsPremium
		// mid-update (#43).
		cfg.WithLock(func() {
			if req.AdminPassword != "" {
				cfg.AdminPassword = req.AdminPassword
			}
			if req.AIProvider != "" {
				cfg.AIProvider = req.AIProvider
			}
			if req.AIApiKey != "" {
				cfg.AIApiKey = req.AIApiKey
			}
			if req.AIEndpoint != "" {
				cfg.AIEndpoint = req.AIEndpoint
			}
			if req.AIModel != "" {
				cfg.AIModel = req.AIModel
			}
			if req.LicenseKey != "" {
				cfg.LicenseKey = req.LicenseKey
				cfg.IsPremium = license.Validate(req.LicenseKey)
			}
			if req.GPUVendor != "" {
				cfg.GPUVendor = req.GPUVendor
			}
			if req.QualityPreset != "" {
				cfg.QualityPreset = req.QualityPreset
			}
			if req.CRF != 0 {
				cfg.CRF = req.CRF
			}
			if req.SubtitleMode != "" {
				cfg.SubtitleMode = req.SubtitleMode
			}
			if req.SubtitleLang != "" {
				cfg.SubtitleLang = req.SubtitleLang
			}
			if req.SubtitleAPIKey != "" {
				cfg.SubtitleAPIKey = req.SubtitleAPIKey
			}
			if req.SubtitleUsername != "" {
				cfg.SubtitleUsername = req.SubtitleUsername
			}
			if req.SubtitlePassword != "" {
				cfg.SubtitlePassword = req.SubtitlePassword
			}
		})

		if err := cfg.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
			// Don't fail the request, but log it
		}

		if err := cfg.MarkInitialized(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(fiber.Map{"success": true})
	})

	setup.Post("/test-ai", func(c *fiber.Ctx) error {
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

		provider, err := ai.NewProvider(ai.AIConfig{
			Provider: req.Provider,
			APIKey:   req.APIKey,
			Endpoint: req.Endpoint,
			Model:    req.Model,
		})
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

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

	// Initialize rate limiter for login
	loginLimiter := NewRateLimiter()

	// Login
	api.Post("/login", func(c *fiber.Ctx) error {
		ip := c.IP()
		if !loginLimiter.Check(ip) {
			log.Printf("[Auth] Rate limit exceeded for IP: %s", ip)
			return c.Status(429).JSON(fiber.Map{"error": "Too many login attempts. Please try again in a minute."})
		}

		var req struct {
			Password string `json:"password"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}

		// Check if password is configured
		adminPassword := cfg.Snapshot().AdminPassword
		if adminPassword == "" {
			return c.Status(500).JSON(fiber.Map{"error": "Admin password not configured"})
		}

		// Constant-time: a plain != leaks how many leading bytes of a guess
		// matched via response timing, the same class of concern the token
		// scheme below is designed around (#50).
		if subtle.ConstantTimeCompare([]byte(req.Password), []byte(adminPassword)) != 1 {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid password"})
		}

		token, err := sessions.Issue(SessionTTL)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to issue session token"})
		}
		return c.JSON(fiber.Map{
			"success": true,
			"token":   token,
		})
	})

	// Logout revokes the caller's own token immediately — deliberately
	// inside the authenticated group, both because revoking requires
	// knowing which token to revoke and because an unauthenticated logout
	// would just be a way to guess at valid tokens for free. The old
	// password-derived scheme had no equivalent: a leaked token stayed
	// valid until its date-based window happened to lapse (#50).
	api.Post("/logout", func(c *fiber.Ctx) error {
		if token, ok := c.Locals("authToken").(string); ok {
			sessions.Revoke(token)
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Dashboard Stats
	api.Get("/dashboard/stats", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.JSON(system.DashboardStats{})
		}
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
		if jm != nil && jm.LoadError() != "" {
			return c.Status(500).JSON(fiber.Map{
				"status": "degraded",
				"time":   time.Now(),
				"error":  "Failed to load jobs from disk: " + jm.LoadError(),
			})
		}
		return c.JSON(fiber.Map{"status": "ok", "time": time.Now()})
	})

	// Jobs
	api.Get("/jobs", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		return c.JSON(jm.GetAllJobs())
	})

	api.Post("/jobs", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		var req struct {
			Type            jobs.JobType `json:"type"`
			SourcePath      string       `json:"sourcePath"`
			DestPath        string       `json:"destinationPath"`
			Priority        int          `json:"priority"`
			CreateSubtitles bool         `json:"createSubtitles"`
			Upscale         bool         `json:"upscale"`
			Resolution      string       `json:"resolution"`
			MaxRetries      int          `json:"maxRetries"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		// One snapshot for the whole request: cfg is shared, unsynchronized,
		// with every HTTP handler that writes it and every job-worker
		// goroutine that reads it (#43).
		snap := cfg.Snapshot()

		// Security: Validate paths to prevent arbitrary file access
		sourcePath, err := security.ValidatePath(req.SourcePath, snap.SourceDir)
		if err != nil {
			return c.Status(403).JSON(fiber.Map{"error": err.Error()})
		}

		// Resolution filtering is library policy, not scanning policy (#41):
		// this was previously checked only for scanner-created jobs, which is
		// how already-high-resolution files got manually queued despite the
		// filter being on. A probe failure is not grounds to refuse the job —
		// it fails open, same as the scanner's own filter.
		if req.Type == jobs.JobTypeOptimize && snap.SkipHighResolution {
			ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
			_, height, _, _, probeErr := jm.GetVideoResolution(ctx, sourcePath)
			cancel()
			if probeErr == nil && height >= snap.ResolutionHeightThreshold {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf(
					"source is %dp, at or above the configured %dp skip threshold",
					height, snap.ResolutionHeightThreshold)})
			}
		}

		destPath := req.DestPath
		if destPath != "" {
			// Security: validate the destination too — this used to be a bare
			// filepath.Clean, which resolves ".." lexically but places no
			// bound on where the result actually lands. Both SourceDir and
			// DestDir are allowed: an explicit destination writing back
			// beside its source is exactly what happens below when none is
			// given (#37).
			validDest, err := security.ValidatePath(destPath, snap.SourceDir, snap.DestDir)
			if err != nil {
				return c.Status(403).JSON(fiber.Map{"error": err.Error()})
			}
			destPath = validDest
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
			ID:              util.GenerateID(),
			Type:            req.Type,
			SourcePath:      sourcePath,
			DestinationPath: destPath,
			Status:          jobs.StatusPending,
			Priority:        req.Priority,
			CreateSubtitles: req.CreateSubtitles,
			Upscale:         req.Upscale,
			Resolution:      req.Resolution,
			MaxRetries:      req.MaxRetries,
			// Inherit system defaults; the source file won't be deleted unless
			// the system config explicitly opts in (deleteSource) and, for premium
			// users, AI verification is enabled (verifyOutput).
			DeleteSource: snap.DeleteSource,
			VerifyOutput: snap.VerifyOutput,
			CreatedAt:    time.Now(),
		}
		jm.AddJob(job)
		return c.Status(201).JSON(job)
	})

	api.Get("/jobs/:id", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		job := jm.GetJob(c.Params("id"))
		if job == nil {
			return c.Status(404).JSON(fiber.Map{"error": "Job not found"})
		}
		return c.JSON(job)
	})

	// Cancel every pending or processing job. Distinct from DELETE /api/jobs,
	// which purges records by status — this stops work and leaves the history.
	api.Post("/jobs/cancel-all", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		count := jm.CancelAllActive()
		return c.JSON(fiber.Map{"cancelled": count})
	})

	api.Delete("/jobs", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		status := c.Query("status", "failed")
		count := jm.PurgeJobs(jobs.Status(status))
		return c.JSON(fiber.Map{"cleared": count})
	})

	api.Delete("/jobs/:id", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		if jm.CancelJob(c.Params("id")) {
			return c.JSON(fiber.Map{"success": true})
		}
		return c.Status(404).JSON(fiber.Map{"error": "Job not found"})
	})

	api.Post("/jobs/:id/retry", func(c *fiber.Ctx) error {
		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		if err := jm.RetryJob(c.Params("id")); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Config
	api.Get("/config", func(c *fiber.Ctx) error {
		snap := cfg.Snapshot()
		subtitleAPIKey := ""
		if snap.SubtitleAPIKey != "" {
			subtitleAPIKey = security.MaskKey(snap.SubtitleAPIKey)
		}
		return c.JSON(fiber.Map{
			"sourceDir":                 snap.SourceDir,
			"destDir":                   snap.DestDir,
			"gpuVendor":                 snap.GPUVendor,
			"qualityPreset":             snap.QualityPreset,
			"crf":                       snap.CRF,
			"aiProvider":                snap.AIProvider,
			"aiApiKey":                  security.MaskKey(snap.AIApiKey),
			"aiEndpoint":                snap.AIEndpoint,
			"aiModel":                   snap.AIModel,
			"licenseKey":                security.MaskKey(snap.LicenseKey),
			"isPremium":                 snap.IsPremium,
			"planName":                  license.GetPlanName(snap.LicenseKey),
			"verifyOutput":              snap.VerifyOutput,
			"deleteSource":              snap.DeleteSource,
			"autoConvertISO":            snap.AutoConvertISO,
			"overrideAICRF":             snap.OverrideAICRF,
			"skipHighResolution":        snap.SkipHighResolution,
			"resolutionHeightThreshold": snap.ResolutionHeightThreshold,
			"subtitleMode":              snap.SubtitleMode,
			"subtitleLang":              snap.SubtitleLang,
			"subtitleApiKey":            subtitleAPIKey,
			"subtitleUsername":          snap.SubtitleUsername,
			"subtitlePasswordSet":       snap.SubtitlePassword != "",
			"schedule":                  snap.Schedule,
			// maxConcurrentJobs is read-write here but only takes effect on
			// the next restart — see the restartRequired flag on POST
			// /api/config's response (#51).
			"maxConcurrentJobs": snap.MaxConcurrentJobs,
			"replaceInPlace":    snap.ReplaceInPlace,
			"holdingDir":        snap.HoldingDir,
			"puid":              snap.PUID,
			"pgid":              snap.PGID,
			"savingsFloor":      snap.SavingsFloor,
			"densityFloor":      snap.DensityFloor,
		})
	})

	api.Post("/config", func(c *fiber.Ctx) error {
		var req struct {
			QualityPreset             string                     `json:"qualityPreset"`
			CRF                       *int                       `json:"crf"`
			AIProvider                string                     `json:"aiProvider"`
			AIApiKey                  string                     `json:"aiApiKey"`
			AIEndpoint                string                     `json:"aiEndpoint"`
			AIModel                   string                     `json:"aiModel"`
			LicenseKey                string                     `json:"licenseKey"`
			VerifyOutput              *bool                      `json:"verifyOutput"`
			DeleteSource              *bool                      `json:"deleteSource"`
			AutoConvertISO            *bool                      `json:"autoConvertISO"`
			OverrideAICRF             *bool                      `json:"overrideAICRF"`
			SkipHighResolution        *bool                      `json:"skipHighResolution"`
			ResolutionHeightThreshold *int                       `json:"resolutionHeightThreshold"`
			SubtitleMode              string                     `json:"subtitleMode"`
			SubtitleLang              string                     `json:"subtitleLang"`
			SubtitleAPIKey            string                     `json:"subtitleApiKey"`
			SubtitleUsername          string                     `json:"subtitleUsername"`
			SubtitlePassword          string                     `json:"subtitlePassword"`
			Schedule                  *config.ProcessingSchedule `json:"schedule"`
			GPUVendor                 string                     `json:"gpuVendor"`
			MaxConcurrentJobs         *int                       `json:"maxConcurrentJobs"`
			ReplaceInPlace            *bool                      `json:"replaceInPlace"`
			HoldingDir                string                     `json:"holdingDir"`
			PUID                      *int                       `json:"puid"`
			PGID                      *int                       `json:"pgid"`
			SavingsFloor              *float64                   `json:"savingsFloor"`
			DensityFloor              *float64                   `json:"densityFloor"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		// Validated before the lock is ever taken: this can reject the
		// request outright, and doing that from inside the WithLock closure
		// below would only return from the closure, not this handler — the
		// response would fall through to "success" regardless (#43).
		if req.CRF != nil && (*req.CRF < 0 || *req.CRF > 51) {
			return c.Status(400).JSON(fiber.Map{"error": "CRF must be between 0 and 51"})
		}
		if req.GPUVendor != "" {
			switch req.GPUVendor {
			case "cpu", "nvidia", "intel", "amd":
			default:
				return c.Status(400).JSON(fiber.Map{"error": "gpuVendor must be one of: cpu, nvidia, intel, amd"})
			}
		}
		// MaxConcurrentJobs is snapshotted once into Manager.maxConcurrent at
		// startup and never re-read (#51) — accepted and persisted here so it
		// takes effect on the next restart, but never silently, hence the
		// separate "restartRequired" flag in the response below rather than a
		// plain "success" that would look identical to every other field's
		// immediate effect.
		if req.MaxConcurrentJobs != nil && *req.MaxConcurrentJobs < 1 {
			return c.Status(400).JSON(fiber.Map{"error": "maxConcurrentJobs must be at least 1"})
		}
		if req.PUID != nil && *req.PUID < -1 {
			return c.Status(400).JSON(fiber.Map{"error": "puid must be -1 (leave ownership untouched) or a non-negative uid"})
		}
		if req.PGID != nil && *req.PGID < -1 {
			return c.Status(400).JSON(fiber.Map{"error": "pgid must be -1 (leave ownership untouched) or a non-negative gid"})
		}
		if req.SavingsFloor != nil && (*req.SavingsFloor < 0 || *req.SavingsFloor > 1) {
			return c.Status(400).JSON(fiber.Map{"error": "savingsFloor must be between 0 and 1"})
		}
		if req.DensityFloor != nil && *req.DensityFloor < 0 {
			return c.Status(400).JSON(fiber.Map{"error": "densityFloor must not be negative"})
		}

		// One WithLock for every field this request touches, so a
		// concurrent reader's Snapshot() sees either all of these changes
		// or none of them. The AI-provider fields are also captured here,
		// under the same lock, rather than re-read afterward — otherwise a
		// second POST /api/config landing in between could mean the
		// provider built below doesn't match what was just logged as saved.
		var aiProvider, aiAPIKey, aiEndpoint, aiModel string
		var isPremium bool
		cfg.WithLock(func() {
			if req.QualityPreset != "" {
				cfg.QualityPreset = req.QualityPreset
			}
			if req.CRF != nil {
				cfg.CRF = *req.CRF
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

			// Boolean fields — pointer check distinguishes "not sent" from false
			if req.VerifyOutput != nil {
				cfg.VerifyOutput = *req.VerifyOutput
			}
			if req.DeleteSource != nil {
				cfg.DeleteSource = *req.DeleteSource
			}
			if req.AutoConvertISO != nil {
				cfg.AutoConvertISO = *req.AutoConvertISO
			}
			if req.OverrideAICRF != nil {
				cfg.OverrideAICRF = *req.OverrideAICRF
			}
			if req.SkipHighResolution != nil {
				cfg.SkipHighResolution = *req.SkipHighResolution
			}
			if req.ResolutionHeightThreshold != nil {
				cfg.ResolutionHeightThreshold = *req.ResolutionHeightThreshold
			}

			// Subtitle settings
			if req.SubtitleMode != "" {
				cfg.SubtitleMode = req.SubtitleMode
			}
			if req.SubtitleLang != "" {
				cfg.SubtitleLang = req.SubtitleLang
			}
			if req.SubtitleAPIKey != "" && !strings.Contains(req.SubtitleAPIKey, "....") {
				cfg.SubtitleAPIKey = req.SubtitleAPIKey
			}
			if req.SubtitleUsername != "" {
				cfg.SubtitleUsername = req.SubtitleUsername
			}
			if req.SubtitlePassword != "" {
				cfg.SubtitlePassword = req.SubtitlePassword
			}

			// Schedule
			if req.Schedule != nil {
				cfg.Schedule = *req.Schedule
			}

			if req.GPUVendor != "" {
				cfg.GPUVendor = req.GPUVendor
			}
			if req.MaxConcurrentJobs != nil {
				cfg.MaxConcurrentJobs = *req.MaxConcurrentJobs
			}
			if req.ReplaceInPlace != nil {
				cfg.ReplaceInPlace = *req.ReplaceInPlace
			}
			if req.HoldingDir != "" {
				cfg.HoldingDir = req.HoldingDir
			}
			if req.PUID != nil {
				cfg.PUID = *req.PUID
			}
			if req.PGID != nil {
				cfg.PGID = *req.PGID
			}
			if req.SavingsFloor != nil {
				cfg.SavingsFloor = *req.SavingsFloor
			}
			if req.DensityFloor != nil {
				cfg.DensityFloor = *req.DensityFloor
			}

			aiProvider, aiAPIKey, aiEndpoint, aiModel = cfg.AIProvider, cfg.AIApiKey, cfg.AIEndpoint, cfg.AIModel
			isPremium = cfg.IsPremium
		})

		// Re-initialize AI provider in manager
		newAI, err := ai.NewProvider(ai.AIConfig{
			Provider: aiProvider,
			APIKey:   aiAPIKey,
			Endpoint: aiEndpoint,
			Model:    aiModel,
		})
		if err == nil {
			if jm != nil {
				jm.UpdateAIProvider(newAI)
			}
		} else {
			log.Printf("Error updating AI provider: %v", err)
		}

		log.Printf("Configuration updated: AI Provider=%s, Premium=%v", aiProvider, isPremium)

		if err := cfg.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}

		return c.JSON(fiber.Map{
			"success": true,
			// True when this request changed a field the running process
			// only reads once at startup — MaxConcurrentJobs, snapshotted
			// into Manager.maxConcurrent and never re-read (#51). The saved
			// value takes effect on the next restart; nothing else in this
			// response distinguishes that from every other field's
			// immediate effect.
			"restartRequired": req.MaxConcurrentJobs != nil,
		})
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
			storedKey := cfg.Snapshot().AIApiKey
			if apiKey == security.MaskKey(storedKey) {
				apiKey = storedKey
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

		// time.NewTicker panics on a non-positive duration, and periodicScan
		// builds one straight from this field — Validate() (called inside
		// UpdateConfig below) floors any bad value as a last line of
		// defense, but silently replacing a deliberately-bad value there
		// means the operator never finds out their input didn't take.
		//
		// 0 is left to Validate()'s silent default rather than rejected
		// here: this endpoint has no partial-update contract (unlike
		// POST /api/config's pointer fields) and BodyParser leaves every
		// omitted field at its zero value the same way, so treating 0 as
		// "not provided" is consistent with every other field on this
		// struct. A value that's present but still under the floor (1-59,
		// or negative) can only be a deliberate, bad input, so that's what
		// gets rejected (#36).
		if newCfg.ScanIntervalSec != 0 && newCfg.ScanIntervalSec < scanner.MinScanIntervalSec {
			return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf(
				"scanIntervalSec must be at least %d seconds", scanner.MinScanIntervalSec)})
		}

		// One snapshot for the whole request: cfg is shared, unsynchronized,
		// with every HTTP handler that writes it and every job-worker
		// goroutine that reads it (#43).
		snap := cfg.Snapshot()

		// Security: Validate watch directories
		for i, dir := range newCfg.WatchDirectories {
			validPath, err := security.ValidatePath(dir.Path, snap.SourceDir)
			if err != nil {
				return c.Status(403).JSON(fiber.Map{"error": fmt.Sprintf("Watch directory %d: %v", i, err)})
			}
			newCfg.WatchDirectories[i].Path = validPath
		}

		// Security: Validate output directory
		if newCfg.OutputDirectory != "" {
			validOutput, err := security.ValidatePath(newCfg.OutputDirectory, snap.DestDir)
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

	// Discover files without creating jobs
	api.Get("/scanner/discover", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}
		files, err := fs.Discover()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"files": files})
	})

	// Queue specific files by path
	api.Post("/scanner/queue", func(c *fiber.Ctx) error {
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
		}
		var req struct {
			Paths []string `json:"paths"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
		}
		queued := 0
		var errs []string
		for _, path := range req.Paths {
			if err := fs.QueueFile(path); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			} else {
				queued++
			}
		}
		return c.JSON(fiber.Map{"queued": queued, "errors": errs})
	})

	// SSE short-lived token exchange (bug #26 mitigation)
	// Issues a 2-minute token so the long-lived session token never appears in server access logs.
	api.Post("/events/token", func(c *fiber.Ctx) error {
		token, err := sessions.Issue(SSETokenTTL)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to issue SSE token"})
		}
		return c.JSON(fiber.Map{"token": token})
	})

	// AI Search
	api.Get("/search", func(c *fiber.Ctx) error {
		query := c.Query("q")
		if query == "" {
			return c.Status(400).JSON(fiber.Map{"error": "Query is required"})
		}

		if !cfg.Snapshot().IsPremium {
			return c.Status(403).JSON(fiber.Map{"error": "AI Search is a premium feature"})
		}

		if jm == nil {
			return c.Status(500).JSON(fiber.Map{"error": "Job manager not initialized"})
		}
		if fs == nil {
			return c.Status(503).JSON(fiber.Map{"error": "Scanner not initialized"})
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
