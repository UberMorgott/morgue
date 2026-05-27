package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// groupFiles classifies discovered files into logical groups.
// Priority: Unity IL2CPP > Unity Mono > Standalone.
func groupFiles(files []string) []TargetGroup {
	var groups []TargetGroup
	claimed := map[string]bool{}

	// Pass 1: find Unity IL2CPP groups
	for _, g := range findUnityIL2CPP(files) {
		groups = append(groups, g)
		for _, f := range g.Files {
			claimed[f] = true
		}
	}

	// Pass 1.5: find Unreal Engine groups
	for _, g := range findUnreal(files) {
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

// findUnreal detects Unreal Engine game layouts.
// Markers: .pak files in */Content/Paks/, .utoc/.ucas IoStore containers.
func findUnreal(files []string) []TargetGroup {
	pakDirs := map[string]bool{}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".pak" || ext == ".utoc" {
			pakDirs[filepath.Dir(f)] = true
		}
	}
	if len(pakDirs) == 0 {
		return nil
	}

	roots := map[string]bool{}
	for dir := range pakDirs {
		root := findUnrealRoot(dir)
		if root != "" {
			roots[root] = true
		}
	}

	var groups []TargetGroup
	for root := range roots {
		var gf []string
		for _, f := range files {
			if isUnder(f, root) {
				gf = append(gf, f)
			}
		}
		if len(gf) > 0 {
			groups = append(groups, TargetGroup{
				Kind:  GroupUnreal,
				Root:  root,
				Files: gf,
			})
		}
	}
	return groups
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
