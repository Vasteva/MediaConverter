package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/security"
	"github.com/gofiber/fiber/v2"
)

type FileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"isDir"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	Extension string    `json:"extension"`
}

type FileListResponse struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent"`
	Entries []FileEntry `json:"entries"`
	IsRoot  bool        `json:"isRoot"`
	Error   string      `json:"error,omitempty"`
}

func RegisterFSRoutes(api fiber.Router, cfg *config.Config) {
	api.Get("/fs/list", func(c *fiber.Ctx) error {
		return handleListFiles(c, cfg)
	})
}

func handleListFiles(c *fiber.Ctx, cfg *config.Config) error {
	reqPath := c.Query("path")

	// Default to root if not specified
	if reqPath == "" {
		reqPath = "/"
	}

	// Clean the path
	absPath := filepath.Clean(reqPath)

	log.Printf("[FS] Listing path: %s", absPath)

	// Security: Restrict to SourceDir or DestDir
	if _, err := security.ValidatePath(absPath, cfg.SourceDir, cfg.DestDir); err != nil {
		// Check if the requested path is a parent of an allowed directory.
		// If so, return a virtual listing showing only the accessible subdirs.
		var virtualEntries []FileEntry
		for _, allowedDir := range []string{cfg.SourceDir, cfg.DestDir} {
			if allowedDir == "" {
				continue
			}
			cleanAllowed := filepath.Clean(allowedDir)
			prefix := absPath
			if prefix != "/" {
				prefix = prefix + string(filepath.Separator)
			}
			if strings.HasPrefix(cleanAllowed, prefix) {
				info, statErr := os.Stat(cleanAllowed)
				if statErr == nil {
					virtualEntries = append(virtualEntries, FileEntry{
						Name:    filepath.Base(cleanAllowed),
						Path:    cleanAllowed,
						IsDir:   true,
						ModTime: info.ModTime(),
					})
				}
			}
		}
		if len(virtualEntries) > 0 {
			log.Printf("[FS] Returning virtual listing for parent path: %s", absPath)
			parent := filepath.Dir(absPath)
			if absPath == "/" {
				parent = ""
			}
			return c.JSON(FileListResponse{
				Path:    absPath,
				Parent:  parent,
				Entries: virtualEntries,
				IsRoot:  absPath == "/",
			})
		}
		log.Printf("[FS] Access denied for path: %s", absPath)
		return c.Status(403).JSON(fiber.Map{"error": "Access denied"})
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		log.Printf("[FS] Error reading %s: %v", absPath, err)
		return c.Status(500).JSON(fiber.Map{
			"error": fmt.Sprintf("Failed to read directory: %v", err),
		})
	}

	var files []FileEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, FileEntry{
			Name:      entry.Name(),
			Path:      filepath.Join(absPath, entry.Name()),
			IsDir:     entry.IsDir(),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Extension: filepath.Ext(entry.Name()),
		})
	}

	// Sort: Directories first, then files by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	parent := filepath.Dir(absPath)
	if absPath == "/" {
		parent = ""
	}

	log.Printf("[FS] Found %d entries in %s", len(files), absPath)

	return c.JSON(FileListResponse{
		Path:    absPath,
		Parent:  parent,
		Entries: files,
		IsRoot:  absPath == "/",
	})
}
