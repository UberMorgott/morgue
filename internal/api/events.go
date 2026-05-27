package api

import (
	"fmt"
	"net/http"
	"sync"
)

// EventBroadcaster fans out server-sent events to connected clients.
type EventBroadcaster struct {
	mu       sync.RWMutex
	clients  map[chan string]struct{}
	shutdown chan struct{}
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

	ch := make(chan string, 64)

	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()

	defer func() {
		eb.mu.Lock()
		delete(eb.clients, ch)
		eb.mu.Unlock()
	}()

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
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
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)

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
func (eb *EventBroadcaster) Shutdown() {
	close(eb.shutdown)

	eb.mu.Lock()
	defer eb.mu.Unlock()

	for ch := range eb.clients {
		close(ch)
		delete(eb.clients, ch)
	}
}
