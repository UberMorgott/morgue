package recipe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// AssetRipperClient drives a headless AssetRipper Free instance over HTTP.
type AssetRipperClient struct {
	BaseURL string
	HTTP    *http.Client
}

// post sends a form-urlencoded POST and returns an error on any non-2xx,
// capturing the response body for diagnostics.
func (c *AssetRipperClient) post(ctx context.Context, path string, form url.Values) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// LoadFolder loads a Unity *_Data folder.
func (c *AssetRipperClient) LoadFolder(ctx context.Context, gameDataDir string) error {
	return c.post(ctx, "/LoadFolder", url.Values{"Path": {gameDataDir}})
}

// UpdateSetting sets a single AssetRipper setting (key=value).
func (c *AssetRipperClient) UpdateSetting(ctx context.Context, key, value string) error {
	return c.post(ctx, "/Settings/Update", url.Values{key: {value}})
}

// Reset clears the loaded game from the AssetRipper instance.
func (c *AssetRipperClient) Reset(ctx context.Context) error {
	return c.post(ctx, "/Reset", nil)
}

// ExportPrimaryContent runs the primary-content export: LoadFolder -> Export/PrimaryContent
// (primary/visual content only: meshes, textures, sprites; skips full project structure
// including scripts, ScriptableObjects, and TextAssets) -> Reset.
func (c *AssetRipperClient) ExportPrimaryContent(ctx context.Context, gameDataDir, outDir string) error {
	if err := c.LoadFolder(ctx, gameDataDir); err != nil {
		return err
	}
	if err := c.post(ctx, "/Export/PrimaryContent", url.Values{"Path": {outDir}}); err != nil {
		return err
	}
	return c.Reset(ctx)
}

// ExportUnityProject runs the FULL export (opt-in; large). LoadFolder ->
// Export/UnityProject -> Reset.
func (c *AssetRipperClient) ExportUnityProject(ctx context.Context, gameDataDir, outDir string) error {
	if err := c.LoadFolder(ctx, gameDataDir); err != nil {
		return err
	}
	if err := c.post(ctx, "/Export/UnityProject", url.Values{"Path": {outDir}}); err != nil {
		return err
	}
	return c.Reset(ctx)
}

// itoaInt renders an int as a decimal string. Kept local to this package so the
// recipe code does not import a formatter from elsewhere just for launch args.
func itoaInt(v int) string { return fmt.Sprintf("%d", v) }

// freePort asks the OS for an available TCP port and returns it. The listener
// is closed immediately; the port is then reused by the spawned process. There
// is a tiny race window, but binding 127.0.0.1:0 picks a port unlikely to be
// taken before the child binds it.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitReady polls the AssetRipper root until it responds or ctx is done.
func (c *AssetRipperClient) waitReady(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("assetripper not ready: %w", ctx.Err())
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/", nil)
		resp, err := c.HTTP.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ripperLaunchArgs builds the headless launch args for AssetRipper.GUI.Free.exe.
func ripperLaunchArgs(port int) []string {
	return []string{"--headless", "--port", itoaInt(port)}
}

// ripperEnv redirects the spawned process's temp to the output disk so large
// extractions don't fill C:.
func ripperEnv(tmpDir string) []string {
	return []string{"TEMP=" + tmpDir, "TMP=" + tmpDir}
}

// RunAssetRipperExport launches AssetRipper headless, drives the export, and
// shuts it down. When full is false, exports PrimaryContent (config-only);
// when true, exports the full Unity project (opt-in, large).
// onLine receives the process's stdout lines for logging.
func RunAssetRipperExport(ctx context.Context, exePath, gameDataDir, outDir, tmpDir string, full bool, onLine func(string)) error {
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("create ripper tmp dir: %w", err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create ripper out dir: %w", err)
	}

	port, err := freePort()
	if err != nil {
		return fmt.Errorf("pick assetripper port: %w", err)
	}

	// Launch headless in the background; it serves until the process is killed.
	// Use the breakaway spawn so AssetRipper escapes morgue's per-process Job
	// Object memory cap: loading a large IL2CPP game runs Cpp2IL over the whole
	// GameAssembly (LC2 ≈ 165 MB) and peaks well above the default 4 GiB cap.
	// Under the cap, /LoadFolder is throttled and Kestrel returns HTTP 500, so the
	// export silently produces no assets. (Same reason the InspectorRedux + Ghidra
	// spawns break away.)
	launchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, runErr := util.RunCmdStreamingEnvBreakaway(launchCtx, ripperEnv(tmpDir), exePath, ripperLaunchArgs(port), filepath.Dir(exePath), onLine)
		errCh <- runErr
	}()

	cl := &AssetRipperClient{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		HTTP:    &http.Client{Timeout: 0}, // exports can be long; no client timeout
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, 60*time.Second)
	defer readyCancel()
	if err := cl.waitReady(readyCtx); err != nil {
		return err
	}

	if full {
		err = cl.ExportUnityProject(ctx, gameDataDir, outDir)
	} else {
		err = cl.ExportPrimaryContent(ctx, gameDataDir, outDir)
	}
	// Stop the server regardless of export result.
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
	}
	return err
}
