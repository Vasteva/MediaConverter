package subtitles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPClientEnforcesTimeout covers #49: http.DefaultClient has no
// Timeout, so an unresponsive subtitle host — or a download link that never
// answers — hung its caller indefinitely. fetchContent is the one call here
// that takes its URL directly rather than through the fixed osBaseURL
// constant, so it's the one that can be pointed at a local test server. This
// confirms the shared httpClient's Timeout actually bounds the call, shrunk
// for the duration of this test rather than waiting out the real (30s)
// production default.
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

	d := &Downloader{}
	done := make(chan error, 1)
	go func() {
		_, err := d.fetchContent(context.Background(), srv.URL)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchContent did not return within 5s — the client timeout was not enforced")
	}
}
