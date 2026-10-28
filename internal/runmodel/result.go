package runmodel

const (
	OutcomeSuccess   = "success"
	OutcomeError     = "error"
	OutcomeCancelled = "cancelled"
	OutcomeSkipped   = "skipped"
)

type Counts struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Running   int64 `json:"running"`
	Success   int64 `json:"success"`
	Error     int64 `json:"error"`
	Cancelled int64 `json:"cancelled"`
	Skipped   int64 `json:"skipped"`
}

type TargetResult struct {
	ID             string `json:"id"`
	ParentTargetID string `json:"parentTargetId,omitempty"`
	InputPath      string `json:"inputPath"`
	RelativeInput  string `json:"relativeInput"`
	Kind           string `json:"kind"`
	Recipe         string `json:"recipe"`
	Output         string `json:"output"`
	Outcome        string `json:"outcome"`
	Error          string `json:"error"`
	SkipReason     string `json:"skipReason"`
	DurationMs     int64  `json:"durationMs"`
}

type RunResult struct {
	Handle      RunHandle      `json:"handle"`
	Outcome     string         `json:"outcome"`
	Targets     []TargetResult `json:"targets"`
	Counts      Counts         `json:"counts"`
	LeafTargets Counts         `json:"leafTargets"`
	Error       string         `json:"error"`
}
