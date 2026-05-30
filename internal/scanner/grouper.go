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

	// Collapse server/nested duplicate roots (e.g. a packaged WindowsServer build
	// nested under Builds/) so a single game yields ONE Unreal target. Containers
	// from dropped roots are folded into the surviving ancestor root when one
	// exists, so they still count toward the representative pick.
	roots := make([]string, 0, len(rootFiles))
	for root := range rootFiles {
		roots = append(roots, root)
	}
	kept := dedupeUnrealRoots(roots)
	keptSet := map[string]bool{}
	for _, r := range kept {
		keptSet[r] = true
	}
	for _, dropped := range roots {
		if keptSet[dropped] {
			continue
		}
		// Re-home a dropped root's containers onto the deepest kept ancestor, if any.
		// (Server-under-Builds roots usually have no kept ancestor, so their
		// containers are simply discarded — that's the intended dedup.)
		var bestAncestor string
		for _, k := range kept {
			if isAncestorRoot(k, dropped) && len(k) > len(bestAncestor) {
				bestAncestor = k
			}
		}
		if bestAncestor != "" {
			rootFiles[bestAncestor] = append(rootFiles[bestAncestor], rootFiles[dropped]...)
		}
	}

	var groups []TargetGroup
	for _, root := range kept {
		rep := representativePak(rootFiles[root])
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

// dedupeUnrealRoots collapses server/nested duplicate Unreal roots so a single
// game yields ONE target, using conservative path-component rules:
//
//  1. If at least one root has a "Builds" path segment (full component,
//     case-insensitive — e.g. ...\R5\Builds\WindowsServer\...) AND at least one
//     root does NOT, drop every root under a Builds/ segment. A packaged server
//     build is treated as a duplicate of the client game, not a separate game.
//  2. Among the survivors, if one root is an ancestor of another (its path
//     components are a prefix of the other's), keep the shallowest ancestor and
//     drop the descendants.
//
// Anything left after that is treated as a genuinely separate game and KEPT
// (we intentionally do NOT over-collapse unrelated games). Comparison uses
// cleaned, separator-split path components with case-insensitive matching for
// Windows semantics — never naive substring Contains (so a dir literally named
// "Rebuilds" is not mistaken for a "Builds" segment).
func dedupeUnrealRoots(roots []string) []string {
	if len(roots) <= 1 {
		return roots
	}

	hasBuilds := false
	hasNonBuilds := false
	for _, r := range roots {
		if hasBuildsSegment(r) {
			hasBuilds = true
		} else {
			hasNonBuilds = true
		}
	}

	// Rule 1: drop Builds/-nested roots only when a non-Builds root also exists.
	var afterBuilds []string
	if hasBuilds && hasNonBuilds {
		for _, r := range roots {
			if !hasBuildsSegment(r) {
				afterBuilds = append(afterBuilds, r)
			}
		}
	} else {
		afterBuilds = append(afterBuilds, roots...)
	}

	// Rule 2: drop descendants when an ancestor root is also present.
	var kept []string
	for _, r := range afterBuilds {
		isDescendant := false
		for _, other := range afterBuilds {
			if other == r {
				continue
			}
			if isAncestorRoot(other, r) {
				isDescendant = true
				break
			}
		}
		if !isDescendant {
			kept = append(kept, r)
		}
	}
	return kept
}

// pathComponents returns the cleaned, separator-split components of a path
// (drive/volume name included as the first component on Windows).
func pathComponents(p string) []string {
	cleaned := filepath.Clean(p)
	vol := filepath.VolumeName(cleaned)
	rest := cleaned[len(vol):]
	rest = strings.Trim(rest, `\/`)
	var parts []string
	if vol != "" {
		parts = append(parts, vol)
	}
	if rest != "" {
		parts = append(parts, strings.FieldsFunc(rest, func(r rune) bool {
			return r == '\\' || r == '/'
		})...)
	}
	return parts
}

// hasBuildsSegment reports whether any full path component equals "Builds"
// (case-insensitive). Component-based so "Rebuilds" or "BuildsX" never match.
func hasBuildsSegment(p string) bool {
	for _, c := range pathComponents(p) {
		if strings.EqualFold(c, "Builds") {
			return true
		}
	}
	return false
}

// isAncestorRoot reports whether ancestor is a strict path-component prefix of
// descendant (ancestor != descendant). Case-insensitive per Windows semantics.
func isAncestorRoot(ancestor, descendant string) bool {
	a := pathComponents(ancestor)
	d := pathComponents(descendant)
	if len(a) >= len(d) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], d[i]) {
			return false
		}
	}
	return true
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
