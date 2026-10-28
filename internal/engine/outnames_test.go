package engine

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAssignOutputNames(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "Cosmoteer.dll"),
		filepath.Join(root, "Cosmoteer.exe"),
		filepath.Join(root, "Mods", "A", "Plugin.dll"),
		filepath.Join(root, "Mods", "B", "Plugin.dll"),
		filepath.Join(root, "Unique.dll"),
		filepath.Join(root, "Sub", "unique.so"), // case-insensitive stem clash with Unique.dll
	}
	tasks := make([]fileTask, len(paths))
	for i, p := range paths {
		tasks[i] = fileTask{filePath: p}
	}

	assignOutputNames(tasks, root)

	want := []string{
		"Cosmoteer.dll",
		"Cosmoteer.exe",
		"Mods_A_Plugin.dll",
		"Mods_B_Plugin.dll",
		"Unique.dll",
		"Sub_unique.so",
	}
	seen := map[string]bool{}
	for i, task := range tasks {
		if task.outName != want[i] {
			t.Errorf("outName(%s) = %q, want %q", paths[i], task.outName, want[i])
		}
		key := strings.ToLower(task.outName)
		if seen[key] {
			t.Errorf("duplicate output dir %q", task.outName)
		}
		seen[key] = true
	}

	// Stable: a rerun over the same targets yields the same names.
	again := make([]fileTask, len(paths))
	for i, p := range paths {
		again[i] = fileTask{filePath: p}
	}
	assignOutputNames(again, root)
	for i := range again {
		if again[i].outName != tasks[i].outName {
			t.Errorf("rerun name %q != %q", again[i].outName, tasks[i].outName)
		}
	}
}

func TestAssignOutputNamesUniqueStemKeepsShortName(t *testing.T) {
	root := t.TempDir()
	tasks := []fileTask{
		{filePath: filepath.Join(root, "Game.exe")},
		{filePath: filepath.Join(root, "Plugins", "Helper.dll")},
	}
	assignOutputNames(tasks, root)
	if tasks[0].outName != "Game" || tasks[1].outName != "Helper" {
		t.Errorf("names = %q, %q; want Game, Helper", tasks[0].outName, tasks[1].outName)
	}
}

func TestAssignOutputNamesResidualClashGetsSuffix(t *testing.T) {
	root := t.TempDir()
	tasks := []fileTask{
		{filePath: filepath.Join(root, "a_b", "x.dll")},
		{filePath: filepath.Join(root, "a", "b_x.dll")},
		{filePath: filepath.Join(root, "a", "b", "x.dll")},
	}
	assignOutputNames(tasks, root)
	seen := map[string]bool{}
	for _, task := range tasks {
		if seen[strings.ToLower(task.outName)] {
			t.Errorf("duplicate output dir %q", task.outName)
		}
		seen[strings.ToLower(task.outName)] = true
	}
	if tasks[2].outName != "a_b_x.dll-2" {
		t.Errorf("residual clash = %q, want a_b_x.dll-2", tasks[2].outName)
	}
}

func TestAssignOutputNamesFileInput(t *testing.T) {
	p := filepath.Join(t.TempDir(), "App.exe")
	tasks := []fileTask{{filePath: p}}
	assignOutputNames(tasks, p)
	if tasks[0].outName != "App" {
		t.Errorf("outName = %q, want App", tasks[0].outName)
	}
}
