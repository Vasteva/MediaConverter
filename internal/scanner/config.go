package scanner

import (
	"encoding/json"
	"log"
	"os"

	"github.com/Vasteva/MediaConverter/internal/config"
)

// LoadScannerConfig loads scanner configuration from file and environment.
// It never returns an error; if the config file is missing or unreadable the
// env/global-config defaults are used so that ProcessedFilePath is always set.
func LoadScannerConfig(cfg *config.Config, watchDirsFile string) (*ScannerConfig, error) {
	// Build defaults from environment/global config
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

	if watchDirsFile == "" {
		// No config file — use a default watch directory from SOURCE_DIR
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
		return scannerCfg, nil
	}

	data, err := os.ReadFile(watchDirsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Scanner] Warning: could not read config file %s: %v — using defaults", watchDirsFile, err)
		}
		return scannerCfg, nil
	}

	// The file is written by saveConfig() as a full ScannerConfig object.
	// Support a legacy []WatchDirectory array format as well.
	if len(data) > 0 && data[0] == '{' {
		var fullCfg ScannerConfig
		if err := json.Unmarshal(data, &fullCfg); err == nil {
			fullCfg.Validate()
			return &fullCfg, nil
		}
		log.Printf("[Scanner] Warning: could not parse config file %s as ScannerConfig: %v — using defaults", watchDirsFile, err)
	} else if len(data) > 0 && data[0] == '[' {
		var watchDirs []WatchDirectory
		if err := json.Unmarshal(data, &watchDirs); err == nil {
			scannerCfg.WatchDirectories = watchDirs
		} else {
			log.Printf("[Scanner] Warning: could not parse config file %s as watch directories: %v — using defaults", watchDirsFile, err)
		}
	}

	return scannerCfg, nil
}

// SaveWatchDirectories saves watch directory configuration to JSON file
func SaveWatchDirectories(filePath string, watchDirs []WatchDirectory) error {
	data, err := json.MarshalIndent(watchDirs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}
