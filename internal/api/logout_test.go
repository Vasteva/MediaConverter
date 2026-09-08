package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// authedRequest builds a request carrying token as a Bearer token.
func authedRequest(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// TestLogoutRevokesToken covers #50's actual new capability end to end,
// through the real app: the old password-derived token scheme had no
// server-side record of issued tokens, so there was nothing a logout
// endpoint could have revoked even if one had existed. A request that
// succeeds before logout must be rejected with the same token afterward.
func TestLogoutRevokesToken(t *testing.T) {
	app, token, _ := newTestApp(t)

	resp, err := app.Test(authedRequest(http.MethodGet, "/api/jobs", token))
	if err != nil {
		t.Fatalf("app.Test (before logout): %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("before logout: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = app.Test(authedRequest(http.MethodPost, "/api/logout", token))
	if err != nil {
		t.Fatalf("app.Test (logout): %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = app.Test(authedRequest(http.MethodGet, "/api/jobs", token))
	if err != nil {
		t.Fatalf("app.Test (after logout): %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("after logout: status = %d, want %d — the revoked token still worked", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestLoginIssuesFreshTokenEachTime covers the core defect in the old
// scheme: sha256(password + date) meant every login on the same day
// returned the identical token. Two separate logins must not.
func TestLoginIssuesFreshTokenEachTime(t *testing.T) {
	app, firstToken, _ := newTestApp(t)

	secondToken, err := login(app, "test-password")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if firstToken == secondToken {
		t.Error("two separate logins returned the identical token — tokens must be random per session, not derived from the password")
	}

	// Both are independently valid — logging in again must not invalidate
	// the caller's other open session.
	for _, tok := range []string{firstToken, secondToken} {
		resp, err := app.Test(authedRequest(http.MethodGet, "/api/jobs", tok))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("token %q: status = %d, want %d", tok, resp.StatusCode, http.StatusOK)
		}
	}
}

// TestInvalidTokenRejected covers the ordinary auth gate, now backed by
// SessionStore.Valid instead of a recomputed hash comparison.
func TestInvalidTokenRejected(t *testing.T) {
	app, _, _ := newTestApp(t)

	resp, err := app.Test(authedRequest(http.MethodGet, "/api/jobs", "not-a-real-token"))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
