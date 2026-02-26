package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/jobs"
	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

// sseClient represents a single connected SSE subscriber.
type sseClient struct {
	ch chan []byte
}

// SSEBroadcaster distributes job update events to all connected SSE clients.
type SSEBroadcaster struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

func NewSSEBroadcaster() *SSEBroadcaster {
	return &SSEBroadcaster{
		clients: make(map[*sseClient]struct{}),
	}
}

func (b *SSEBroadcaster) subscribe() *sseClient {
	client := &sseClient{ch: make(chan []byte, 32)}
	b.mu.Lock()
	b.clients[client] = struct{}{}
	b.mu.Unlock()
	return client
}

func (b *SSEBroadcaster) unsubscribe(client *sseClient) {
	b.mu.Lock()
	delete(b.clients, client)
	b.mu.Unlock()
}

// Broadcast serialises a job and fans it out to every connected client.
// Slow clients whose channel is full are skipped to avoid blocking the job pipeline.
func (b *SSEBroadcaster) Broadcast(job *jobs.Job) {
	data, err := json.Marshal(job)
	if err != nil {
		log.Printf("[SSE] Failed to marshal job %s: %v", job.ID, err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for client := range b.clients {
		select {
		case client.ch <- data:
		default:
		}
	}
}

// RegisterSSERoute adds GET /api/events to the app.
// Authentication uses ?token= because EventSource does not support custom headers.
func RegisterSSERoute(app *fiber.App, broadcaster *SSEBroadcaster, jm *jobs.Manager, cfg *config.Config) {
	app.Get("/api/events", func(c *fiber.Ctx) error {
		token := c.Query("token")
		if !validateSSEToken(token, cfg.AdminPassword) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
		}

		client := broadcaster.subscribe()

		c.Set(fiber.HeaderContentType, "text/event-stream")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderConnection, "keep-alive")
		c.Set("X-Accel-Buffering", "no") // Prevent nginx from buffering the stream

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			defer broadcaster.unsubscribe(client)

			// Send all current jobs immediately so the client has a complete snapshot.
			if jm != nil {
				for _, job := range jm.GetAllJobs() {
					data, err := json.Marshal(job)
					if err == nil {
						fmt.Fprintf(w, "event: job\ndata: %s\n\n", data)
					}
				}
				if err := w.Flush(); err != nil {
					return
				}
			}

			// Keepalive ping every 15 s prevents proxies from closing idle connections.
			keepalive := time.NewTicker(15 * time.Second)
			defer keepalive.Stop()

			for {
				select {
				case data := <-client.ch:
					if _, err := fmt.Fprintf(w, "event: job\ndata: %s\n\n", data); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-keepalive.C:
					if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		}))

		return nil
	})
}
