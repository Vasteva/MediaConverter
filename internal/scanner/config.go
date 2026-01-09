package scanner

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rwurtz/vastiva/internal/config"
)

// LoadScannerConfig loads scanner configuration from file and environment
func LoadScannerConfig(cfg *config.Config, watchDirsFile string) (*ScannerConfig, error) {
	scannerCfg := &ScannerConfig{
		Mode:              ScanMode(cfg.ScannerMode),
		Enabled:           cfg.ScannerEnabled,
		ScanIntervalSec:   cfg.ScannerIntervalSec,
		AutoCreateJobs:    cfg.ScannerAutoCreate,
		ProcessedFilePath: cfg.ScannerProcessedFile,
		DefaultPriority:   5,
		OutputDirectory:   cfg.DestDir,

		// Default file extensions
		ExtractExtensions: []string{".iso"},
		OptimizeExtensions: []string{
			".mkv", ".mp4", ".avi", ".mov", ".m4v",
			".mpg", ".mpeg", ".wmv", ".flv", ".webm",
		},
	}

	// Load watch directories from file if it exists
	if watchDirsFile != "" {
		watchDirs, err := loadWatchDirectories(watchDirsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load watch directories: %w", err)
		}
		scannerCfg.WatchDirectories = watchDirs
	} else {
		// Use default watch directory from SOURCE_DIR
		scannerCfg.WatchDirectories = []WatchDirectory{
			{
				Path:              cfg.SourceDir,
				Recursive:         true,
				IncludePatterns:   []string{"*.mkv", "*.mp4", "*.avi", "*.iso"},
				ExcludePatterns:   []string{"*_optimized.mkv", "*_temp*", ".*"},
				MinFileSizeMB:     10,
				MinFileAgeMinutes: 2,
			},
		}
	}

	return scannerCfg, nil
}

// loadWatchDirectories loads watch directory configuration from JSON file
func loadWatchDirectories(filePath string) ([]WatchDirectory, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var watchDirs []WatchDirectory
	if err := json.Unmarshal(data, &watchDirs); err != nil {
		return nil, err
	}

	return watchDirs, nil
}

// SaveWatchDirectories saves watch directory configuration to JSON file
func SaveWatchDirectories(filePath string, watchDirs []WatchDirectory) error {
	data, err := json.MarshalIndent(watchDirs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}
