package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// sseKeepAlive is how often an idle SSE stream gets a comment ping, so proxies
// and clients see a live connection and dead peers are detected promptly.
var sseKeepAlive = 20 * time.Second

// EventBroadcaster fans out server-sent events to connected clients.
type EventBroadcaster struct {
	mu           sync.RWMutex
	clients      map[chan string]struct{}
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// NewEventBroadcaster creates an EventBroadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		clients:  make(map[chan string]struct{}),
		shutdown: make(chan struct{}),
	}
}

// HandleSSE is the HTTP handler for the SSE endpoint.
func (eb *EventBroadcaster) HandleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// The server-wide WriteTimeout would kill the stream after 120 s; SSE is
	// long-lived by design, so lift the deadline for this response only.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	ch := make(chan string, 64)

	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()

	defer func() {
		eb.mu.Lock()
		delete(eb.clients, ch)
		eb.mu.Unlock()
	}()

	// Flush headers immediately so clients know the stream is open.
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(sseKeepAlive)
	defer ping.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-eb.shutdown:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// Broadcast sends an event to all connected SSE clients.
// Messages are dropped if a client channel is full (non-blocking send).
func (eb *EventBroadcaster) Broadcast(event string, data []byte) {
	eb.send(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
}

// BroadcastWithID is Broadcast with an SSE id field (events v2 use
// "<run>:<seq>" so clients can detect gaps and reconcile via /api/run/status).
func (eb *EventBroadcaster) BroadcastWithID(event, id string, data []byte) {
	eb.send(fmt.Sprintf("event: %s\nid: %s\ndata: %s\n\n", event, id, data))
}

func (eb *EventBroadcaster) send(msg string) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for ch := range eb.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Shutdown closes the shutdown channel and disconnects all clients.
// Safe to call multiple times.
func (eb *EventBroadcaster) Shutdown() {
	eb.shutdownOnce.Do(func() {
		close(eb.shutdown)

		eb.mu.Lock()
		defer eb.mu.Unlock()

		for ch := range eb.clients {
			close(ch)
			delete(eb.clients, ch)
		}
	})
}

// broadcastRunEvent relays one events v2 record to SSE clients as
// "event: pipeline:event:v2", "id: <run>:<seq>" and the identical JSON the
// Wails webview receives. A slow client may miss notifications (non-blocking
// fan-out); it reconciles through GET /api/run/status lastSeq.
func (s *Server) broadcastRunEvent(ev runmodel.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.events.BroadcastWithID(runmodel.EventName, fmt.Sprintf("%s:%d", ev.Run, ev.Seq), data)
}
