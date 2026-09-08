package api

import (
	"testing"
	"time"
)

// TestSessionStoreIssueAndValid covers the basic contract #50's SessionStore
// replaces the old password-derived token with: a freshly issued token is
// valid, an unrelated string is not, and an empty token is never valid
// regardless of what's in the store.
func TestSessionStoreIssueAndValid(t *testing.T) {
	s := NewSessionStore()

	token, err := s.Issue(time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !s.Valid(token) {
		t.Error("expected a freshly issued token to be valid")
	}
	if s.Valid("some-other-token") {
		t.Error("expected an unrelated token to be invalid")
	}
	if s.Valid("") {
		t.Error("expected an empty token to be invalid")
	}
}

// TestSessionStoreIssueIsRandom covers the core defect in the old scheme:
// sha256(password + date) meant every session issued on the same day was
// the identical token, computable by anyone who knew the password without
// ever logging in. Two tokens issued back to back must differ.
func TestSessionStoreIssueIsRandom(t *testing.T) {
	s := NewSessionStore()

	a, err := s.Issue(time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	b, err := s.Issue(time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if a == b {
		t.Error("two issued tokens were identical — tokens must be random, not derived from anything reproducible")
	}
}

// TestSessionStoreExpiry covers the token lifetime: a token issued with a
// negative TTL (i.e. already in the past) must be rejected, and Valid must
// clean it up rather than leaving a dead entry behind.
func TestSessionStoreExpiry(t *testing.T) {
	s := NewSessionStore()

	token, err := s.Issue(-time.Second)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if s.Valid(token) {
		t.Error("expected an already-expired token to be invalid")
	}
	s.mu.Lock()
	_, stillPresent := s.sessions[token]
	s.mu.Unlock()
	if stillPresent {
		t.Error("Valid should have removed the expired entry")
	}
}

// TestSessionStoreRevoke covers #50's actual new capability: the old scheme
// had no server-side record of issued tokens at all, so there was nothing to
// revoke — a leaked token stayed valid until its date-based window lapsed on
// its own, up to 48h later. Revoke must take effect immediately.
func TestSessionStoreRevoke(t *testing.T) {
	s := NewSessionStore()

	token, err := s.Issue(time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !s.Valid(token) {
		t.Fatal("test setup: token should be valid before revocation")
	}

	s.Revoke(token)

	if s.Valid(token) {
		t.Error("expected a revoked token to be invalid immediately")
	}

	// Revoking an unknown token, or the same one twice, must not panic.
	s.Revoke(token)
	s.Revoke("never-issued")
}

// TestSessionStoreEvictionBoundsMapGrowth covers the same unbounded-map
// defect #50 fixes in RateLimiter: nothing ever removed an expired session,
// so the store grew by one entry per login (or per SSE reconnect) forever.
// evictExpiredLocked runs at most once per evictionInterval, so this drives
// it directly rather than waiting out the real interval.
func TestSessionStoreEvictionBoundsMapGrowth(t *testing.T) {
	s := NewSessionStore()

	for i := 0; i < 10; i++ {
		if _, err := s.Issue(-time.Second); err != nil { // already expired
			t.Fatalf("Issue: %v", err)
		}
	}
	s.mu.Lock()
	before := len(s.sessions)
	s.mu.Unlock()
	if before != 10 {
		t.Fatalf("set up %d sessions, want 10", before)
	}

	// Force the eviction sweep to run on the next Issue regardless of how
	// much wall-clock time has actually elapsed.
	s.mu.Lock()
	s.lastEviction = time.Time{}
	s.mu.Unlock()

	if _, err := s.Issue(time.Hour); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	s.mu.Lock()
	after := len(s.sessions)
	s.mu.Unlock()
	// The 10 expired sessions should be gone, leaving only the one just issued.
	if after != 1 {
		t.Errorf("%d sessions remain after eviction, want 1 (the freshly issued one)", after)
	}
}
