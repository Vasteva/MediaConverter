package api

import (
	"sync"
	"time"
)

type rateLimitEntry struct {
	attempts  int
	lastReset time.Time
}

// staleAfter bounds how long an inactive IP's entry survives. The reset
// window itself is one minute, so anything older than this has been
// inactive for a while and is safe to drop.
const staleAfter = 10 * time.Minute

// evictionInterval caps how often Check pays the cost of sweeping the map.
const rateLimiterEvictionInterval = 10 * time.Minute

// RateLimiter is a fixed-window limiter keyed by client IP.
//
// Every distinct IP that ever calls Check gets an entry that, before this
// fix, was never removed — a public login endpoint accumulated one entry
// per attacker-controlled IP forever, for as long as the process ran (#50).
type RateLimiter struct {
	mu           sync.Mutex
	ips          map[string]*rateLimitEntry
	lastEviction time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		ips: make(map[string]*rateLimitEntry),
	}
}

func (rl *RateLimiter) Check(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.evictStaleLocked()

	entry, ok := rl.ips[ip]
	if !ok {
		rl.ips[ip] = &rateLimitEntry{
			attempts:  1,
			lastReset: time.Now(),
		}
		return true
	}

	// Reset every minute
	if time.Since(entry.lastReset) > time.Minute {
		entry.attempts = 1
		entry.lastReset = time.Now()
		return true
	}

	if entry.attempts >= 5 {
		return false
	}

	entry.attempts++
	return true
}

// evictStaleLocked removes entries inactive for longer than staleAfter, at
// most once per rateLimiterEvictionInterval. Called with mu already held.
func (rl *RateLimiter) evictStaleLocked() {
	if time.Since(rl.lastEviction) < rateLimiterEvictionInterval {
		return
	}
	now := time.Now()
	for ip, entry := range rl.ips {
		if now.Sub(entry.lastReset) > staleAfter {
			delete(rl.ips, ip)
		}
	}
	rl.lastEviction = now
}
