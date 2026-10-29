package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/services"
)

// HistoryHandlers serves the read-only current-run snapshot and bounded
// journal replay. Register them behind the server's auth middleware:
//
//	GET /api/runs/current         -> services.CurrentRun
//	GET /api/runs/current/events  -> {runId,events,lastSeq,hasMore}
type HistoryHandlers struct {
	ws *services.WorkspaceService
}

// NewHistoryHandlers wraps ws.
func NewHistoryHandlers(ws *services.WorkspaceService) *HistoryHandlers {
	return &HistoryHandlers{ws: ws}
}

// Register adds the read-only routes to mux.
func (h *HistoryHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/current", h.HandleCurrent)
	mux.HandleFunc("GET /api/runs/current/events", h.HandleEvents)
}

func historyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrNoCurrentRun):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrStaleRun):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// HandleCurrent returns the on-disk snapshot of the current run. A partial
// projection with a read error is still returned as 500 with the error.
func (h *HistoryHandlers) HandleCurrent(w http.ResponseWriter, _ *http.Request) {
	cur, err := h.ws.Current()
	if err != nil {
		if errors.Is(err, services.ErrNoCurrentRun) {
			historyError(w, err)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "current": cur})
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

// HandleEvents replays ?run=ID&after=SEQ[&limit=N] in pages of at most 1000.
func (h *HistoryHandlers) HandleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	runID := q.Get("run")
	if !validRunID(runID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run must be a 32-char lowercase hex run ID"})
		return
	}
	var after uint64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "after must be a nonnegative integer"})
			return
		}
		after = n
	}
	limit := services.ReplayPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be 1..1000"})
			return
		}
		limit = n
	}
	page, err := h.ws.EventsLimit(runID, after, limit)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func validRunID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ParseLastEventID splits an SSE Last-Event-ID "<run>:<seq>".
func ParseLastEventID(id string) (runID string, seq uint64, ok bool) {
	run, s, found := strings.Cut(strings.TrimSpace(id), ":")
	if !found || !validRunID(run) {
		return "", 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return run, n, true
}

// CheckReplay validates a reconnect before any stream bytes are written:
// an old run ID must produce 409 before streaming. It returns the run and
// sequence to replay from; ok=false means the response was already written.
func (h *HistoryHandlers) CheckReplay(w http.ResponseWriter, lastEventID string) (runID string, after uint64, ok bool) {
	runID, after, parsed := ParseLastEventID(lastEventID)
	if !parsed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Last-Event-ID must be <run>:<seq>"})
		return "", 0, false
	}
	if _, err := h.ws.EventsLimit(runID, after, 1); err != nil {
		historyError(w, err)
		return "", 0, false
	}
	return runID, after, true
}

// WriteReplay writes every journal event of runID after seq in the exact SSE
// framing of live delivery (event pipeline:event:v2, id <run>:<seq>), page by
// page. It returns the last written seq so the caller can drop live
// duplicates (seq <= last) when switching to the broadcaster.
func (h *HistoryHandlers) WriteReplay(w io.Writer, runID string, after uint64) (uint64, error) {
	return services.ReplayEvents(h.ws, runID, after, func(ev runmodel.Event) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: %s\nid: %s:%d\ndata: %s\n\n", runmodel.EventName, ev.Run, ev.Seq, data)
		return err
	})
}
