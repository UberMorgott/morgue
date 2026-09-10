package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v74/github"
)

// httpDo issues a request bound to ctx, so a cancelled install/pipeline aborts
// the in-flight call instead of waiting out the client Timeout.
func httpDo(ctx context.Context, client *http.Client, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

type assetInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// --- Release cache ---

type releaseCacheEntry struct {
	Tag      string      `json:"tag"`
	Assets   []assetInfo `json:"assets"`
	CachedAt time.Time   `json:"cached_at"`
}

type releaseCache struct {
	Entries map[string]releaseCacheEntry `json:"entries"`
}

const releaseCacheTTL = 1 * time.Hour
const releaseCacheFile = ".release-cache.json"

var (
	cacheMu sync.Mutex
)

func loadReleaseCache(baseDir string) releaseCache {
	rc := releaseCache{Entries: make(map[string]releaseCacheEntry)}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(baseDir, releaseCacheFile)))
	if err != nil {
		return rc
	}
	_ = json.Unmarshal(data, &rc)
	if rc.Entries == nil {
		rc.Entries = make(map[string]releaseCacheEntry)
	}
	return rc
}

func saveReleaseCache(baseDir string, rc releaseCache) {
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(baseDir, releaseCacheFile), data, 0644)
}

// fetchLatestCommit returns the short hash of the latest commit on default branch.
// Uses atom feed — no API, no rate limit.
func fetchLatestCommit(ctx context.Context, repo string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	for _, branch := range []string{"main", "master"} {
		url := fmt.Sprintf("https://github.com/%s/commits/%s.atom", repo, branch)
		resp, err := httpDo(ctx, client, http.MethodGet, url)
		if err != nil {
			continue
		}
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		_ = resp.Body.Close()
		if err != nil {
			continue
		}

		// Parse: <id>tag:github.com,2008:Grit::Commit/{hash}</id>
		content := string(body)
		marker := "Grit::Commit/"
		idx := strings.Index(content, marker)
		if idx < 0 {
			continue
		}

		start := idx + len(marker)
		end := strings.Index(content[start:], "<")
		if end < 0 {
			continue
		}

		hash := content[start : start+end]
		if len(hash) >= 7 {
			return hash[:7], nil
		}
	}
	return "", fmt.Errorf("no commits found for %s", repo)
}

// --- Version check via HTTP redirect (no API) ---

// fetchLatestVersion gets the latest release tag from GitHub without using the API.
// It issues a GET to /releases/latest and parses the redirect URL.
func fetchLatestVersion(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/latest", repo)

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := httpDo(ctx, client, http.MethodGet, url)
	if err != nil {
		return "", fmt.Errorf("check latest version %s: %w", repo, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("expected redirect for %s/releases/latest, got %d", repo, resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no Location header in redirect for %s", repo)
	}

	// Location: https://github.com/owner/repo/releases/tag/v1.2.3
	parts := strings.Split(loc, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("unexpected redirect URL for %s: %s", repo, loc)
	}
	return parts[len(parts)-1], nil
}

// --- API-based release fetch (fallback, uses rate-limited API) ---

// fetchLatestRelease gets the latest release info from GitHub API.
// This consumes API rate limit — prefer fetchLatestVersion + cache.
func fetchLatestRelease(ctx context.Context, repo, token string) (tagName string, assets []assetInfo, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid repo: %s", repo)
	}

	var client *github.Client
	if token != "" {
		client = github.NewClient(nil).WithAuthToken(token)
	} else {
		client = github.NewClient(nil)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	release, _, err := client.Repositories.GetLatestRelease(ctx, parts[0], parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("fetch release %s: %w", repo, err)
	}

	var infos []assetInfo
	for _, a := range release.Assets {
		infos = append(infos, assetInfo{
			Name: a.GetName(),
			URL:  a.GetBrowserDownloadURL(),
		})
	}
	return release.GetTagName(), infos, nil
}

// resolveInstallTag returns the tag to install: the pinned ToolDef.Version when
// set, otherwise the latest tag discovered from the release feed.
func resolveInstallTag(tool ToolDef, latestTag string) string {
	if tool.Version != "" {
		return tool.Version
	}
	return latestTag
}

// fetchReleaseByTag fetches a specific release (not "latest") via the GitHub API.
func fetchReleaseByTag(ctx context.Context, repo, token, tag string) (string, []assetInfo, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid repo: %s", repo)
	}
	var client *github.Client
	if token != "" {
		client = github.NewClient(nil).WithAuthToken(token)
	} else {
		client = github.NewClient(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, _, err := client.Repositories.GetReleaseByTag(ctx, parts[0], parts[1], tag)
	if err != nil {
		return "", nil, fmt.Errorf("fetch release %s@%s: %w", repo, tag, err)
	}
	var infos []assetInfo
	for _, a := range release.Assets {
		infos = append(infos, assetInfo{Name: a.GetName(), URL: a.GetBrowserDownloadURL()})
	}
	return release.GetTagName(), infos, nil
}

// downloadAndExtract downloads each matched asset into destDir, extracting archives.
func downloadAndExtract(ctx context.Context, matched []assetInfo, destDir string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) error {
	for _, asset := range matched {
		archivePath := filepath.Join(destDir, asset.Name)
		if err := downloadFile(ctx, asset.URL, archivePath, onProgress); err != nil {
			_ = os.Remove(archivePath)
			return err
		}
		if isArchiveFile(archivePath) {
			if onExtract != nil {
				onExtract()
			}
			if err := extractArchive(archivePath, destDir); err != nil {
				return err
			}
			_ = os.Remove(archivePath)
		}
		// Plain files (exe, dll, etc.) stay in destDir as-is.
	}
	return nil
}

// fetchReleaseCached returns release info, using cache when available.
// On cache miss it calls the GitHub API and saves the result.
// On API error it falls back to stale cache if available.
func fetchReleaseCached(ctx context.Context, baseDir, repo, token string) (string, []assetInfo, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	cache := loadReleaseCache(baseDir)

	// Check fresh cache
	if entry, ok := cache.Entries[repo]; ok && time.Since(entry.CachedAt) < releaseCacheTTL {
		return entry.Tag, entry.Assets, nil
	}

	// Cache miss or stale — call API
	tag, assets, err := fetchLatestRelease(ctx, repo, token)
	if err != nil {
		// API failed — use stale cache if available
		if entry, ok := cache.Entries[repo]; ok {
			return entry.Tag, entry.Assets, nil
		}
		return "", nil, err
	}

	// Save to cache
	cache.Entries[repo] = releaseCacheEntry{
		Tag:      tag,
		Assets:   assets,
		CachedAt: time.Now(),
	}
	saveReleaseCache(baseDir, cache)

	return tag, assets, nil
}

// matchAssets returns all assets matching a glob pattern.
func matchAssets(assets []assetInfo, glob string) []assetInfo {
	var result []assetInfo
	for _, a := range assets {
		if matched, _ := filepath.Match(glob, a.Name); matched {
			result = append(result, a)
		}
	}
	if len(result) > 0 {
		return result
	}
	// Fallback: case-insensitive substring match using glob parts split on wildcards
	parts := strings.Split(strings.ToLower(glob), "*")
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		allMatch := true
		for _, p := range parts {
			if p == "" {
				continue
			}
			idx := strings.Index(name, p)
			if idx < 0 {
				allMatch = false
				break
			}
			name = name[idx+len(p):]
		}
		if allMatch {
			result = append(result, a)
		}
	}
	return result
}

// nonInstallableAssetExts are sidecar files (signatures, checksums, notes) that
// must never be selected as the primary installable asset.
var nonInstallableAssetExts = []string{".asc", ".sig", ".sha256", ".sha512", ".md5", ".txt", ".json"}

// selectPrimaryAsset picks exactly ONE installable archive from a matched set,
// making asset selection deterministic when a glob matches several files. It
// drops signature/checksum/text sidecars, prefers a .zip, then any archive,
// then the first remaining asset. Returns nil if nothing installable remains.
func selectPrimaryAsset(matched []assetInfo) *assetInfo {
	var candidates []assetInfo
	for _, a := range matched {
		lower := strings.ToLower(a.Name)
		skip := false
		for _, ext := range nonInstallableAssetExts {
			if strings.HasSuffix(lower, ext) {
				skip = true
				break
			}
		}
		if !skip {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Prefer .zip explicitly (the InspectorRedux/AssetRipper CLI archives).
	for i := range candidates {
		if strings.HasSuffix(strings.ToLower(candidates[i].Name), ".zip") {
			return &candidates[i]
		}
	}
	// Then any recognized archive.
	for i := range candidates {
		if isArchiveName(candidates[i].Name) {
			return &candidates[i]
		}
	}
	return &candidates[0]
}

// isArchiveName reports whether a filename looks like a supported archive.
func isArchiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".zip", ".tar.gz", ".tgz", ".7z", ".gz"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// validateBinaryInstalled confirms the tool's expected Binary is resolvable under
// destDir after extraction (including inside a nested archive top-level dir, e.g.
// Il2CppInspectorRedux.CLI-win-x64/). Returns a clear error if not, so a
// half-installed tool fails here rather than later in Resolve().
func validateBinaryInstalled(tool ToolDef, destDir string) error {
	if tool.Binary == "" {
		return nil
	}
	if findBinaryRecursive(destDir, tool.Binary) == "" {
		return fmt.Errorf("install %s: expected binary %q not found under %s after extraction",
			tool.Name, tool.Binary, destDir)
	}
	return nil
}

// installFromGitHub downloads and extracts a GitHub release asset.
// Uses cached release info to avoid API rate limits.
// Falls back to direct URL download when API is unavailable.
// Returns the version tag on success.
func installFromGitHub(ctx context.Context, tool ToolDef, destDir, token string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) (string, error) {
	baseDir := filepath.Dir(destDir)

	// Pinned version: reproducibility requires the EXACT tag. Fetch it via API and
	// fail loudly if the pinned release/asset is missing — never silently fall back
	// to scrape/latest, which would defeat the lock.
	if tool.Version != "" {
		tag := resolveInstallTag(tool, "")
		_, assets, err := fetchReleaseByTag(ctx, tool.Repo, token, tag)
		if err != nil {
			return "", fmt.Errorf("pinned release %s for %s not found: %w", tag, tool.Name, err)
		}
		matched := matchAssets(assets, tool.AssetGlob)
		primary := selectPrimaryAsset(matched)
		if primary == nil {
			return "", fmt.Errorf("pinned release %s for %s: no installable asset matched glob %q (found %d assets)",
				tag, tool.Name, tool.AssetGlob, len(assets))
		}
		if derr := downloadAndExtract(ctx, []assetInfo{*primary}, destDir, onProgress, onExtract); derr != nil {
			return "", derr
		}
		if verr := validateBinaryInstalled(tool, destDir); verr != nil {
			return "", verr
		}
		_ = os.WriteFile(filepath.Join(destDir, ".version"), []byte(tag), 0644)
		return tag, nil
	}

	tagName, assets, err := fetchReleaseCached(ctx, baseDir, tool.Repo, token)
	if err == nil {
		matched := matchAssets(assets, tool.AssetGlob)
		if len(matched) > 0 {
			for _, asset := range matched {
				archivePath := filepath.Join(destDir, asset.Name)

				// Reuse the shared downloader so this release-asset path gets the
				// same bounded retry-with-backoff and enriched (URL + timeout)
				// errors as every other download.
				if err := downloadFile(ctx, asset.URL, archivePath, onProgress); err != nil {
					_ = os.Remove(archivePath)
					return "", err
				}

				if isArchiveFile(archivePath) {
					if onExtract != nil {
						onExtract()
					}
					if err := extractArchive(archivePath, destDir); err != nil {
						return "", err
					}
					_ = os.Remove(archivePath)
				}
				// Plain files (exe, dll, etc.) stay in destDir as-is.
			}

			versionFile := filepath.Join(destDir, ".version")
			_ = os.WriteFile(versionFile, []byte(tagName), 0644)

			return tagName, nil
		}
	}

	// Fallback: direct download without API
	version, verErr := fetchLatestVersion(ctx, tool.Repo)
	if verErr != nil {
		// Return original API error if version redirect also fails
		if err != nil {
			return "", err
		}
		return "", verErr
	}

	if dlErr := tryDirectDownload(ctx, tool, version, destDir, onProgress, onExtract); dlErr != nil {
		return "", fmt.Errorf("install %s: API unavailable and direct download failed: %w", tool.Name, dlErr)
	}

	_ = os.WriteFile(filepath.Join(destDir, ".version"), []byte(version), 0644)
	return version, nil
}

// tryDirectDownload attempts to download a release asset without using the GitHub API.
// It scrapes the expanded_assets HTML page to discover real asset names,
// then matches them using the same glob logic as the API path.
func tryDirectDownload(ctx context.Context, tool ToolDef, version, destDir string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) error {
	assets, err := scrapeReleaseAssets(ctx, tool.Repo, version)
	if err != nil {
		return fmt.Errorf("scrape assets for %s %s: %w", tool.Repo, version, err)
	}

	matched := matchAssets(assets, tool.AssetGlob)
	if len(matched) == 0 {
		return fmt.Errorf("no matching asset for %s in scraped list (glob: %s, found %d assets)",
			tool.Name, tool.AssetGlob, len(assets))
	}

	for _, asset := range matched {
		archivePath := filepath.Join(destDir, asset.Name)
		if err := downloadFile(ctx, asset.URL, archivePath, onProgress); err != nil {
			_ = os.Remove(archivePath)
			return fmt.Errorf("download %s: %w", asset.Name, err)
		}

		if isArchiveFile(archivePath) {
			if onExtract != nil {
				onExtract()
			}
			if err := extractArchive(archivePath, destDir); err != nil {
				return err
			}
			_ = os.Remove(archivePath)
		}
		// Plain files (exe, dll, etc.) stay in destDir as-is.
	}
	return nil
}

// scrapeReleaseAssets fetches the expanded_assets HTML fragment for a GitHub release
// and extracts download links. This does not use the GitHub API and is not rate-limited.
func scrapeReleaseAssets(ctx context.Context, repo, tag string) ([]assetInfo, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/expanded_assets/%s", repo, tag)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpDo(ctx, client, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expanded_assets returned %d", resp.StatusCode)
	}

	// Read body — these pages are small (typically < 100KB), limit to 2MB safety cap
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read expanded_assets body: %w", err)
	}
	body := string(rawBody)

	// Parse href="/owner/repo/releases/download/tag/filename"
	prefix := fmt.Sprintf("/%s/releases/download/", repo)
	var assets []assetInfo
	for {
		idx := strings.Index(body, prefix)
		if idx < 0 {
			break
		}
		body = body[idx:]
		// Find the closing quote
		end := strings.IndexByte(body[1:], '"')
		if end < 0 {
			break
		}
		path := body[:end+1]
		body = body[end+1:]

		// Extract filename (last path segment)
		parts := strings.Split(path, "/")
		name := parts[len(parts)-1]
		if name == "" {
			continue
		}

		dlURL := "https://github.com" + path
		assets = append(assets, assetInfo{Name: name, URL: dlURL})
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no download links found on expanded_assets page")
	}
	return assets, nil
}
