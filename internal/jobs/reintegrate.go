package jobs

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vasteva/MediaConverter/internal/util"
)

// outputContainerExt is the container every optimise job produces. Replacing a
// source in place is not necessarily a same-name overwrite: an .avi source
// becomes a .mkv, so the original is retired and a differently-named file takes
// its position in the library.
const outputContainerExt = ".mkv"

// TempFilePrefix marks in-progress transcodes written into the library. The
// scanner skips these, so a partially written file is never picked up as a new
// source — which would queue a transcode of a file still being written.
const TempFilePrefix = ".vastiva-tmp-"

// replacementPaths describes where a replace-in-place job writes.
type replacementPaths struct {
	// Temp is where FFmpeg writes. It sits in the destination directory so the
	// final move is a rename within one filesystem, which is atomic. Writing to
	// a separate /output mount and moving afterwards is not: those are distinct
	// bind mounts and the rename fails with EXDEV.
	Temp string

	// Final is the path the transcode takes in the library once validated.
	Final string

	// Source is the file being replaced.
	Source string
}

// planReplacement computes the paths for replacing sourcePath in place.
func planReplacement(sourcePath, jobID string) replacementPaths {
	dir := filepath.Dir(sourcePath)
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))

	return replacementPaths{
		Temp:   filepath.Join(dir, TempFilePrefix+jobID+outputContainerExt),
		Final:  filepath.Join(dir, base+outputContainerExt),
		Source: sourcePath,
	}
}

// holdingPathFor returns where a replaced original should be parked, preserving
// its position relative to the media root so two films with the same filename
// in different folders cannot collide.
func holdingPathFor(holdingDir, sourceRoot, sourcePath string) string {
	rel, err := filepath.Rel(sourceRoot, sourcePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Outside the configured root — fall back to a flat name rather than
		// writing somewhere unexpected.
		rel = filepath.Base(sourcePath)
	}
	return filepath.Join(holdingDir, rel)
}

// reintegrate puts a validated transcode into the library in place of its
// source, and moves the original to the holding directory.
//
// Ordering matters, because every step can fail:
//
//  1. Move the original to holding. If this fails nothing has changed.
//  2. Rename the validated temp file into place. If this fails, the original is
//     moved back, so the library is never left without the title.
//  3. Set ownership. A failure here is logged, not fatal — the file is correct,
//     only its uid/gid is not.
//
// The original is moved rather than deleted so a bad batch can be reversed by
// moving the holding directory back. Nothing here removes it.
func (m *Manager) reintegrate(job *Job, paths replacementPaths) error {
	cfg := m.config

	holdingPath := holdingPathFor(cfg.HoldingDir, cfg.SourceDir, paths.Source)
	if err := os.MkdirAll(filepath.Dir(holdingPath), 0o755); err != nil {
		return fmt.Errorf("creating holding directory: %w", err)
	}

	// A previous run may have parked a file at this exact path. Refuse rather
	// than overwrite: the held file is the only remaining copy of that original.
	if _, err := os.Stat(holdingPath); err == nil {
		return fmt.Errorf("holding path %s already exists — refusing to overwrite a retained original", holdingPath)
	}

	if err := os.Rename(paths.Source, holdingPath); err != nil {
		return fmt.Errorf("moving original to holding: %w", err)
	}

	if err := os.Rename(paths.Temp, paths.Final); err != nil {
		// Put the original back so the library still has the title.
		if restoreErr := os.Rename(holdingPath, paths.Source); restoreErr != nil {
			return fmt.Errorf("promoting transcode failed (%w), and restoring the original ALSO failed (%v) — "+
				"the original is at %s", err, restoreErr, holdingPath)
		}
		return fmt.Errorf("promoting transcode failed, original restored: %w", err)
	}

	owner := m.fileOwnership()
	if err := owner.Apply(paths.Final); err != nil {
		log.Printf("[Job %s] Warning: %v", job.ID, err)
	}

	log.Printf("[Job %s] Replaced in library: %s (original held at %s)",
		job.ID, paths.Final, holdingPath)

	m.appendAILog(job, AILog{
		Timestamp: time.Now(),
		Operation: "reintegrated",
		Provider:  "System",
		Detail: fmt.Sprintf("Replaced %s in the library; original retained at %s",
			filepath.Base(paths.Final), holdingPath),
		Success: true,
	})

	return nil
}

// fileOwnership returns the uid/gid that written files should carry.
func (m *Manager) fileOwnership() util.FileOwnership {
	return util.FileOwnership{UID: m.config.PUID, GID: m.config.PGID}
}

// cleanupTemp removes a leftover temp transcode. Safe to call unconditionally.
func (m *Manager) cleanupTemp(job *Job, path string) {
	if path == "" {
		return
	}
	if !strings.HasPrefix(filepath.Base(path), TempFilePrefix) {
		// Guard against ever being handed a real library file.
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[Job %s] Warning: could not remove temp file %s: %v", job.ID, path, err)
	}
}
