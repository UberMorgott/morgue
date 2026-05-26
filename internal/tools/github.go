package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/google/go-github/v60/github"
)

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
	data, err := os.ReadFile(filepath.Join(baseDir, releaseCacheFile))
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

// --- Version check via HTTP redirect (no API) ---

// fetchLatestVersion gets the latest release tag from GitHub without using the API.
// It issues a GET to /releases/latest and parses the redirect URL.
func fetchLatestVersion(repo string) (string, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/latest", repo)

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("check latest version %s: %w", repo, err)
	}
	defer resp.Body.Close()

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
func fetchLatestRelease(repo string) (tagName string, assets []assetInfo, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid repo: %s", repo)
	}

	client := github.NewClient(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

// fetchReleaseCached returns release info, using cache when available.
// On cache miss it calls the GitHub API and saves the result.
// On API error it falls back to stale cache if available.
func fetchReleaseCached(baseDir, repo string) (string, []assetInfo, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	cache := loadReleaseCache(baseDir)

	// Check fresh cache
	if entry, ok := cache.Entries[repo]; ok && time.Since(entry.CachedAt) < releaseCacheTTL {
		return entry.Tag, entry.Assets, nil
	}

	// Cache miss or stale — call API
	tag, assets, err := fetchLatestRelease(repo)
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

// matchAsset finds the first asset matching a glob pattern.
func matchAsset(assets []assetInfo, glob string) (assetInfo, bool) {
	for _, a := range assets {
		if matched, _ := filepath.Match(glob, a.Name); matched {
			return a, true
		}
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
			return a, true
		}
	}
	return assetInfo{}, false
}

// installFromGitHub downloads and extracts a GitHub release asset.
// Uses cached release info to avoid API rate limits.
// Returns the version tag on success.
func installFromGitHub(tool ToolDef, destDir string, onProgress func(bytesDown, bytesTotal int64)) (string, error) {
	baseDir := filepath.Dir(destDir)

	tagName, assets, err := fetchReleaseCached(baseDir, tool.Repo)
	if err != nil {
		return "", err
	}

	asset, found := matchAsset(assets, tool.AssetGlob)
	if !found {
		return "", fmt.Errorf("no matching asset for %s (glob: %s)", tool.Name, tool.AssetGlob)
	}

	archivePath := filepath.Join(destDir, asset.Name)

	client := grab.NewClient()
	req, _ := grab.NewRequest(archivePath, asset.URL)

	resp := client.Do(req)

	// Progress reporting loop
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()

	done := false
	for !done {
		select {
		case <-t.C:
			if onProgress != nil {
				onProgress(resp.BytesComplete(), resp.Size())
			}
		case <-resp.Done:
			done = true
		}
	}

	// Final progress report
	if onProgress != nil {
		onProgress(resp.BytesComplete(), resp.Size())
	}

	if err := resp.Err(); err != nil {
		os.Remove(archivePath)
		return "", fmt.Errorf("download %s: %w", asset.Name, err)
	}

	defer os.Remove(archivePath)

	if err := extractArchive(archivePath, destDir); err != nil {
		return "", err
	}

	// Save version to .version file
	versionFile := filepath.Join(destDir, ".version")
	os.WriteFile(versionFile, []byte(tagName), 0644)

	return tagName, nil
}
