package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionTTL is how long an issued login session token remains valid. There
// is no idle-timeout / activity-extension concept — this is a hard cap from
// issuance.
const SessionTTL = 24 * time.Hour

// SSETokenTTL is deliberately much shorter than SessionTTL: an SSE token
// travels in a URL query string (EventSource cannot set a custom
// Authorization header), which is far more likely to end up in a server
// access log, proxy log, or browser history entry than a header ever would.
const SSETokenTTL = 2 * time.Minute

// evictionInterval bounds how often Issue sweeps expired sessions from the
// map. Without this, the store would grow by one entry per login (or per SSE
// reconnect) for as long as the process runs — the same unbounded-map defect
// #50 also fixes in RateLimiter.
const evictionInterval = 10 * time.Minute

// SessionStore is a server-side registry of issued auth tokens.
//
// It replaces a scheme where a token was sha256(adminPassword + today's
// date): deterministic, so anyone who knew the password could compute a
// valid token without ever logging in; impossible to revoke individually,
// since there was nothing to revoke — only the derivation existed, not a
// record of who held a token; and worst of all, a leaked token was itself an
// offline brute-force oracle for the password, since checking a candidate
// password against a stolen token never had to touch the rate-limited login
// endpoint at all (#50).
//
// Tokens here are random and carry no information about the password —
// this store is the only thing that says whether one is currently valid.
type SessionStore struct {
	mu           sync.Mutex
	sessions     map[string]time.Time // token -> expiry
	lastEviction time.Time
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]time.Time)}
}

// Issue generates a new random token valid for ttl and records it.
func (s *SessionStore) Issue(ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()
	s.sessions[token] = time.Now().Add(ttl)
	return token, nil
}

// Valid reports whether token exists and has not expired. An expired token
// found here is removed immediately rather than waiting for the next sweep.
func (s *SessionStore) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.sessions, token)
		return false
	}
	return true
}

// Revoke removes a token immediately — e.g. on logout — closing a gap the
// old password-derived scheme had no way to fill at all: there was nothing
// to revoke, so a leaked token stayed valid until its date-based window
// lapsed on its own, up to 48h later.
func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// evictExpiredLocked removes expired entries, at most once per
// evictionInterval. Called with mu already held.
func (s *SessionStore) evictExpiredLocked() {
	if time.Since(s.lastEviction) < evictionInterval {
		return
	}
	now := time.Now()
	for token, expiry := range s.sessions {
		if now.After(expiry) {
			delete(s.sessions, token)
		}
	}
	s.lastEviction = now
}
