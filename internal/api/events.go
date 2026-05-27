package api

import "net/http"

// EventBroadcaster fans out server-sent events to connected clients.
type EventBroadcaster struct{}

// NewEventBroadcaster creates an EventBroadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{}
}

// HandleSSE is the HTTP handler for the SSE endpoint. Stub.
func (eb *EventBroadcaster) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// TODO: implement SSE streaming
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Broadcast sends an event to all connected SSE clients. Stub.
func (eb *EventBroadcaster) Broadcast(event string, data []byte) {
	// TODO: implement broadcasting
}
