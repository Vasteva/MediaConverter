package api

import (
	"sync"
	"time"
)

type rateLimitEntry struct {
	attempts int
	lastReset time.Time
}

type RateLimiter struct {
	mu sync.Mutex
	ips map[string]*rateLimitEntry
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		ips: make(map[string]*rateLimitEntry),
	}
}

func (rl *RateLimiter) Check(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

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
