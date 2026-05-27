package tools

// Category represents the functional category of a tool.
type Category int

const (
	CategoryDetector    Category = iota
	CategoryDecompiler
	CategoryDeobfuscator
	CategoryUnpacker
	CategoryAnalyzer
	CategoryExtractor
)

var categoryNames = [...]string{
	"Detector",
	"Decompiler",
	"Deobfuscator",
	"Unpacker",
	"Analyzer",
	"Extractor",
}

func (c Category) String() string {
	if int(c) < len(categoryNames) {
		return categoryNames[c]
	}
	return "Unknown"
}

// Method describes how a tool is obtained.
type Method int

const (
	MethodGitHubRelease Method = iota
	MethodDirectURL
	MethodDotnetTool
	MethodGitBuild
	MethodNuGet
)

var methodNames = [...]string{
	"GitHubRelease",
	"DirectURL",
	"DotnetTool",
	"GitBuild",
	"NuGet",
}

func (m Method) String() string {
	if int(m) < len(methodNames) {
		return methodNames[m]
	}
	return "Unknown"
}

// ToolDef defines a tool that morgue depends on.
type ToolDef struct {
	Name        string
	Description string
	Category    Category
	Method      Method
	Repo        string // GitHub owner/repo
	URL          string   // Direct download URL (for MethodDirectURL)
	DownloadURLs []string // Multiple direct download URLs (for MethodDirectURL)
	DotnetID      string // dotnet tool ID (for MethodDotnetTool)
	DotnetVersion string // pinned version for dotnet tool install (optional)
	AssetGlob   string // Glob pattern for matching GitHub release assets
	Binary      string        // Expected executable name after install
	Optional    bool
	RuntimeDeps []RuntimeKind `json:"RuntimeDeps,omitempty"`
}

// ToolStatus holds the installed state of a tool.
type ToolStatus struct {
	Name            string `json:"Name"`
	Installed       bool   `json:"Installed"`
	Path            string `json:"Path"`
	Version         string `json:"Version"`
	LatestVersion   string `json:"LatestVersion"`
	UpdateAvailable bool   `json:"UpdateAvailable"`
	Category        string        `json:"Category"`
	Description     string        `json:"Description"`
	Optional        bool          `json:"Optional"`
	RuntimeDeps     []RuntimeKind `json:"RuntimeDeps,omitempty"`
}
