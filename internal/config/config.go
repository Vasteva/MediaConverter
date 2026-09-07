package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Vasteva/MediaConverter/internal/license"
	"github.com/Vasteva/MediaConverter/internal/media"
	"github.com/Vasteva/MediaConverter/internal/system"
)

const currentSchemaVersion = 1

type ProcessingSchedule struct {
	Enabled     bool   `json:"enabled"`
	StartHour   int    `json:"startHour"`   // 0–23
	EndHour     int    `json:"endHour"`     // 0–23
	AllowedDays []int  `json:"allowedDays"` // 0=Sun…6=Sat; empty = all days
	Timezone    string `json:"timezone"`    // IANA e.g. "America/New_York"
}

// MarshalJSON always emits AllowedDays as an array, never null.
//
// A nil slice marshals to JSON null, and the zero value of this struct is the
// normal case — any config that has never had a schedule set. The settings UI
// reads the field directly (schedule.allowedDays.includes(...)), so null threw
// a TypeError the moment the schedule checkbox was ticked and the day picker
// rendered, unmounting the whole settings page.
//
// Guaranteeing the shape here rather than at the handler means every consumer
// gets it, including the config written to disk.
func (s ProcessingSchedule) MarshalJSON() ([]byte, error) {
	// The local type sheds this method so the nested Marshal does not recurse.
	type scheduleFields ProcessingSchedule
	if s.AllowedDays == nil {
		s.AllowedDays = []int{}
	}
	return json.Marshal(scheduleFields(s))
}

type Config struct {
	SchemaVersion int `json:"schemaVersion"`

	// Server
	Port string `json:"port"`

	// Paths
	SourceDir string `json:"sourceDir"`
	DestDir   string `json:"destDir"`

	// Encoding
	GPUVendor     string `json:"gpuVendor"`
	QualityPreset string `json:"qualityPreset"`
	CRF           int    `json:"crf"`

	// Jobs
	MaxConcurrentJobs int `json:"maxConcurrentJobs"`

	// AI
	AIProvider string `json:"aiProvider"`
	AIApiKey   string `json:"aiApiKey"`
	AIEndpoint string `json:"aiEndpoint"`
	AIModel    string `json:"aiModel"`

	// Auth
	AdminPassword string `json:"adminPassword"`
	LicenseKey    string `json:"licenseKey"`

	// Scanner
	ScannerEnabled       bool   `json:"scannerEnabled"`
	ScannerMode          string `json:"scannerMode"`
	ScannerIntervalSec   int    `json:"scannerIntervalSec"`
	ScannerAutoCreate    bool   `json:"scannerAutoCreate"`
	ScannerProcessedFile string `json:"scannerProcessedFile"`

	// Subtitles
	SubtitleMode     string `json:"subtitleMode"`     // "always", "selective", "never"
	SubtitleLang     string `json:"subtitleLang"`     // ISO 639-1, e.g. "en"
	SubtitleAPIKey   string `json:"subtitleApiKey"`   // OpenSubtitles API (app) key
	SubtitleUsername string `json:"subtitleUsername"` // OpenSubtitles account username
	SubtitlePassword string `json:"subtitlePassword"` // OpenSubtitles account password

	// Processing
	VerifyOutput   bool `json:"verifyOutput"`   // Default: false (Pro only)
	DeleteSource   bool `json:"deleteSource"`   // Default: false
	AutoConvertISO bool `json:"autoConvertISO"` // Default: false

	// OverrideAICRF forces the configured CRF and skips the AI adaptive
	// encoding suggestion entirely, even when premium and an AI provider are
	// configured. Default off: existing premium+AI installs keep today's
	// behaviour until they opt out.
	OverrideAICRF bool `json:"overrideAICRF"` // Default: false

	// SkipHighResolution and ResolutionHeightThreshold used to live only on
	// ScannerConfig, so POST /api/jobs and manually-queued files never saw
	// them — how a 2160p source got processed despite the filter being on.
	// This is a policy about the library, not about scanning, so both the
	// scanner and manual job creation share it from here (#41).
	SkipHighResolution        bool `json:"skipHighResolution"`        // Default: false
	ResolutionHeightThreshold int  `json:"resolutionHeightThreshold"` // Default: 1080

	// SavingsFloor is the minimum fraction (0.15 == 15%) an output must be
	// smaller than its source to be kept; below it the output is discarded and
	// the original retained. Not yet exposed on GET/POST /api/config or the
	// settings UI — see #51, sequenced after the config mutex fix in #43 so
	// this doesn't become one more field mutated without synchronisation.
	SavingsFloor float64 `json:"savingsFloor"` // Default: 0.15

	// DensityFloor is the bits-per-pixel-per-frame density at or below which
	// an HEVC/AV1 source is skipped rather than re-encoded — see
	// media.IsAlreadyEfficient. Same exposure caveat as SavingsFloor above.
	DensityFloor float64 `json:"densityFloor"` // Default: 0.06

	// Reintegration. With ReplaceInPlace the transcode is written beside its
	// source and, once validated, takes the source's place in the library —
	// so Jellyfin sees the optimised file without anything being moved by
	// hand. The original is moved to HoldingDir rather than deleted, which
	// keeps every replacement reversible until that directory is emptied.
	ReplaceInPlace bool   `json:"replaceInPlace"` // Default: false
	HoldingDir     string `json:"holdingDir"`

	// File ownership for everything this process writes. The container runs as
	// root, so without these the library fills with root-owned files. -1 leaves
	// ownership untouched. Named to match the linuxserver convention already
	// used by the Jellyfin container in the same stack.
	PUID int `json:"puid"`
	PGID int `json:"pgid"`

	// Scheduling
	Schedule ProcessingSchedule `json:"schedule"`

	// State
	IsPremium     bool `json:"-"`
	IsInitialized bool `json:"-"`

	// lastMerge records how the environment and the persisted file were
	// reconciled at load time, for diagnostics.
	lastMerge *merger `json:"-"`

	// mu guards every field above from concurrent read/write once the server
	// is serving requests (#43): one *Config is shared, unsynchronized,
	// between the HTTP handlers that write it (POST /api/config and friends)
	// and every job-worker goroutine and scanner scan that reads it mid-job.
	// A pointer, not an embedded sync.RWMutex, so Config stays copyable by
	// value (see Snapshot) without `go vet`'s copylocks check firing on the
	// copy itself.
	//
	// Nil-tolerant: Load() always sets it, but plenty of tests construct a
	// bare &Config{...} directly, and it would be needless churn to make
	// every one of them set a mutex they don't need. Lock/Unlock/RLock/
	// RUnlock/Snapshot all treat a nil mu as "no concurrent access to guard
	// against" rather than panicking.
	mu *sync.RWMutex `json:"-"`
}

// WithLock runs fn while holding the write lock, so several related field
// changes — see POST /api/config and POST /api/setup/complete — are seen by
// readers all-or-nothing instead of a Snapshot landing mid-update.
//
// Deliberately not exported as Lock/Unlock: a type with methods named
// exactly that satisfies sync.Locker, and go vet's copylocks check then
// flags every copy of *any* Config value — including Snapshot's own return,
// which is the copy this whole mechanism exists to make safe. Named
// differently, none of that fires, and the closure shape also makes it
// impossible to take the lock and forget to release it.
//
// A nil mu (a Config built directly, e.g. in a test, rather than via Load)
// makes this a no-op rather than a nil-pointer panic.
func (c *Config) WithLock(fn func()) {
	if c.mu == nil {
		fn()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
}

func (c *Config) rLock() {
	if c.mu != nil {
		c.mu.RLock()
	}
}

func (c *Config) rUnlock() {
	if c.mu != nil {
		c.mu.RUnlock()
	}
}

// Snapshot returns a point-in-time copy of the config, safe to read from
// without further locking. Callers should take exactly one snapshot at the
// start of the request or job they're handling and read every field from
// that local copy from then on, rather than re-reading the shared Config
// repeatedly — the copy can never be mutated out from under you mid-job, and
// it's one lock acquisition instead of one per field.
func (c *Config) Snapshot() Config {
	c.rLock()
	defer c.rUnlock()
	cp := *c
	// The one field that's a reference type — without this the copy would
	// still alias the original's backing array, defeating the point.
	cp.Schedule.AllowedDays = append([]int(nil), c.Schedule.AllowedDays...)
	// A snapshot is a plain, inert value. Without this it would carry the
	// same *sync.RWMutex as the live Config it was copied from, so calling
	// Lock/Snapshot again on the snapshot would silently reach back into the
	// original's lock instead of no-op'ing like every other bare value does.
	cp.mu = nil
	return cp
}

// ConfigFile is the persisted settings path. Overridable via CONFIG_FILE, for
// consistency with JOBS_FILE and SCANNER_CONFIG_FILE, and so tests can point it
// somewhere writable.
var ConfigFile = getEnv("CONFIG_FILE", "/data/config.json")

func Load() *Config {
	// Default values
	cfg := &Config{
		mu:                        &sync.RWMutex{},
		SchemaVersion:             currentSchemaVersion,
		Port:                      getEnv("PORT", "8080"),
		SourceDir:                 getEnv("SOURCE_DIR", "/storage"),
		DestDir:                   getEnv("DEST_DIR", "/output"),
		GPUVendor:                 getEnv("GPU_VENDOR", "auto"),
		QualityPreset:             getEnv("QUALITY_PRESET", "medium"),
		CRF:                       getEnvInt("CRF", 23),
		MaxConcurrentJobs:         getEnvInt("MAX_CONCURRENT_JOBS", 2),
		AIProvider:                getEnv("AI_PROVIDER", "none"),
		AIApiKey:                  getEnv("AI_API_KEY", ""),
		AIEndpoint:                getEnv("AI_ENDPOINT", ""),
		AIModel:                   getEnv("AI_MODEL", ""),
		AdminPassword:             getEnv("ADMIN_PASSWORD", ""),
		LicenseKey:                getEnv("LICENSE_KEY", ""),
		ScannerEnabled:            getEnvBool("SCANNER_ENABLED", false),
		ScannerMode:               getEnv("SCANNER_MODE", "manual"),
		ScannerIntervalSec:        getEnvInt("SCANNER_INTERVAL_SEC", 300),
		ScannerAutoCreate:         getEnvBool("SCANNER_AUTO_CREATE", true),
		ScannerProcessedFile:      getEnv("SCANNER_PROCESSED_FILE", "/data/processed.json"),
		SubtitleMode:              getEnv("SUBTITLE_MODE", "selective"),
		SubtitleLang:              getEnv("SUBTITLE_LANG", "en"),
		SubtitleAPIKey:            getEnv("SUBTITLE_API_KEY", ""),
		SubtitleUsername:          getEnv("SUBTITLE_USERNAME", ""),
		SubtitlePassword:          getEnv("SUBTITLE_PASSWORD", ""),
		VerifyOutput:              getEnvBool("VERIFY_OUTPUT", false),
		DeleteSource:              getEnvBool("DELETE_SOURCE", false),
		AutoConvertISO:            getEnvBool("AUTO_CONVERT_ISO", false),
		OverrideAICRF:             getEnvBool("OVERRIDE_AI_CRF", false),
		SkipHighResolution:        getEnvBool("SKIP_HIGH_RESOLUTION", false),
		ResolutionHeightThreshold: getEnvInt("RESOLUTION_HEIGHT_THRESHOLD", 1080),
		SavingsFloor:              getEnvFloat("SAVINGS_FLOOR", 0.15),
		DensityFloor:              getEnvFloat("DENSITY_FLOOR", media.DefaultDensityFloor),
		ReplaceInPlace:            getEnvBool("REPLACE_IN_PLACE", false),
		HoldingDir:                getEnv("HOLDING_DIR", ""),
		PUID:                      getEnvInt("PUID", -1),
		PGID:                      getEnvInt("PGID", -1),
	}

	if cfg.GPUVendor == "auto" || cfg.GPUVendor == "" {
		cfg.GPUVendor = system.DetectGPU()
	}

	// Override with values from disk where the environment did not speak.
	if err := cfg.loadFromDisk(); err != nil && !os.IsNotExist(err) {
		log.Printf("[Config] Warning: could not read %s: %v — using environment and defaults", ConfigFile, err)
	}

	cfg.IsPremium = license.Validate(cfg.LicenseKey)
	cfg.IsInitialized = checkInitialized(cfg.ScannerProcessedFile)

	return cfg
}

func (c *Config) loadFromDisk() error {
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return err
	}

	importJSON := &Config{}
	if err := json.Unmarshal(data, importJSON); err != nil {
		return err
	}

	// Which keys are actually present in the JSON. Needed to tell an explicit
	// false or 0 from a missing field, since both unmarshal to the zero value.
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(data, &rawFields)
	present := func(key string) bool {
		_, ok := rawFields[key]
		return ok
	}

	// Warn if the on-disk schema is older than the current version.
	if importJSON.SchemaVersion == 0 {
		log.Printf("[Config] Warning: config file has no schemaVersion; treating as version 0. Current version is %d.", currentSchemaVersion)
	} else if importJSON.SchemaVersion < currentSchemaVersion {
		log.Printf("[Config] Warning: config file schema version %d is older than current version %d. Some defaults may apply.", importJSON.SchemaVersion, currentSchemaVersion)
	}

	// The environment has already populated c. Anything it explicitly set wins
	// over the file, and every disagreement is recorded.
	m := &merger{}

	m.str(&c.Port, importJSON.Port, "PORT", "port")
	m.str(&c.SourceDir, importJSON.SourceDir, "SOURCE_DIR", "sourceDir")
	m.str(&c.DestDir, importJSON.DestDir, "DEST_DIR", "destDir")

	// GPU keeps its special case: "cpu" and "auto" on disk are not explicit
	// choices, so runtime detection stays in effect.
	if importJSON.GPUVendor != "cpu" && importJSON.GPUVendor != "auto" {
		m.str(&c.GPUVendor, importJSON.GPUVendor, "GPU_VENDOR", "gpuVendor")
	}

	m.str(&c.QualityPreset, importJSON.QualityPreset, "QUALITY_PRESET", "qualityPreset")
	m.integer(&c.CRF, importJSON.CRF, present("crf"), "CRF", "crf")
	m.integer(&c.MaxConcurrentJobs, importJSON.MaxConcurrentJobs, present("maxConcurrentJobs"), "MAX_CONCURRENT_JOBS", "maxConcurrentJobs")

	m.str(&c.AIProvider, importJSON.AIProvider, "AI_PROVIDER", "aiProvider")
	m.str(&c.AIApiKey, importJSON.AIApiKey, "AI_API_KEY", "aiApiKey")
	m.str(&c.AIEndpoint, importJSON.AIEndpoint, "AI_ENDPOINT", "aiEndpoint")
	m.str(&c.AIModel, importJSON.AIModel, "AI_MODEL", "aiModel")

	m.str(&c.AdminPassword, importJSON.AdminPassword, "ADMIN_PASSWORD", "adminPassword")
	m.str(&c.LicenseKey, importJSON.LicenseKey, "LICENSE_KEY", "licenseKey")

	m.boolean(&c.ScannerEnabled, importJSON.ScannerEnabled, present("scannerEnabled"), "SCANNER_ENABLED", "scannerEnabled")
	m.str(&c.ScannerMode, importJSON.ScannerMode, "SCANNER_MODE", "scannerMode")
	m.integer(&c.ScannerIntervalSec, importJSON.ScannerIntervalSec, present("scannerIntervalSec"), "SCANNER_INTERVAL_SEC", "scannerIntervalSec")
	m.boolean(&c.ScannerAutoCreate, importJSON.ScannerAutoCreate, present("scannerAutoCreate"), "SCANNER_AUTO_CREATE", "scannerAutoCreate")
	m.str(&c.ScannerProcessedFile, importJSON.ScannerProcessedFile, "SCANNER_PROCESSED_FILE", "scannerProcessedFile")

	m.boolean(&c.VerifyOutput, importJSON.VerifyOutput, present("verifyOutput"), "VERIFY_OUTPUT", "verifyOutput")
	m.boolean(&c.DeleteSource, importJSON.DeleteSource, present("deleteSource"), "DELETE_SOURCE", "deleteSource")
	m.boolean(&c.AutoConvertISO, importJSON.AutoConvertISO, present("autoConvertISO"), "AUTO_CONVERT_ISO", "autoConvertISO")
	m.boolean(&c.OverrideAICRF, importJSON.OverrideAICRF, present("overrideAICRF"), "OVERRIDE_AI_CRF", "overrideAICRF")
	m.boolean(&c.SkipHighResolution, importJSON.SkipHighResolution, present("skipHighResolution"), "SKIP_HIGH_RESOLUTION", "skipHighResolution")
	m.integer(&c.ResolutionHeightThreshold, importJSON.ResolutionHeightThreshold, present("resolutionHeightThreshold"), "RESOLUTION_HEIGHT_THRESHOLD", "resolutionHeightThreshold")
	m.float(&c.SavingsFloor, importJSON.SavingsFloor, present("savingsFloor"), "SAVINGS_FLOOR", "savingsFloor")
	m.float(&c.DensityFloor, importJSON.DensityFloor, present("densityFloor"), "DENSITY_FLOOR", "densityFloor")

	m.boolean(&c.ReplaceInPlace, importJSON.ReplaceInPlace, present("replaceInPlace"), "REPLACE_IN_PLACE", "replaceInPlace")
	m.str(&c.HoldingDir, importJSON.HoldingDir, "HOLDING_DIR", "holdingDir")
	m.integer(&c.PUID, importJSON.PUID, present("puid"), "PUID", "puid")
	m.integer(&c.PGID, importJSON.PGID, present("pgid"), "PGID", "pgid")

	m.str(&c.SubtitleMode, importJSON.SubtitleMode, "SUBTITLE_MODE", "subtitleMode")
	m.str(&c.SubtitleLang, importJSON.SubtitleLang, "SUBTITLE_LANG", "subtitleLang")
	m.str(&c.SubtitleAPIKey, importJSON.SubtitleAPIKey, "SUBTITLE_API_KEY", "subtitleApiKey")
	m.str(&c.SubtitleUsername, importJSON.SubtitleUsername, "SUBTITLE_USERNAME", "subtitleUsername")
	m.str(&c.SubtitlePassword, importJSON.SubtitlePassword, "SUBTITLE_PASSWORD", "subtitlePassword")

	if present("schedule") {
		c.Schedule = importJSON.Schedule
	}

	m.report()
	c.lastMerge = m

	return nil
}

func (c *Config) Save() error {
	c.rLock()
	data, err := json.MarshalIndent(c, "", "  ")
	c.rUnlock()
	if err != nil {
		return err
	}

	// Ensure /data exists
	dir := filepath.Dir(ConfigFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigFile)
}

func checkInitialized(processedFile string) bool {
	dir := filepath.Dir(processedFile)
	initFile := filepath.Join(dir, ".initialized")
	_, err := os.Stat(initFile)
	return err == nil
}

// MarkInitialized creates the .initialized file
func (c *Config) MarkInitialized() error {
	dir := filepath.Dir(c.ScannerProcessedFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	initFile := filepath.Join(dir, ".initialized")
	c.WithLock(func() { c.IsInitialized = true })
	return os.WriteFile(initFile, []byte(time.Now().Format(time.RFC3339)), 0600)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return fallback
}
