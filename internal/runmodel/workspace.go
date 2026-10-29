package runmodel

import "time"

const (
	WorkspaceVersion = 1
	JournalVersion   = 1
	JournalPath      = ".morgue/journal.ndjson"
	RunToolsLockPath = ".morgue/tools.lock.json"
	ManifestPath     = "morgue.json"
)

type AppInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type Workspace struct {
	SchemaVersion  int            `json:"schemaVersion"`
	JournalVersion int            `json:"journalVersion"`
	EventVersion   int            `json:"eventVersion"`
	App            AppInfo        `json:"app"`
	Run            RunRecord      `json:"run"`
	Spec           RunSpec        `json:"spec"`
	LastSeq        uint64         `json:"lastSeq"`
	Journal        string         `json:"journal"`
	ToolsLock      string         `json:"toolsLock"`
	Targets        []TargetRecord `json:"targets"`
	Artifacts      []Artifact     `json:"artifacts"`
	Warnings       []string       `json:"warnings"`
}

type RunRecord struct {
	ID          string     `json:"id"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	DurationMs  int64      `json:"durationMs"`
	State       string     `json:"state"`
	Outcome     *string    `json:"outcome"`
	Error       string     `json:"error"`
	Targets     Counts     `json:"targets"`
	LeafTargets Counts     `json:"leafTargets"`
}

type FileCounts struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

type InputFile struct {
	Role       string  `json:"role"`
	Path       string  `json:"path"`
	SHA256     *string `json:"sha256"`
	SizeBytes  int64   `json:"sizeBytes"`
	HashStatus string  `json:"hashStatus"`
	Error      string  `json:"error"`
}

type TargetRecord struct {
	ID             string         `json:"id"`
	ParentTargetID *string        `json:"parentTargetId"`
	InputPath      string         `json:"inputPath"`
	RelativeInput  string         `json:"relativeInput"`
	SHA256         *string        `json:"sha256"`
	InputSet       []InputFile    `json:"inputSet"`
	Kind           string         `json:"kind"`
	Recipe         string         `json:"recipe"`
	State          string         `json:"state"`
	Outcome        *string        `json:"outcome"`
	SkipReason     string         `json:"skipReason"`
	Error          string         `json:"error"`
	Output         string         `json:"output"`
	StartedAt      *time.Time     `json:"startedAt"`
	FinishedAt     *time.Time     `json:"finishedAt"`
	DurationMs     int64          `json:"durationMs"`
	Files          FileCounts     `json:"files"`
	Quality        *QualityReport `json:"quality"`
	Provenance     *string        `json:"provenance"`
}
