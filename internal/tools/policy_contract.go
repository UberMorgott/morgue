package tools

import "errors"

const InstalledLockVersion = 2
const RunLockVersion = 1

var ErrInUse = errors.New("tool is in use")
var ErrSelectionClosed = errors.New("run tool selection is closed")

// InstallOptions.Version is an operator override. Empty clears an existing
// override only after a successful install; failure preserves it.
type InstallOptions struct {
	Version string `json:"version,omitempty"`
}

// ToolIdentity describes the payload actually selected, never an upstream alias.
// Runtime fields are a sorted aggregate when more than one runtime is required.
type ToolIdentity struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Source         string `json:"source"`
	PayloadSHA256  string `json:"payloadSha256"`
	RuntimeVersion string `json:"runtimeVersion"`
	RuntimeSHA256  string `json:"runtimeSha256"`
	Managed        bool   `json:"managed"`
	HashScope      string `json:"hashScope"`
}

type InstalledLock struct {
	SchemaVersion    int                     `json:"schemaVersion"`
	Tools            map[string]ToolIdentity `json:"tools"`
	OperatorVersions map[string]string       `json:"operatorVersions"`
}

// RunToolsLock is copied to the workspace before invocation and at completion.
// Snapshot(false) is provisional; Snapshot(true) is final and sorted by name.
type RunToolsLock struct {
	SchemaVersion int            `json:"schemaVersion"`
	Complete      bool           `json:"complete"`
	Tools         []ToolIdentity `json:"tools"`
}

// ToolPolicyStatus is also delivered through InstallCallbacks.OnStatus.
// Freshness: unchecked|checking|current|updated|pinned|system|warning|error.
type ToolPolicyStatus struct {
	Name            string `json:"name"`
	Freshness       string `json:"freshness"`
	Warning         string `json:"warning,omitempty"`
	OperatorVersion string `json:"operatorVersion,omitempty"`
	InUse           bool   `json:"inUse"`
}

// KnownHelper is intentionally separate from the external tool registry.
func KnownHelper(name string) bool {
	return name == "cfxextract" || name == "cfxstrings" || name == "cfxcflow"
}
