package scanner

// GroupKind represents the classification of a group of files.
type GroupKind int

const (
	// GroupStandalone is a single binary with no related companion files.
	GroupStandalone GroupKind = iota
	// GroupDotNetApp is a .NET application and its managed assemblies.
	GroupDotNetApp
	// GroupDelphiApp is a Delphi-compiled application.
	GroupDelphiApp
	// GroupUnityMono is a Unity game using the Mono scripting backend.
	GroupUnityMono
	// GroupUnityIL2CPP is a Unity game using the IL2CPP scripting backend.
	GroupUnityIL2CPP
	// GroupUnreal is an Unreal Engine application.
	GroupUnreal
)

var groupKindNames = [...]string{
	"Standalone",
	"DotNetApp",
	"DelphiApp",
	"UnityMono",
	"UnityIL2CPP",
	"Unreal",
}

// String returns the human-readable name of the GroupKind.
func (g GroupKind) String() string {
	if int(g) < len(groupKindNames) {
		return groupKindNames[g]
	}
	return "Unknown"
}

// TargetGroup is a logical group of related binaries.
type TargetGroup struct {
	Kind  GroupKind
	Root  string   // root directory of the group
	Files []string // absolute paths of files in the group
}

// ScanResult holds the output of a directory scan.
type ScanResult struct {
	Files   []string      // all discovered binary files
	Groups  []TargetGroup // logically grouped targets
	Skipped []SkippedFile
}

// SkippedFile records a file that was skipped and why.
type SkippedFile struct {
	Path   string
	Reason string
}
