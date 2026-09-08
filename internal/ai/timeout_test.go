package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPClientEnforcesTimeout covers #49: http.DefaultClient has no
// Timeout, so an unresponsive AI endpoint hung its caller indefinitely — on
// the job path specifically, that meant a stuck worker forever, not just a
// stuck request. This points a provider at a server that never responds and
// confirms the shared httpClient actually bounds the call, rather than
// letting it hang for the real (120s) production default, which is shrunk
// for the duration of this test.
func TestHTTPClientEnforcesTimeout(t *testing.T) {
	old := httpClient.Timeout
	httpClient.Timeout = 100 * time.Millisecond
	defer func() { httpClient.Timeout = old }()

	blockForever := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockForever // never respond
	}))
	// LIFO: unblock the handler before Close() waits on it.
	defer srv.Close()
	defer close(blockForever)

	p := NewOpenAIProvider("test-key", srv.URL, "gpt-4o")

	done := make(chan error, 1)
	go func() {
		_, err := p.Analyze(context.Background(), "test prompt")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Analyze did not return within 5s — the client timeout was not enforced")
	}
}
