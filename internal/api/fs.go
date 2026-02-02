package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

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

func RegisterFSRoutes(api fiber.Router) {
	api.Get("/fs/list", handleListFiles)
}

func handleListFiles(c *fiber.Ctx) error {
	reqPath := c.Query("path")

	// Default to root if not specified
	if reqPath == "" {
		reqPath = "/"
	}

	// Clean the path
	absPath := filepath.Clean(reqPath)

	log.Printf("[FS] Listing path: %s", absPath)

	// Security: In a real app we might want to restrict this
	// But since this is a self-hosted media tool, we allow browsing
	// We could restrict to /mnt/media or similar if strict mode is on

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
