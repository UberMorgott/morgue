package recipe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

// ExportPrimaryContent runs the config-only export: LoadFolder -> Export/PrimaryContent
// (scripts/ScriptableObject/TextAsset only; skips textures/meshes/audio/video) -> Reset.
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
