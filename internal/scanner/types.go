package scanner

// GroupKind represents the classification of a group of files.
type GroupKind int

const (
	GroupStandalone  GroupKind = iota
	GroupUnityMono
	GroupUnityIL2CPP
)

var groupKindNames = [...]string{
	"Standalone",
	"UnityMono",
	"UnityIL2CPP",
}

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
