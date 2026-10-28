package tools

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// pointerFixture is the DotnetToolSettings.xml shipped in ilspycmd 11.2.0.10050,
// a .NET 10 RID pointer package with no binaries of its own.
func pointerFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/ilspycmd-11.2-pointer-DotnetToolSettings.xml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParsePointerTargetPrefersAny(t *testing.T) {
	id, err := parsePointerTarget(pointerFixture(t))
	if err != nil || id != "ilspycmd.any" {
		t.Fatalf("parsePointerTarget = (%q, %v), want (ilspycmd.any, nil)", id, err)
	}

	regular := []byte(`<DotNetCliTool Version="1"><Commands><Command Name="ilspycmd" EntryPoint="ilspycmd.dll" Runner="dotnet" /></Commands></DotNetCliTool>`)
	if id, err := parsePointerTarget(regular); err != nil || id != "" {
		t.Fatalf("regular package: got (%q, %v), want (\"\", nil)", id, err)
	}

	unsupported := []byte(`<DotNetCliTool Version="2"><RuntimeIdentifierPackages><RuntimeIdentifierPackage RuntimeIdentifier="linux-x64" Id="x.linux-x64" /></RuntimeIdentifierPackages></DotNetCliTool>`)
	if _, err := parsePointerTarget(unsupported); err == nil {
		t.Fatal("pointer without a supported RID must error")
	}
}

// TestInstallFromNuGetFollowsPointer: installing a pointer package must fetch the
// "any" RID package of the same version so the entry-point dll exists.
func TestInstallFromNuGetFollowsPointer(t *testing.T) {
	const ver = "11.2.0.10050"
	pointer := makeZip(t, map[string][]byte{
		"ilspycmd.nuspec":                      []byte("<package/>"),
		"tools/any/any/DotnetToolSettings.xml": pointerFixture(t),
	})
	anyPkg := makeZip(t, map[string][]byte{
		"tools/net10.0/any/DotnetToolSettings.xml": []byte(`<DotNetCliTool Version="1"><Commands><Command Name="ilspycmd" EntryPoint="ilspycmd.dll" Runner="dotnet" /></Commands></DotNetCliTool>`),
		"tools/net10.0/any/ilspycmd.dll":           []byte("MZ"),
	})
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/api/v2/package/ilspycmd/" + ver:
			_, _ = w.Write(pointer)
		case "/api/v2/package/ilspycmd.any/" + ver:
			_, _ = w.Write(anyPkg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setAssetMirror(srv.URL)
	t.Cleanup(func() { setAssetMirror("") })

	def := ToolDef{Name: "ilspycmd", Method: MethodNuGet, DotnetID: "ilspycmd", DotnetVersion: ver, Binary: "ilspycmd.dll"}
	dest := t.TempDir()
	got, err := installFromNuGet(t.Context(), def, dest, nil, nil)
	if err != nil {
		t.Fatalf("installFromNuGet: %v (requests %v)", err, requested)
	}
	if got != ver {
		t.Fatalf("version = %q, want %q", got, ver)
	}
	if p := findBinaryRecursive(dest, "ilspycmd.dll"); !strings.HasSuffix(p, `ilspycmd.dll`) {
		t.Fatalf("ilspycmd.dll not extracted into %s", dest)
	}
}

// TestInstallFromNuGetPointerOnlyFails: when the RID package is unavailable the
// install must fail instead of reporting success with no binary (v0.7.8 bug).
func TestInstallFromNuGetPointerOnlyFails(t *testing.T) {
	const ver = "11.2.0.10050"
	pointer := makeZip(t, map[string][]byte{"tools/any/any/DotnetToolSettings.xml": pointerFixture(t)})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/package/ilspycmd/"+ver {
			_, _ = w.Write(pointer)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	setAssetMirror(srv.URL)
	t.Cleanup(func() { setAssetMirror("") })

	def := ToolDef{Name: "ilspycmd", Method: MethodNuGet, DotnetID: "ilspycmd", DotnetVersion: ver, Binary: "ilspycmd.dll"}
	if _, err := installFromNuGet(t.Context(), def, t.TempDir(), nil, nil); err == nil {
		t.Fatal("pointer-only install reported success")
	}
}
