package recipe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/util"
)

// A cached helper DLL is trusted only when its stamp matches the current
// source/SDK/app digest; bare DLL existence triggers a rebuild.
func TestHelperCacheRequiresMatchingSourceDigest(t *testing.T) {
	ctx := &Context{Ctx: context.Background()}
	const tool = "cfxhelper-cache-test"
	dir := util.ToolDir(tool)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := filepath.Join(dir, "bin", "Release", "net8.0")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "helper.dll"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingSDK := filepath.Join(t.TempDir(), "no-dotnet.exe")
	src := []string{"cfxstrings.csproj", "Program.cs"}
	noop := func(string, string) {}
	if _, _, err := buildHelperCached(ctx, missingSDK, noop, tool, "cfxstrings.csproj", "helper.dll", cfxStringsAssets, "assets/cfxstrings/", src, ""); err == nil {
		t.Fatal("unstamped cached DLL was trusted instead of rebuilt")
	}
	digest, err := helperSourceDigest(ctx, missingSDK, tool, cfxStringsAssets, "assets/cfxstrings/", src)
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest %q: %v", digest, err)
	}
	if err := os.WriteFile(filepath.Join(dir, helperStamp), []byte(digest+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dll, got, err := buildHelperCached(ctx, missingSDK, noop, tool, "cfxstrings.csproj", "helper.dll", cfxStringsAssets, "assets/cfxstrings/", src, "")
	if err != nil || got != digest || dll != filepath.Join(bin, "helper.dll") {
		t.Fatalf("matching stamp not reused: dll=%q digest=%q err=%v", dll, got, err)
	}
	other, _ := helperSourceDigest(ctx, missingSDK+"2", tool, cfxStringsAssets, "assets/cfxstrings/", src)
	cflow, _ := helperSourceDigest(ctx, missingSDK, tool, cfxCflowAssets, "assets/cfxcflow/", []string{"cfxcflow.csproj", "Program.cs"})
	if other == digest || cflow == digest {
		t.Fatal("digest must change with SDK identity and embedded source")
	}
}
