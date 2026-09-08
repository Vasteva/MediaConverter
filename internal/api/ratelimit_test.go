package api

import (
	"testing"
	"time"
)

// TestRateLimiterBlocksAfterFiveAttempts covers the existing, intended
// behavior: a sixth attempt within the same one-minute window is rejected.
func TestRateLimiterBlocksAfterFiveAttempts(t *testing.T) {
	rl := NewRateLimiter()
	for i := 0; i < 5; i++ {
		if !rl.Check("1.2.3.4") {
			t.Fatalf("attempt %d: expected to be allowed", i+1)
		}
	}
	if rl.Check("1.2.3.4") {
		t.Error("6th attempt within the window should have been rejected")
	}
}

// TestRateLimiterEvictionBoundsMapGrowth covers #50: nothing ever removed a
// stale IP's entry, so a public login endpoint accumulated one entry per
// attacker-controlled IP forever, for as long as the process ran.
// evictStaleLocked runs at most once per rateLimiterEvictionInterval, so
// this drives it directly rather than waiting out the real interval.
func TestRateLimiterEvictionBoundsMapGrowth(t *testing.T) {
	rl := NewRateLimiter()

	stale := time.Now().Add(-staleAfter - time.Minute)
	for i := 0; i < 10; i++ {
		ip := string(rune('a' + i))
		rl.ips[ip] = &rateLimitEntry{attempts: 5, lastReset: stale}
	}
	if len(rl.ips) != 10 {
		t.Fatalf("set up %d stale entries, want 10", len(rl.ips))
	}

	// Force the eviction sweep to run on the next Check regardless of how
	// much wall-clock time has actually elapsed.
	rl.lastEviction = time.Time{}

	if !rl.Check("fresh-ip") {
		t.Fatal("expected a first attempt from a new IP to be allowed")
	}

	if got := len(rl.ips); got != 1 {
		t.Errorf("%d entries remain after eviction, want 1 (only the fresh IP)", got)
	}
	if _, ok := rl.ips["fresh-ip"]; !ok {
		t.Error("the fresh IP's own entry should not have been evicted")
	}
}
