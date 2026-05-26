package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestFetchLatestVersion(t *testing.T) {
	// Test redirect-based version detection (no API)
	repos := []string{
		"horsicq/DIE-engine",
		"icsharpcode/ILSpy",
		"NationalSecurityAgency/ghidra",
	}
	for _, repo := range repos {
		ver, err := fetchLatestVersion(repo)
		if err != nil {
			t.Errorf("fetchLatestVersion(%s): %v", repo, err)
			continue
		}
		if ver == "" {
			t.Errorf("fetchLatestVersion(%s): empty version", repo)
			continue
		}
		t.Logf("%s → %s", repo, ver)
	}
}

func TestInstallStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping download test in short mode")
	}

	baseDir := filepath.Join(os.TempDir(), "morgue-test-install")
	os.MkdirAll(baseDir, 0755)
	defer os.RemoveAll(baseDir)

	mgr := NewManager(baseDir, config.Config{})
	mgr.OnProgress = func(tool string, down, total int64) {
		if total > 0 {
			fmt.Printf("\r  %s download: %d/%d (%d%%)", tool, down, total, down*100/total)
		}
	}

	ver, err := mgr.Install("strings")
	fmt.Println()
	if err != nil {
		t.Fatalf("Install strings: %v", err)
	}
	t.Logf("Installed strings version=%q", ver)

	// Verify it's detected as installed
	st := mgr.Check("strings")
	if !st.Installed {
		t.Error("strings should be installed after Install()")
	}
	if st.Version == "" {
		t.Error("strings should have a version after Install()")
	}
	if st.Path == "" {
		t.Error("strings should have a path after Install()")
	}
	t.Logf("Check: installed=%v version=%q path=%q", st.Installed, st.Version, st.Path)
}

func TestCheckAllWithUpdatesNoAPI(t *testing.T) {
	baseDir := filepath.Join(os.TempDir(), "morgue-test-check")
	os.MkdirAll(baseDir, 0755)
	defer os.RemoveAll(baseDir)

	mgr := NewManager(baseDir, config.Config{})
	statuses := mgr.CheckAllWithUpdates()

	if len(statuses) == 0 {
		t.Fatal("CheckAllWithUpdates returned empty")
	}

	for _, st := range statuses {
		t.Logf("%s: installed=%v version=%q latest=%q update=%v",
			st.Name, st.Installed, st.Version, st.LatestVersion, st.UpdateAvailable)
	}
}

func TestCheckRuntimes(t *testing.T) {
	baseDir := filepath.Join(os.TempDir(), "morgue-test-rt")
	os.MkdirAll(baseDir, 0755)
	defer os.RemoveAll(baseDir)

	mgr := NewManager(baseDir, config.Config{})
	rts := mgr.CheckRuntimes()

	for _, rt := range rts {
		t.Logf("%s: available=%v version=%q path=%q local=%v",
			rt.Kind, rt.Available, rt.Version, rt.Path, rt.Local)
	}
}
