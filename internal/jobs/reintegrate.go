package jobs

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/media"
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
//
// finalBaseName is the name (without extension) the promoted file should take —
// the AI-cleaned title when metadata cleaning renamed the job, so the library
// ends up with "Dolittle (2020).mkv" rather than the release name it was
// downloaded under. Empty falls back to the source's own base name, so a job
// that was not renamed keeps the file where it was, only switching the
// container to .mkv. It is always placed in the source's directory; any path
// components in finalBaseName are stripped.
func planReplacement(sourcePath, jobID, finalBaseName string) replacementPaths {
	dir := filepath.Dir(sourcePath)
	if finalBaseName == "" {
		finalBaseName = strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	} else {
		finalBaseName = filepath.Base(finalBaseName)
	}

	return replacementPaths{
		Temp:   filepath.Join(dir, TempFilePrefix+jobID+outputContainerExt),
		Final:  filepath.Join(dir, finalBaseName+outputContainerExt),
		Source: sourcePath,
	}
}

// alreadyReplacedInPlaceReason returns a non-empty skip reason when sourcePath
// is itself the output of a prior replace-in-place optimise of this same file:
// replace-in-place is on, a retained original sits at sourcePath's holding
// path, and the file is already in this pipeline's target codec. Re-optimising
// such a file is generational loss, and reintegrate would abort on it anyway
// rather than overwrite the retained original.
//
// The codec check keeps this to the common case. A source that is not HEVC/AV1
// but still has a holding counterpart (e.g. a fresh non-HEVC download dropped
// in under an identical release name) is left to run and, if it collides, to
// hit reintegrate's own guard.
func alreadyReplacedInPlaceReason(cfg config.Config, sourcePath string, info *media.MediaInfo) string {
	if !cfg.ReplaceInPlace || cfg.HoldingDir == "" || info == nil {
		return ""
	}
	if !media.IsHEVCOrAV1(info.CodecName) {
		return ""
	}
	holding := holdingPathFor(cfg.HoldingDir, cfg.SourceDir, sourcePath)
	if holding == sourcePath {
		return ""
	}
	if _, err := os.Stat(holding); err != nil {
		return ""
	}
	return fmt.Sprintf(
		"source is already this pipeline's %s output — the original is retained at %s; "+
			"re-encoding it is generational loss and cannot be reintegrated over the retained original (%s)",
		strings.ToUpper(info.CodecName), holding, filepath.Base(sourcePath))
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
	cfg := m.config.Snapshot()

	holdingPath := holdingPathFor(cfg.HoldingDir, cfg.SourceDir, paths.Source)
	if err := os.MkdirAll(filepath.Dir(holdingPath), 0o755); err != nil {
		return fmt.Errorf("creating holding directory: %w", err)
	}

	// A previous run may have parked a file at this exact path. Refuse rather
	// than overwrite: the held file is the only remaining copy of that original.
	if _, err := os.Stat(holdingPath); err == nil {
		return fmt.Errorf("holding path %s already exists — refusing to overwrite a retained original", holdingPath)
	}

	// A different container extension (movie.avi -> movie.mkv) means Final
	// can collide with some unrelated file already sitting at that name —
	// os.Rename below would replace it with no warning (#47). Skipped when
	// Final and Source are the same path (the common same-container replace,
	// e.g. movie.mkv -> movie.mkv): Source is about to be moved to holding,
	// so by the time Final is written to, this path is naturally free.
	if paths.Final != paths.Source {
		if _, err := os.Stat(paths.Final); err == nil {
			return fmt.Errorf("output path %s already exists — refusing to overwrite an unrelated file", paths.Final)
		}
	}

	// The renames below surface paths.Final to the scanner's directory
	// watcher. Claim it first so a watch event cannot queue a fresh optimise
	// of this output before the job's completion hook records it. Released on
	// any failure here; superseded by a durable entry once the job completes.
	if m.OnOutputClaimed != nil {
		m.OnOutputClaimed(paths.Final)
	}
	release := func() {
		if m.OnOutputReleased != nil {
			m.OnOutputReleased(paths.Final)
		}
	}

	if err := os.Rename(paths.Source, holdingPath); err != nil {
		release()
		return fmt.Errorf("moving original to holding: %w", err)
	}

	if err := os.Rename(paths.Temp, paths.Final); err != nil {
		// Put the original back so the library still has the title.
		if restoreErr := os.Rename(holdingPath, paths.Source); restoreErr != nil {
			release()
			return fmt.Errorf("promoting transcode failed (%w), and restoring the original ALSO failed (%v) — "+
				"the original is at %s", err, restoreErr, holdingPath)
		}
		release()
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
	cfg := m.config.Snapshot()
	return util.FileOwnership{UID: cfg.PUID, GID: cfg.PGID}
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
