package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// groupFiles classifies discovered files into logical groups.
// Priority: Unreal > Unity IL2CPP > Unity Mono > Standalone.
// pakFiles are Unreal pak/IoStore containers discovered separately; they never
// enter the standalone fallback and collapse into a single Unreal target.
func groupFiles(files []string, pakFiles []string) []TargetGroup {
	var groups []TargetGroup
	claimed := map[string]bool{}

	// Pass 0: Unreal Engine. Collapse all pak/IoStore containers into a single
	// representative target so the ue5 recipe runs exactly once for the whole
	// pak set (it walks up from the target to the game root and finds all paks).
	for _, g := range findUnreal(pakFiles) {
		groups = append(groups, g)
	}

	// Pass 1: find Unity IL2CPP groups
	for _, g := range findUnityIL2CPP(files) {
		groups = append(groups, g)
		for _, f := range g.Files {
			claimed[f] = true
		}
	}

	// Pass 2: find Unity Mono groups
	for _, g := range findUnityMono(files) {
		// Skip files already claimed by IL2CPP
		var unclaimed []string
		for _, f := range g.Files {
			if !claimed[f] {
				unclaimed = append(unclaimed, f)
				claimed[f] = true
			}
		}
		if len(unclaimed) > 0 {
			g.Files = unclaimed
			groups = append(groups, g)
		}
	}

	// Pass 3: remaining files are standalone
	var standalone []string
	for _, f := range files {
		if !claimed[f] {
			standalone = append(standalone, f)
		}
	}
	if len(standalone) > 0 {
		groups = append(groups, TargetGroup{
			Kind:  GroupStandalone,
			Root:  commonDir(standalone),
			Files: standalone,
		})
	}

	return groups
}

// findUnityIL2CPP detects IL2CPP layouts: GameAssembly.dll + global-metadata.dat
func findUnityIL2CPP(files []string) []TargetGroup {
	var groups []TargetGroup
	gameAssemblies := map[string]string{} // dir -> path

	for _, f := range files {
		if strings.EqualFold(filepath.Base(f), "GameAssembly.dll") {
			gameAssemblies[filepath.Dir(f)] = f
		}
	}

	for dir, ga := range gameAssemblies {
		var groupFiles []string
		groupFiles = append(groupFiles, ga)

		// Look for global-metadata.dat under this directory
		for _, f := range files {
			if strings.EqualFold(filepath.Base(f), "global-metadata.dat") && isUnder(f, dir) {
				groupFiles = append(groupFiles, f)
			}
		}

		if len(groupFiles) > 1 { // need at least GameAssembly + metadata
			groups = append(groups, TargetGroup{
				Kind:  GroupUnityIL2CPP,
				Root:  dir,
				Files: groupFiles,
			})
		}
	}
	return groups
}

// findUnityMono detects Unity Mono layouts: *_Data/Managed/*.dll
func findUnityMono(files []string) []TargetGroup {
	monoRoots := map[string][]string{} // managed dir -> files

	for _, f := range files {
		dir := filepath.Dir(f)
		base := filepath.Base(dir)
		if strings.EqualFold(base, "Managed") {
			parent := filepath.Dir(dir)
			parentBase := filepath.Base(parent)
			if strings.HasSuffix(parentBase, "_Data") {
				monoRoots[dir] = append(monoRoots[dir], f)
			}
		}
	}

	var groups []TargetGroup
	for dir, managed := range monoRoots {
		root := filepath.Dir(filepath.Dir(dir)) // up from Managed, then up from *_Data
		groups = append(groups, TargetGroup{
			Kind:  GroupUnityMono,
			Root:  root,
			Files: managed,
		})
	}
	return groups
}

// isUnder checks if path is under the given directory.
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// findUnreal detects Unreal Engine game layouts from pak/IoStore containers.
// Markers: .pak files in */Content/Paks/, .utoc/.ucas IoStore containers.
//
// It collapses each detected pak set into a SINGLE representative target so the
// ue5 recipe runs exactly once per game/mod (the recipe walks up from the
// target to the game root and re-discovers every pak). The representative is
// the largest pak file under the resolved root; if a game-root structure
// (Content/ + Binaries|Engine) is found the representative still resolves up to
// it via ue5's own walk. For a bare mod/paks folder the root is the paks dir
// itself, so the paks are extracted in place.
func findUnreal(pakFiles []string) []TargetGroup {
	if len(pakFiles) == 0 {
		return nil
	}

	pakDirs := map[string]bool{}
	for _, f := range pakFiles {
		ext := strings.ToLower(filepath.Ext(f))
		if unrealExtensions[ext] {
			pakDirs[filepath.Dir(f)] = true
		}
	}
	if len(pakDirs) == 0 {
		return nil
	}

	// Resolve each pak dir to a game/mod root, then bucket the containers by root.
	rootFiles := map[string][]string{}
	for _, f := range pakFiles {
		root := findUnrealRoot(filepath.Dir(f))
		if root == "" {
			root = filepath.Dir(f)
		}
		rootFiles[root] = append(rootFiles[root], f)
	}

	var groups []TargetGroup
	for root, fs := range rootFiles {
		rep := representativePak(fs)
		if rep == "" {
			continue
		}
		groups = append(groups, TargetGroup{
			Kind:  GroupUnreal,
			Root:  root,
			Files: []string{rep}, // single target -> ue5 runs once
		})
	}
	return groups
}

// representativePak picks one file to stand in for the whole pak set.
// Prefers the largest .pak (the main container) so ue5 starts from a real pak;
// falls back to the largest container of any pak/IoStore extension.
func representativePak(files []string) string {
	var bestPak, bestAny string
	var bestPakSize, bestAnySize int64 = -1, -1
	for _, f := range files {
		var size int64
		if info, err := os.Stat(f); err == nil {
			size = info.Size()
		}
		if size > bestAnySize {
			bestAnySize = size
			bestAny = f
		}
		if strings.ToLower(filepath.Ext(f)) == ".pak" && size > bestPakSize {
			bestPakSize = size
			bestPak = f
		}
	}
	if bestPak != "" {
		return bestPak
	}
	return bestAny
}

// findUnrealRoot walks up from a directory containing .pak/.utoc files
// to find the game root (parent with Content/ subfolder).
func findUnrealRoot(pakDir string) string {
	dir := pakDir
	for i := 0; i < 5; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			dir = parent
			continue
		}
		hasContent := false
		hasBinaries := false
		for _, e := range entries {
			lower := strings.ToLower(e.Name())
			if lower == "content" && e.IsDir() {
				hasContent = true
			}
			if (lower == "binaries" || lower == "engine") && e.IsDir() {
				hasBinaries = true
			}
		}
		if hasContent && hasBinaries {
			return parent
		}
		if hasContent {
			return parent
		}
		dir = parent
	}
	gp := filepath.Dir(filepath.Dir(pakDir))
	if gp != pakDir {
		return gp
	}
	return pakDir
}

// commonDir returns the common directory prefix of a set of paths.
func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	common := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		for !isUnder(p, common) {
			parent := filepath.Dir(common)
			if parent == common {
				return common
			}
			common = parent
		}
	}
	return common
}
