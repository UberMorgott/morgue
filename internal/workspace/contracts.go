// Package workspace owns durable run output, locking and journal projection.
package workspace

import (
	"errors"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

var (
	ErrLocked   = errors.New("workspace is locked")
	ErrNotEmpty = errors.New("workspace is not empty")
	ErrNoEvents = errors.New("workspace has no events")
)

// Sink is the engine's persistence boundary. Append precedes notification.
type Sink interface {
	Append(runmodel.Event) error
	Snapshot() (runmodel.Workspace, error)
	Close() error
}

type Recovery struct {
	Workspace   runmodel.Workspace `json:"workspace"`
	LastSeq     uint64             `json:"lastSeq"`
	Interrupted bool               `json:"interrupted"`
	TornTail    bool               `json:"tornTail"`
	Active      bool               `json:"active"`
	Diagnostics []string           `json:"diagnostics"`
}

type EventPage struct {
	RunID   string           `json:"runId"`
	Events  []runmodel.Event `json:"events"`
	LastSeq uint64           `json:"lastSeq"`
	HasMore bool             `json:"hasMore"`
}
