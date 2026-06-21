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

func TestListRuntimesHasAspNet10(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "has 10.x aspnetcore",
			out: "Microsoft.AspNetCore.App 8.0.26 [C:\\Program Files\\dotnet\\shared\\Microsoft.AspNetCore.App]\n" +
				"Microsoft.AspNetCore.App 10.0.9 [E:\\runtimes\\dotnet-aspnet10\\shared\\Microsoft.AspNetCore.App]\n" +
				"Microsoft.NETCore.App 10.0.9 [E:\\runtimes\\dotnet-aspnet10\\shared\\Microsoft.NETCore.App]\n",
			want: true,
		},
		{
			name: "only 8.x present (the real failing machine state)",
			out: "Microsoft.AspNetCore.App 8.0.26 [C:\\Program Files\\dotnet\\shared\\Microsoft.AspNetCore.App]\n" +
				"Microsoft.NETCore.App 8.0.26 [C:\\Program Files\\dotnet\\shared\\Microsoft.NETCore.App]\n",
			want: false,
		},
		{
			name: "netcore 10 but no aspnetcore 10",
			out:  "Microsoft.NETCore.App 10.0.0 [C:\\Program Files\\dotnet\\shared\\Microsoft.NETCore.App]\n",
			want: false,
		},
		{
			name: "empty",
			out:  "",
			want: false,
		},
		{
			name: "10 only as a substring of a path must not false-positive",
			out:  "Microsoft.AspNetCore.App 8.0.10 [C:\\10\\dotnet]\n",
			want: false,
		},
	}
	for _, c := range cases {
		if got := listRuntimesHasAspNet10(c.out); got != c.want {
			t.Fatalf("%s: listRuntimesHasAspNet10 = %v, want %v", c.name, got, c.want)
		}
	}
}
