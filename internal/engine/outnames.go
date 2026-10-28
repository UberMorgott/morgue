package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// assignOutputNames picks a distinct per-target output directory name for
// every task, relative to the run's output root.
//
// A target whose stem (file name without extension) is unique in the run keeps
// the short, historical name ("Cosmoteer.dll" -> "Cosmoteer"). Targets whose
// stems collide (Cosmoteer.dll + Cosmoteer.exe, or the same file name in
// several sub-folders such as Mods/*) instead get their path relative to the
// input root, extension included, with separators flattened to "_"
// ("Cosmoteer.exe", "Mods_Foo_Cosmoteer.dll"). Any residual clash gets a
// numeric suffix. Names compare case-insensitively (Windows file system), and
// the result depends only on the task order, so reruns are stable.
func assignOutputNames(tasks []fileTask, inputRoot string) {
	stemCount := map[string]int{}
	for _, t := range tasks {
		stemCount[strings.ToLower(sanitizeName(filepath.Base(t.filePath)))]++
	}

	used := map[string]bool{}
	claim := func(name string) string {
		candidate := name
		for i := 2; used[strings.ToLower(candidate)]; i++ {
			candidate = fmt.Sprintf("%s-%d", name, i)
		}
		used[strings.ToLower(candidate)] = true
		return candidate
	}

	// Unique stems first so they keep their short name even if a flattened
	// relative path happens to spell the same thing.
	for i := range tasks {
		stem := sanitizeName(filepath.Base(tasks[i].filePath))
		if stemCount[strings.ToLower(stem)] == 1 {
			tasks[i].outName = claim(stem)
		}
	}
	for i := range tasks {
		if tasks[i].outName != "" {
			continue
		}
		tasks[i].outName = claim(flatRelName(inputRoot, tasks[i].filePath))
	}
}

// flatRelName returns filePath relative to inputRoot as a single path
// component ("Mods/Foo/Bar.dll" -> "Mods_Foo_Bar.dll"). Falls back to the base
// name when inputRoot is the file itself or does not contain filePath.
func flatRelName(inputRoot, filePath string) string {
	base := filepath.Base(filePath)
	if inputRoot == "" {
		return base
	}
	if info, err := os.Stat(inputRoot); err == nil && !info.IsDir() {
		return base
	}
	rel, err := filepath.Rel(inputRoot, filePath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return base
	}
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", "_")
}
