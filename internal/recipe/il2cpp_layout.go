package recipe

import (
	"os"
	"path/filepath"
)

// il2cppLayout holds the structured output paths for one IL2CPP run.
//
//	<root>/dump/cs   il2cpp.cs (merged C# from the dumper)
//	<root>/dump/dll  DummyDLLs (ilspycmd input)
//	<root>/data      AssetRipper config export + Odin-decoded config
//	<root>/log       per-stage logs
//	<root>/.tmp      spawned-tool TEMP/cwd (kept off C:)
type il2cppLayout struct {
	Root    string // <out>/<game>  (or just <out> when game == "")
	CsDir   string // <root>/dump/cs
	DllDir  string // <root>/dump/dll
	DataDir string // <root>/data
	LogDir  string // <root>/log
	TmpDir  string // <root>/.tmp
}

// newIL2CPPLayout builds the structured layout under outRoot. When game is "",
// the layout roots directly at outRoot (the Execute path already receives a
// per-target output dir, so a second game segment would double-nest).
func newIL2CPPLayout(outRoot, game string) il2cppLayout {
	root := filepath.Join(outRoot, game)
	return il2cppLayout{
		Root:    root,
		CsDir:   filepath.Join(root, "dump", "cs"),
		DllDir:  filepath.Join(root, "dump", "dll"),
		DataDir: filepath.Join(root, "data"),
		LogDir:  filepath.Join(root, "log"),
		TmpDir:  filepath.Join(root, ".tmp"),
	}
}

// mkdirAll creates every directory in the layout.
func (l il2cppLayout) mkdirAll() error {
	for _, d := range []string{l.CsDir, l.DllDir, l.DataDir, l.LogDir, l.TmpDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// stageDone reports whether a stage's primary output marker already exists, so a
// resumed run can skip work it has already produced. force always returns false
// (re-run regardless of the marker).
func stageDone(marker string, force bool) bool {
	if force {
		return false
	}
	_, err := os.Stat(marker)
	return err == nil
}
