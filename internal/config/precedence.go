package config

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// merger applies values from the persisted config file to a Config that has
// already been populated from the environment, and records every place the two
// disagree.
//
// Precedence is: an explicitly-set environment variable beats the file.
//
// Before this existed the file won unconditionally and silently. A deployment
// could set MAX_CONCURRENT_JOBS=1 in compose, watch `docker compose config`
// resolve it to "1", and still get two workers because /data/config.json held
// maxConcurrentJobs: 2 — with nothing in the logs to say so. The same mechanism
// made AI_PROVIDER=ollama come up as "openai".
//
// The environment wins because it is the deployment's stated intent and is
// visible in version-controlled compose files, whereas the file is a side effect
// of someone once clicking Save in the UI. Conflicts are logged either way, so
// the losing value is never invisible.
type merger struct {
	conflicts []string
}

// secretKeys never have their values logged. Knowing that a password differs is
// useful; printing either version is not.
var secretKeys = map[string]bool{
	"adminPassword":    true,
	"aiApiKey":         true,
	"licenseKey":       true,
	"subtitleApiKey":   true,
	"subtitlePassword": true,
}

// envIsSet reports whether an environment variable was explicitly provided.
// os.LookupEnv rather than os.Getenv: an empty value is still a deliberate
// choice, and treating it as unset is how "unset it in compose" fails to work.
func envIsSet(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}

func (m *merger) note(jsonKey, envKey string, envValue, fileValue any) {
	if secretKeys[jsonKey] {
		m.conflicts = append(m.conflicts,
			fmt.Sprintf("%s: %s is set and differs from config.json — using %s (values not shown)",
				jsonKey, envKey, envKey))
		return
	}
	m.conflicts = append(m.conflicts,
		fmt.Sprintf("%s: %s=%v overrides config.json value %v", jsonKey, envKey, envValue, fileValue))
}

// str applies a file value to dst unless the environment set it.
// An empty file value means "absent" and is never applied.
func (m *merger) str(dst *string, fileValue, envKey, jsonKey string) {
	if fileValue == "" {
		return
	}
	if envIsSet(envKey) {
		if *dst != fileValue {
			m.note(jsonKey, envKey, *dst, fileValue)
		}
		return
	}
	*dst = fileValue
}

// integer applies a file value to dst unless the environment set it.
// present distinguishes an explicit 0 from an absent key.
func (m *merger) integer(dst *int, fileValue int, present bool, envKey, jsonKey string) {
	if !present {
		return
	}
	if envIsSet(envKey) {
		if *dst != fileValue {
			m.note(jsonKey, envKey, *dst, fileValue)
		}
		return
	}
	*dst = fileValue
}

// boolean applies a file value to dst unless the environment set it.
func (m *merger) boolean(dst *bool, fileValue, present bool, envKey, jsonKey string) {
	if !present {
		return
	}
	if envIsSet(envKey) {
		if *dst != fileValue {
			m.note(jsonKey, envKey, *dst, fileValue)
		}
		return
	}
	*dst = fileValue
}

// report writes a summary of every conflict to the log.
//
// Logged at startup, once, listing each field — a silent override is what made
// this class of problem expensive to diagnose, so the fix is as much about
// visibility as it is about precedence.
func (m *merger) report() {
	if len(m.conflicts) == 0 {
		return
	}
	log.Printf("[Config] %d setting(s) in %s were overridden by the environment:",
		len(m.conflicts), ConfigFile)
	for _, c := range m.conflicts {
		log.Printf("[Config]   %s", c)
	}
	log.Printf("[Config] Environment variables take precedence. To use the stored value instead, " +
		"unset the variable; to stop the file shadowing future changes, remove the key from " + ConfigFile)
}

// Conflicts returns the recorded conflicts, for tests and diagnostics.
func (m *merger) Conflicts() []string { return m.conflicts }

// ConflictSummary renders the conflicts as a single line, or "" when there were
// none.
func (m *merger) ConflictSummary() string {
	if len(m.conflicts) == 0 {
		return ""
	}
	return strings.Join(m.conflicts, "; ")
}
