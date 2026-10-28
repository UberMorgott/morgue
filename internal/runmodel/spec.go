// Package runmodel defines transport-neutral, versioned run contracts.
package runmodel

const SpecVersion = 1

// RunRequest preserves omission of boolean options so config defaults are shared.
type RunRequest struct {
	Path           string   `json:"path"`
	Output         string   `json:"output,omitempty"`
	Recipe         string   `json:"recipe,omitempty"`
	NoSkip         *bool    `json:"noSkip,omitempty"`
	Exclude        []string `json:"exclude,omitempty"`
	ReferencePaths []string `json:"referencePaths,omitempty"`
	AllowDynamic   *bool    `json:"allowDynamic,omitempty"`
	Cflow          *bool    `json:"cflow,omitempty"`
	CodeOnly       *bool    `json:"codeOnly,omitempty"`
	GameDataOut    string   `json:"gameDataOut,omitempty"`
	ToolsMirror    string   `json:"toolsMirror,omitempty"`
}

// RunSpec is the resolved immutable description. It never contains credentials.
type RunSpec struct {
	Version        int      `json:"version"`
	Path           string   `json:"path"`
	Output         string   `json:"output"`
	Recipe         string   `json:"recipe"`
	NoSkip         bool     `json:"noSkip"`
	Exclude        []string `json:"exclude"`
	ReferencePaths []string `json:"referencePaths"`
	AllowDynamic   bool     `json:"allowDynamic"`
	Cflow          bool     `json:"cflow"`
	CodeOnly       bool     `json:"codeOnly"`
	ExtractAssets  bool     `json:"extractAssets"`
	GameDataOut    string   `json:"gameDataOut"`
	ToolsMirror    string   `json:"toolsMirror"`
}
