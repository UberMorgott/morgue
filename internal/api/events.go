package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	clients      map[chan sseMsg]struct{}
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// sseMsg is one framed SSE message; run/seq are set for events v2 records so
// a reconnected stream can drop replayed duplicates and detect gaps.
type sseMsg struct {
	run  string
	seq  uint64
	text string
}

// sseReplay resumes one run's events v2 stream from the journal after seq.
// write emits every journal event of run after seq in live SSE framing and
// returns the last seq written.
type sseReplay struct {
	run   string
	after uint64
	write func(w io.Writer, run string, after uint64) (uint64, error)
}

// clientBuffer bounds queued notifications per SSE client. A full buffer
// drops notifications; a replaying client fills gaps from the journal.
const clientBuffer = 256

// NewEventBroadcaster creates an EventBroadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		clients:  make(map[chan sseMsg]struct{}),
		shutdown: make(chan struct{}),
	}
}

// HandleSSE is the HTTP handler for the live SSE endpoint (no replay).
func (eb *EventBroadcaster) HandleSSE(w http.ResponseWriter, r *http.Request) {
	eb.serve(w, r, nil)
}

// serve streams live events. With rp it first subscribes, then replays the
// journal after rp.after, then delivers live records of rp.run in strict
// sequence: duplicates (seq <= last written) are dropped and a gap (a dropped
// notification) is filled from the journal before the next live record.
func (eb *EventBroadcaster) serve(w http.ResponseWriter, r *http.Request, rp *sseReplay) {
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

	ch := make(chan sseMsg, clientBuffer)

	// Subscribe before replay so no record committed after the replay read
	// can be missed; overlap is removed by sequence dedupe below.
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

	var last uint64
	if rp != nil {
		var err error
		if last, err = rp.write(w, rp.run, rp.after); err != nil {
			replayFailed(w, err)
			return
		}
		flusher.Flush()
	}

	ping := time.NewTicker(sseKeepAlive)
	defer ping.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if rp != nil && msg.run == rp.run && msg.seq != 0 {
				if msg.seq <= last {
					continue // already replayed
				}
				if msg.seq > last+1 {
					var err error
					if last, err = rp.write(w, rp.run, last); err != nil {
						replayFailed(w, err)
						return
					}
					if msg.seq <= last {
						flusher.Flush()
						continue
					}
				}
				last = msg.seq
			}
			if _, err := fmt.Fprint(w, msg.text); err != nil {
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

// replayFailed ends a stream whose journal replay failed with an SSE comment;
// the client reconnects with its Last-Event-ID.
func replayFailed(w io.Writer, err error) {
	_, _ = fmt.Fprintf(w, ": replay failed: %s\n\n", strings.ReplaceAll(err.Error(), "\n", " "))
}

// Broadcast sends an event to all connected SSE clients.
// Messages are dropped if a client channel is full (non-blocking send).
func (eb *EventBroadcaster) Broadcast(event string, data []byte) {
	eb.send(sseMsg{text: fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)})
}

// broadcastV2 sends one events v2 record with SSE id "<run>:<seq>" so clients
// can resume with Last-Event-ID and reconcile gaps.
func (eb *EventBroadcaster) broadcastV2(run string, seq uint64, data []byte) {
	eb.send(sseMsg{run: run, seq: seq, text: fmt.Sprintf("event: %s\nid: %s:%d\ndata: %s\n\n", runmodel.EventName, run, seq, data)})
}

func (eb *EventBroadcaster) send(msg sseMsg) {
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
// Wails webview receives. A slow live client may miss notifications
// (non-blocking fan-out); it reconnects with Last-Event-ID or reconciles
// through GET /api/run/status lastSeq.
func (s *Server) broadcastRunEvent(ev runmodel.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.events.broadcastV2(ev.Run, ev.Seq, data)
}

// handleEvents serves GET /api/events. A Last-Event-ID header ("<run>:<seq>",
// sent by EventSource on reconnect) resumes the current run from its journal:
// an old run ID is 409 and a malformed ID 400, both before any stream bytes.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Last-Event-ID")
	if id == "" || s.history == nil {
		s.events.serve(w, r, nil)
		return
	}
	run, after, ok := s.history.CheckReplay(w, id)
	if !ok {
		return
	}
	s.events.serve(w, r, &sseReplay{run: run, after: after, write: s.history.WriteReplay})
}
