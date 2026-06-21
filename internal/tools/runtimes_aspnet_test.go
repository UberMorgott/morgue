package tools

import (
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestAspNetRuntimeWiring(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	dir := m.localRuntimeDir(RuntimeAspNet)
	if filepath.Base(dir) != "dotnet-aspnet10" {
		t.Fatalf("aspnet runtime dir = %q, want .../dotnet-aspnet10", dir)
	}
	if runtimeBinary(RuntimeAspNet) == "" {
		t.Fatalf("runtimeBinary(RuntimeAspNet) empty")
	}
	// aspNetRuntimeURL must point at the .NET 10 aspnetcore win-x64 zip.
	if got := aspNetRuntimeURL(); got != "https://aka.ms/dotnet/10.0/aspnetcore-runtime-win-x64.zip" {
		t.Fatalf("aspnet URL = %q", got)
	}
}
