package runmodel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

const EventVersion = 2
const EventName = "pipeline:event:v2"

type EventType string

const (
	EventRunStarted       EventType = "run.started"
	EventScanCompleted    EventType = "scan.completed"
	EventTargetDiscovered EventType = "target.discovered"
	EventTargetFinished   EventType = "target.finished"
	EventStepProgress     EventType = "step.progress"
	EventToolProgress     EventType = "tool.progress"
	EventArtifactWritten  EventType = "artifact.written"
	EventQualityUpdated   EventType = "quality.updated"
	EventLog              EventType = "log"
	EventRunFinished      EventType = "run.finished"
)

type Event struct {
	Version        int             `json:"v"`
	TS             time.Time       `json:"ts"`
	Seq            uint64          `json:"seq"`
	Run            string          `json:"run"`
	Type           EventType       `json:"type"`
	TargetID       string          `json:"targetId,omitempty"`
	ParentTargetID string          `json:"parentTargetId,omitempty"`
	Data           json.RawMessage `json:"data"`
}

// Payload is sealed: producers cannot substitute an untyped map for a contract.
type Payload interface{ eventType() EventType }

type RunStarted struct {
	Handle RunHandle `json:"handle"`
	Spec   RunSpec   `json:"spec"`
}

func (RunStarted) eventType() EventType { return EventRunStarted }

type ScanCompleted struct {
	FilesTotal  int64 `json:"filesTotal"`
	GroupsTotal int64 `json:"groupsTotal"`
}

func (ScanCompleted) eventType() EventType { return EventScanCompleted }

type TargetDiscovered struct {
	InputPath     string   `json:"inputPath"`
	RelativeInput string   `json:"relativeInput"`
	Kind          string   `json:"kind"`
	Recipe        string   `json:"recipe"`
	Output        string   `json:"output"`
	SizeBytes     int64    `json:"sizeBytes"`
	Compiler      string   `json:"compiler,omitempty"`
	Obfuscator    string   `json:"obfuscator,omitempty"`
	Deobfuscator  string   `json:"deobfuscator,omitempty"`
	RecipeDesc    string   `json:"recipeDesc,omitempty"`
	ToolsNeeded   []string `json:"toolsNeeded,omitempty"`
}

func (TargetDiscovered) eventType() EventType { return EventTargetDiscovered }

type TargetFinished struct {
	Outcome    string `json:"outcome"`
	Recipe     string `json:"recipe"`
	Output     string `json:"output"`
	Error      string `json:"error"`
	SkipReason string `json:"skipReason"`
	DurationMs int64  `json:"durationMs"`
}

func (TargetFinished) eventType() EventType { return EventTargetFinished }

type StepProgress struct {
	StepID     string `json:"stepId"`
	Name       string `json:"name"`
	Tool       string `json:"tool"`
	Unit       string `json:"unit"`
	StepIndex  int64  `json:"stepIndex"`
	StepTotal  int64  `json:"stepTotal"`
	Count      int64  `json:"count"`
	CountTotal int64  `json:"countTotal"`
	DurationMs int64  `json:"durationMs"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

func (StepProgress) eventType() EventType { return EventStepProgress }

type ToolProgress struct {
	Tool       string `json:"tool"`
	Stage      string `json:"stage"`
	Bytes      int64  `json:"bytes"`
	BytesTotal int64  `json:"bytesTotal"`
	Version    string `json:"version"`
	Error      string `json:"error"`
}

func (ToolProgress) eventType() EventType { return EventToolProgress }

type LogRecord struct {
	Level   string `json:"level"`
	Tool    string `json:"tool"`
	Message string `json:"message"`
	Phase   string `json:"phase,omitempty"`
}

func (LogRecord) eventType() EventType { return EventLog }

type RunFinished struct {
	Outcome     string    `json:"outcome"`
	FinishedAt  time.Time `json:"finishedAt"`
	DurationMs  int64     `json:"durationMs"`
	Error       string    `json:"error"`
	Targets     Counts    `json:"targets"`
	LeafTargets Counts    `json:"leafTargets"`
}

func (RunFinished) eventType() EventType { return EventRunFinished }

// Reserved payloads are typed now; M2/M4 will start producing them.
type Artifact struct {
	ID         string   `json:"id"`
	TargetID   *string  `json:"targetId"`
	Path       string   `json:"path"`
	External   bool     `json:"external"`
	Kind       string   `json:"kind"`
	Producer   Producer `json:"producer"`
	SHA256     *string  `json:"sha256"`
	HashStatus string   `json:"hashStatus"`
	Complete   bool     `json:"complete"`
	Warning    string   `json:"warning"`
}
type Producer struct {
	Recipe  string `json:"recipe"`
	StepID  string `json:"stepId"`
	Tool    string `json:"tool"`
	Version string `json:"version"`
}
type ArtifactWritten struct {
	Artifact Artifact `json:"artifact"`
}

func (ArtifactWritten) eventType() EventType { return EventArtifactWritten }

type Evidence struct {
	Path   string `json:"path"`
	Line   *int64 `json:"line"`
	Detail string `json:"detail"`
}
type Metric struct {
	Value    *int64     `json:"value"`
	Status   string     `json:"status"`
	Basis    string     `json:"basis"`
	Evidence []Evidence `json:"evidence"`
}
type Coverage struct {
	Found      Metric   `json:"found"`
	Emitted    Metric   `json:"emitted"`
	Ratio      *float64 `json:"ratio"`
	Comparable bool     `json:"comparable"`
}
type QualityCheck struct {
	ID       string     `json:"id"`
	Status   string     `json:"status"`
	Message  string     `json:"message"`
	Evidence []Evidence `json:"evidence"`
}
type QualityReport struct {
	SchemaVersion        int            `json:"schemaVersion"`
	Family               string         `json:"family"`
	Status               string         `json:"status"`
	Functions            Coverage       `json:"functions"`
	Types                Coverage       `json:"types"`
	DecompilerErrors     Metric         `json:"decompilerErrors"`
	UnknownResultTypes   Metric         `json:"unknownResultTypes"`
	UnresolvedReferences Metric         `json:"unresolvedReferences"`
	ResidualObfuscation  Metric         `json:"residualObfuscation"`
	StringDecryption     Coverage       `json:"stringDecryption"`
	Warnings             []string       `json:"warnings"`
	Checks               []QualityCheck `json:"checks"`
}
type QualityUpdated struct {
	Quality QualityReport `json:"quality"`
}

func (QualityUpdated) eventType() EventType { return EventQualityUpdated }

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// NewEvent validates the envelope and the payload before it enters any transport.
func NewEvent(run string, seq uint64, ts time.Time, targetID, parentID string, payload Payload) (Event, error) {
	if !runIDPattern.MatchString(run) || seq == 0 || ts.IsZero() || payload == nil {
		return Event{}, fmt.Errorf("invalid event envelope")
	}
	switch p := payload.(type) {
	case RunStarted:
		if p.Handle.ID != run || !p.Handle.StartedAt.Equal(ts) || p.Spec.Version != SpecVersion {
			return Event{}, fmt.Errorf("invalid run.started identity/spec")
		}
	case ScanCompleted:
		if p.FilesTotal < 0 || p.GroupsTotal < 0 {
			return Event{}, fmt.Errorf("negative scan counters")
		}
	case TargetDiscovered:
		if targetID == "" || p.SizeBytes < 0 {
			return Event{}, fmt.Errorf("invalid target discovery")
		}
	case TargetFinished:
		if targetID == "" || !validOutcome(p.Outcome, true) || p.DurationMs < 0 {
			return Event{}, fmt.Errorf("invalid target outcome")
		}
	case StepProgress:
		if targetID == "" || p.StepIndex < 0 || p.StepTotal < 0 || p.Count < 0 || p.CountTotal < 0 || p.DurationMs < 0 || !oneOf(p.Status, "pending", "running", "success", "failed", "skipped", "warn") {
			return Event{}, fmt.Errorf("invalid step progress")
		}
	case ToolProgress:
		if p.Bytes < 0 || p.BytesTotal < 0 || !oneOf(p.Stage, "check", "download", "extract", "ready", "failed") {
			return Event{}, fmt.Errorf("invalid tool progress")
		}
	case LogRecord:
		if !oneOf(p.Level, "info", "warn", "error") {
			return Event{}, fmt.Errorf("invalid log level")
		}
	case RunFinished:
		if !validOutcome(p.Outcome, false) || p.DurationMs < 0 || p.FinishedAt.IsZero() || !validCounts(p.Targets) || !validCounts(p.LeafTargets) {
			return Event{}, fmt.Errorf("invalid run outcome")
		}
	case QualityUpdated:
		if targetID == "" {
			return Event{}, fmt.Errorf("quality requires a target")
		}
	case ArtifactWritten:
	default:
		return Event{}, fmt.Errorf("unsupported event payload %T", payload)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Version: EventVersion, TS: ts.UTC(), Seq: seq, Run: run, Type: payload.eventType(), TargetID: targetID, ParentTargetID: parentID, Data: data}, nil
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func validOutcome(s string, skipped bool) bool {
	return oneOf(s, OutcomeSuccess, OutcomeError, OutcomeCancelled) || skipped && s == OutcomeSkipped
}
func validCounts(c Counts) bool {
	return c.Total >= 0 && c.Pending >= 0 && c.Running >= 0 && c.Success >= 0 && c.Error >= 0 && c.Cancelled >= 0 && c.Skipped >= 0 && c.Total == c.Pending+c.Running+c.Success+c.Error+c.Cancelled+c.Skipped
}
