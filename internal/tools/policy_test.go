package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/util"
)

func seedPolicyTool(t *testing.T, base, name, version string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.exe"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".version"), []byte(version), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunSelectionFreshnessOnceAndLease(t *testing.T) {
	var heads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			heads.Add(1)
		}
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2023 15:04:05 GMT")
		_, _ = w.Write([]byte("new"))
	}))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "policy", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "policy", "2022.01.02")
	s, err := m.NewRunSelection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ids, err := s.Ensure(t.Context(), []string{"policy", "policy"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].Version != "2023.01.02" || len(ids[0].PayloadSHA256) != 64 {
		t.Fatalf("identities: %+v", ids)
	}
	if _, err := s.Bind().Resolve("policy"); err != nil {
		t.Fatal(err)
	}
	if heads.Load() != 1 {
		t.Fatalf("freshness probes=%d, want 1", heads.Load())
	}
	if err := m.Delete("policy"); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete leased: %v", err)
	}
	if _, err := m.Install(t.Context(), "policy", nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("update leased: %v", err)
	}
	if !m.Check("policy").InUse {
		t.Fatal("active lease not visible")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("policy"); err != nil {
		t.Fatal(err)
	}
}

func TestRunSelectionOfflineRetainsWorkingAndWarns(t *testing.T) {
	for _, code := range []int{404, 429} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
			defer srv.Close()
			onlyTestTool(t, ToolDef{Name: "offline", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"})
			m := NewManager(t.TempDir(), config.Config{})
			seedPolicyTool(t, m.baseDir, "offline", "1")
			s, _ := m.NewRunSelection(t.Context())
			defer s.Close()
			var warning string
			ids, err := s.Ensure(t.Context(), []string{"offline"}, &InstallCallbacks{OnStatus: func(st ToolPolicyStatus) {
				if st.Freshness == "warning" {
					warning = st.Warning
				}
			}})
			if err != nil || len(ids) != 1 || ids[0].Version != "1" || warning == "" {
				t.Fatalf("ids=%+v err=%v warning=%q", ids, err, warning)
			}
		})
	}
}

func TestInstalledLockMigratesAndPreservesOperatorVersion(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, lockFile), []byte(`{"tools":{"old":"v1"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(base, config.Config{})
	if err := m.RecordInstall("new", "v2"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(base, lockFile))
	var lk InstalledLock
	if err := json.Unmarshal(data, &lk); err != nil {
		t.Fatal(err)
	}
	if lk.SchemaVersion != 2 || lk.Tools["old"].Version != "v1" || lk.Tools["new"].Version != "v2" {
		t.Fatalf("migrated lock: %s", data)
	}
}

func TestSelectionCancelledWhileWaitingForExclusive(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "wait", Method: MethodDirectURL, Version: "1", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "wait", "1")
	l, err := acquireToolLock(t.Context(), m.baseDir, "wait", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := s.Ensure(ctx, []string{"wait"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait cancellation: %v", err)
	}
}

func TestExplicitVersionUnsupportedDirectURLDoesNotMutate(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "direct", Method: MethodDirectURL, URL: "http://127.0.0.1:1/tool.exe", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "direct", "old")
	if _, err := m.InstallVersion(t.Context(), "direct", InstallOptions{Version: "X"}, nil); err == nil {
		t.Fatal("unsupported version accepted")
	}
	data, _ := os.ReadFile(filepath.Join(m.baseDir, "direct", "tool.exe"))
	if string(data) != "old" {
		t.Fatal("working install changed")
	}
}

func TestInstallLockFailureRollsBackWorkingPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("new")) }))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "transaction", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe", Version: "2"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "transaction", "1")
	if err := os.Mkdir(filepath.Join(m.baseDir, lockFile), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InstallVersion(t.Context(), "transaction", InstallOptions{Version: "2"}, nil); err == nil {
		t.Fatal("lock recording failure ignored")
	}
	data, _ := os.ReadFile(filepath.Join(m.baseDir, "transaction", "tool.exe"))
	if string(data) != "old" {
		t.Fatalf("working install lost: %q", data)
	}
	if m.Check("transaction").Version != "1" {
		t.Fatal("failed activation changed version")
	}
}

func TestPayloadInventoryIncludesPluginsExcludesMutableCache(t *testing.T) {
	dir := t.TempDir()
	seedPolicyTool(t, dir, "payload", "1")
	root := filepath.Join(dir, "payload")
	first, err := inventoryHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "cache", "mutable"), []byte("cache"), 0644)
	second, _ := inventoryHash(t.Context(), root)
	if first != second {
		t.Fatal("cache changed identity")
	}
	_ = os.WriteFile(filepath.Join(root, "plugin.dll"), []byte("plugin"), 0644)
	third, _ := inventoryHash(t.Context(), root)
	if third == second {
		t.Fatal("plugin omitted from identity")
	}
}

func TestBoundSystemToolMutationIsDetected(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("PATH exe fixture is Windows-specific")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "tool.exe")
	_ = os.WriteFile(bin, []byte("one"), 0755)
	t.Setenv("PATH", dir)
	onlyTestTool(t, ToolDef{Name: "external", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{PreferSystemTools: true})
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	if _, err := s.Bind().Resolve("external"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(bin, []byte("two"), 0755)
	if _, err := s.Bind().Resolve("external"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("external mutation: %v", err)
	}
}

func TestFreshnessTimeoutRetainsInstalledWithoutSuppressingCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "timeout", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "timeout", "1")
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Millisecond)
	defer cancel()
	if _, err := s.Ensure(ctx, []string{"timeout"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled probe: %v", err)
	}
	if !m.Check("timeout").Installed {
		t.Fatal("cancelled probe destroyed working tool")
	}
}

// rewriteTestTransport routes every upstream to one local server; no live API.
type rewriteTestTransport struct {
	base   http.RoundTripper
	server string
}

func (rt rewriteTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u := *req.URL
	clone.URL = &u
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(rt.server, "http://")
	return rt.base.RoundTrip(clone)
}

func TestOperatorVersionRoundtripGitHubAndNuGet(t *testing.T) {
	for _, method := range []Method{MethodGitHubRelease, MethodNuGet} {
		t.Run(method.String(), func(t *testing.T) {
			var probes atomic.Int32
			var failCheck atomic.Bool
			var server string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failCheck.Load() && (r.URL.Path == "/owner/repo/releases/latest" || r.URL.Path == "/v3-flatcontainer/package/index.json") {
					probes.Add(1)
					w.WriteHeader(503)
					return
				}
				switch {
				case r.URL.Path == "/owner/repo/releases/latest":
					probes.Add(1)
					w.Header().Set("Location", "https://github.com/owner/repo/releases/tag/v2")
					w.WriteHeader(302)
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/"):
					ver := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/")
					_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": ver, "assets": []any{map[string]any{"name": "tool.exe", "browser_download_url": server + "/asset/" + ver + "/tool.exe"}}})
				case strings.HasPrefix(r.URL.Path, "/asset/"):
					_, _ = w.Write([]byte(r.URL.Path))
				case r.URL.Path == "/v3-flatcontainer/package/index.json":
					probes.Add(1)
					_, _ = w.Write([]byte(`{"versions":["1","2"]}`))
				case strings.HasPrefix(r.URL.Path, "/api/v2/package/package/"):
					_, _ = w.Write(makeZip(t, map[string][]byte{"tool.exe": []byte(r.URL.Path)}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			server = srv.URL
			original := http.DefaultTransport
			http.DefaultTransport = rewriteTestTransport{base: original, server: server}
			defer func() { http.DefaultTransport = original }()
			onlyTestTool(t, ToolDef{Name: "versioned", Method: method, Repo: "owner/repo", DotnetID: "package", AssetGlob: "tool.exe", Binary: "tool.exe"})
			m := NewManager(t.TempDir(), config.Config{ToolsMirror: srv.URL})
			pin := "1"
			latest := "2"
			if method == MethodGitHubRelease {
				pin = "v1"
				latest = "v2"
			}
			id, err := m.InstallVersion(t.Context(), "versioned", InstallOptions{Version: pin}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if id.Version != pin {
				t.Fatalf("wrong exact version: %+v", id)
			}
			s, _ := m.NewRunSelection(t.Context())
			ids, err := s.Ensure(t.Context(), []string{"versioned"}, nil)
			if err != nil || len(ids) != 1 || ids[0].Version != pin || probes.Load() != 0 {
				t.Fatalf("pin did not survive run: %+v %v probes=%d", ids, err, probes.Load())
			}
			_ = s.Close()
			failCheck.Store(true)
			if _, err := m.InstallVersion(t.Context(), "versioned", InstallOptions{}, nil); err == nil {
				t.Fatal("failed unversioned install succeeded")
			}
			preserved, err := ReadInstalledLock(m.baseDir)
			if err != nil || preserved.OperatorVersions["versioned"] != pin || m.Check("versioned").Version != pin {
				t.Fatalf("failed install lost pin/payload: %+v %v", preserved, err)
			}
			failCheck.Store(false)
			id, err = m.InstallVersion(t.Context(), "versioned", InstallOptions{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			lk, err := ReadInstalledLock(m.baseDir)
			if err != nil {
				t.Fatal(err)
			}
			if id.Version != latest || lk.OperatorVersions["versioned"] != "" || probes.Load() != 2 {
				t.Fatalf("automatic updates not restored: %+v %+v probes=%d", id, lk, probes.Load())
			}
		})
	}
}

func TestTwoRunSelectionsSharePayloadWithoutWaitingForCompletion(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "shared", Method: MethodDirectURL, Version: "1", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "shared", "1")
	a, _ := m.NewRunSelection(t.Context())
	defer a.Close()
	b, _ := m.NewRunSelection(t.Context())
	defer b.Close()
	if _, err := a.Ensure(t.Context(), []string{"shared"}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := b.Ensure(ctx, []string{"shared"}, nil); err != nil {
		t.Fatalf("second run could not share frozen payload: %v", err)
	}
	_ = a.Close()
	if err := m.Delete("shared"); !errors.Is(err, ErrInUse) {
		t.Fatalf("second lease lost: %v", err)
	}
}

func TestRunToolsLockSealsAndSortsIdentities(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "z", Method: MethodDirectURL, Version: "1", Binary: "tool.exe"})
	Registry = append(Registry, ToolDef{Name: "a", Method: MethodDirectURL, Version: "1", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "a", "1")
	seedPolicyTool(t, m.baseDir, "z", "1")
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	if _, err := s.Ensure(t.Context(), []string{"z", "a"}, nil); err != nil {
		t.Fatal(err)
	}
	lk := s.Snapshot(true)
	if !lk.Complete || len(lk.Tools) != 2 || lk.Tools[0].Name != "a" {
		t.Fatalf("lock: %+v", lk)
	}
	lk.Tools[0].Version = "mutated"
	if s.Snapshot(false).Tools[0].Version != "1" {
		t.Fatal("snapshot aliases live state")
	}
	if _, err := s.Ensure(t.Context(), []string{"new"}, nil); err == nil {
		t.Fatal("sealed selection admitted another tool")
	}
}

func TestRuntimeLeaseAndFailedRuntimeUpdateRetainsPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer srv.Close()
	m := NewManager(t.TempDir(), config.Config{ToolsMirror: srv.URL})
	dir := m.localRuntimeDir(RuntimeAspNet)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	bin := m.localRuntimeBin(RuntimeAspNet)
	_ = os.WriteFile(bin, []byte("old-runtime"), 0755)
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	if _, err := s.Bind().RuntimePath(RuntimeAspNet); err != nil {
		t.Fatal(err)
	}
	if err := m.InstallRuntime(RuntimeAspNet, nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("runtime mutation bypassed lease: %v", err)
	}
	_ = s.Close()
	if err := m.InstallRuntime(RuntimeAspNet, nil); err == nil {
		t.Fatal("runtime missing download accepted")
	}
	data, _ := os.ReadFile(bin)
	if string(data) != "old-runtime" {
		t.Fatalf("working runtime lost: %q", data)
	}
}

func TestKnownHelperRequiresExistingPayloadAndSDKIdentity(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	dir := m.localRuntimeDir(RuntimeDotnet)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(m.localRuntimeBin(RuntimeDotnet), []byte("fake-sdk"), 0755)
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	bound := s.Bind()
	if _, err := bound.RuntimePath(RuntimeDotnet); err != nil {
		t.Fatal(err)
	}
	helperDir := filepath.Join(m.baseDir, "embedded", "cfxextract")
	if err := os.MkdirAll(helperDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(helperDir, "unrelated"), []byte("file"), 0644)
	if _, err := bound.RecordHelper(t.Context(), "cfxextract", filepath.Join(helperDir, "missing.dll"), strings.Repeat("a", 64)); err == nil {
		t.Fatal("nonexistent helper payload recorded")
	}
}

func TestWorkingOfflineTimeoutUsesFrozenPayload(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = errorTransport{}
	defer func() { http.DefaultTransport = original }()
	onlyTestTool(t, ToolDef{Name: "net-timeout", Method: MethodDirectURL, URL: "https://example.invalid/tool.exe", Binary: "tool.exe"})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "net-timeout", "1")
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	var warning string
	ids, err := s.Ensure(t.Context(), []string{"net-timeout"}, &InstallCallbacks{OnStatus: func(st ToolPolicyStatus) { warning = st.Warning }})
	if err != nil || len(ids) != 1 || warning == "" {
		t.Fatalf("timeout fallback ids=%+v err=%v warning=%s", ids, err, warning)
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestMissingOptionalToolIsProbedOnlyOncePerRun(t *testing.T) {
	var probes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { probes.Add(1); http.NotFound(w, r) }))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "missing", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe", Optional: true})
	m := NewManager(t.TempDir(), config.Config{})
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	for range 2 {
		if _, err := s.Ensure(t.Context(), []string{"missing"}, nil); err == nil {
			t.Fatal("missing optional tool reported installed")
		}
	}
	if probes.Load() != 1 {
		t.Fatalf("failed optional tool probed %d times", probes.Load())
	}
}

func TestPreseededHeadlessGhidraSurvivesBoundSelection(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "ghidra", Binary: "ghidraRun.bat", Method: MethodGitHubRelease, RuntimeDeps: []RuntimeKind{RuntimeJava}})
	base := t.TempDir()
	home := filepath.Join(base, "external-ghidra")
	if err := os.MkdirAll(filepath.Join(home, "support"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(home, "support", "analyzeHeadless.bat"), []byte("headless"), 0644)
	m := NewManager(filepath.Join(base, "tools"), config.Config{GhidraHome: home})
	if err := os.MkdirAll(filepath.Dir(m.localRuntimeBin(RuntimeJava)), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(m.localRuntimeBin(RuntimeJava), []byte("fake-java"), 0755)
	_ = os.WriteFile(filepath.Join(filepath.Dir(m.localRuntimeBin(RuntimeJava)), javacFile()), []byte("fake-javac"), 0755)
	stubJavaVersion(t, "25.0.1")
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	path, err := s.Bind().Resolve("ghidra")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != home {
		t.Fatalf("lost configured Ghidra home: %s", path)
	}
}

func TestPayloadInventoryRejectsJunctionEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction fixture")
	}
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	outside := filepath.Join(root, "outside")
	_ = os.Mkdir(payload, 0755)
	_ = os.Mkdir(outside, 0755)
	_ = os.WriteFile(filepath.Join(payload, "tool.exe"), []byte("tool"), 0644)
	_ = os.WriteFile(filepath.Join(outside, "plugin.dll"), []byte("external"), 0644)
	alias := filepath.Join(payload, "plugins")
	if _, err := util.RunCmd(t.Context(), "cmd.exe", []string{"/d", "/c", "mklink", "/J", alias, outside}, ""); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(alias)
	if _, err := inventoryHash(t.Context(), payload); err == nil {
		t.Fatal("junction payload escape was silently excluded from hash")
	}
}

func TestHelperBuildPanicReleasesPublicationLock(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("build did not panic")
			}
		}()
		_, _ = s.Bind().WithHelper(t.Context(), "cfxextract", func() (string, string, error) { panic("build failed") })
	}()
	lease, err := acquireToolLock(t.Context(), m.baseDir, "helper:cfxextract", true, false)
	if err != nil {
		t.Fatalf("panicking helper leaked publication lock: %v", err)
	}
	_ = lease.Close()
}

func TestHelperPublicationDoesNotWaitWhileHoldingOtherLeases(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	if err := os.MkdirAll(m.localRuntimeDir(RuntimeDotnet), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(m.localRuntimeBin(RuntimeDotnet), []byte("sdk"), 0755)
	a, _ := m.NewRunSelection(t.Context())
	defer a.Close()
	b, _ := m.NewRunSelection(t.Context())
	defer b.Close()
	for _, s := range []*RunSelection{a, b} {
		if _, err := s.Bind().RuntimePath(RuntimeDotnet); err != nil {
			t.Fatal(err)
		}
	}
	build := func(name string) func() (string, string, error) {
		return func() (string, string, error) {
			dir := filepath.Join(m.baseDir, "embedded", name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", "", err
			}
			path := filepath.Join(dir, name+".dll")
			err := os.WriteFile(path, []byte(name), 0644)
			return path, strings.Repeat("a", 64), err
		}
	}
	if _, err := a.Bind().WithHelper(t.Context(), "cfxstrings", build("cfxstrings")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Bind().WithHelper(t.Context(), "cfxcflow", build("cfxcflow")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.Bind().WithHelper(ctx, "cfxcflow", build("cfxcflow")); !errors.Is(err, ErrInUse) {
		t.Fatalf("publication waited instead of rejecting a lease cycle: %v", err)
	}
}

func TestBoundRuntimeInstallStopsWhenSelectionIsCancelled(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	if err := os.MkdirAll(m.localRuntimeDir(RuntimeAspNet), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(m.localRuntimeBin(RuntimeAspNet), []byte("runtime"), 0755)
	s, _ := m.NewRunSelection(t.Context())
	defer s.Close()
	lease, err := acquireToolLock(t.Context(), m.baseDir, "runtime:"+string(RuntimeAspNet), true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	result := make(chan error, 1)
	go func() { result <- s.Bind().InstallRuntimeContext(context.Background(), RuntimeAspNet, nil) }()
	s.cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runtime admission ignored selection cancellation: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runtime acquisition outlived selection cancellation")
	}
}

func TestFailedStagedHashPreservesWorkingPayloadAndPin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("new")) }))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "hash-failure", Method: MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe", Version: "2", SHA256: strings.Repeat("0", 64)})
	m := NewManager(t.TempDir(), config.Config{})
	seedPolicyTool(t, m.baseDir, "hash-failure", "1")
	pin := "1"
	if err := m.recordIdentity(t.Context(), ToolIdentity{Name: "hash-failure", Version: "1", Source: "direct", Managed: true, HashScope: "not-collected"}, &pin); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(m.baseDir, lockFile))
	if _, err := m.InstallVersion(t.Context(), "hash-failure", InstallOptions{Version: "2"}, nil); err == nil {
		t.Fatal("bad hash accepted")
	}
	after, _ := os.ReadFile(filepath.Join(m.baseDir, lockFile))
	if string(after) != string(before) {
		t.Fatal("failed hash changed installed lock/pin")
	}
	data, _ := os.ReadFile(filepath.Join(m.baseDir, "hash-failure", "tool.exe"))
	if string(data) != "old" {
		t.Fatal("failed hash destroyed working install")
	}
}
