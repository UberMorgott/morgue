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

// fetchLatestCommit returns the short hash of the latest commit on default branch.
// Uses atom feed — no API, no rate limit.
func fetchLatestCommit(repo string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	for _, branch := range []string{"main", "master"} {
		url := fmt.Sprintf("https://github.com/%s/commits/%s.atom", repo, branch)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		resp.Body.Close()
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
func fetchLatestRelease(repo, token string) (tagName string, assets []assetInfo, err error) {
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
func fetchReleaseCached(baseDir, repo, token string) (string, []assetInfo, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	cache := loadReleaseCache(baseDir)

	// Check fresh cache
	if entry, ok := cache.Entries[repo]; ok && time.Since(entry.CachedAt) < releaseCacheTTL {
		return entry.Tag, entry.Assets, nil
	}

	// Cache miss or stale — call API
	tag, assets, err := fetchLatestRelease(repo, token)
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

// installFromGitHub downloads and extracts a GitHub release asset.
// Uses cached release info to avoid API rate limits.
// Falls back to direct URL download when API is unavailable.
// Returns the version tag on success.
func installFromGitHub(tool ToolDef, destDir, token string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) (string, error) {
	baseDir := filepath.Dir(destDir)

	tagName, assets, err := fetchReleaseCached(baseDir, tool.Repo, token)
	if err == nil {
		matched := matchAssets(assets, tool.AssetGlob)
		if len(matched) > 0 {
			for _, asset := range matched {
				archivePath := filepath.Join(destDir, asset.Name)

				// Reuse the shared downloader so this release-asset path gets the
				// same bounded retry-with-backoff and enriched (URL + timeout)
				// errors as every other download.
				if err := downloadFile(asset.URL, archivePath, onProgress); err != nil {
					os.Remove(archivePath)
					return "", err
				}

				if isArchiveFile(archivePath) {
					if onExtract != nil {
						onExtract()
					}
					if err := extractArchive(archivePath, destDir); err != nil {
						return "", err
					}
					os.Remove(archivePath)
				}
				// Plain files (exe, dll, etc.) stay in destDir as-is.
			}

			versionFile := filepath.Join(destDir, ".version")
			os.WriteFile(versionFile, []byte(tagName), 0644)

			return tagName, nil
		}
	}

	// Fallback: direct download without API
	version, verErr := fetchLatestVersion(tool.Repo)
	if verErr != nil {
		// Return original API error if version redirect also fails
		if err != nil {
			return "", err
		}
		return "", verErr
	}

	if dlErr := tryDirectDownload(tool, version, destDir, onProgress, onExtract); dlErr != nil {
		return "", fmt.Errorf("install %s: API unavailable and direct download failed: %w", tool.Name, dlErr)
	}

	os.WriteFile(filepath.Join(destDir, ".version"), []byte(version), 0644)
	return version, nil
}

// tryDirectDownload attempts to download a release asset without using the GitHub API.
// It scrapes the expanded_assets HTML page to discover real asset names,
// then matches them using the same glob logic as the API path.
func tryDirectDownload(tool ToolDef, version, destDir string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) error {
	assets, err := scrapeReleaseAssets(tool.Repo, version)
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
		if err := downloadFile(asset.URL, archivePath, onProgress); err != nil {
			os.Remove(archivePath)
			return fmt.Errorf("download %s: %w", asset.Name, err)
		}

		if isArchiveFile(archivePath) {
			if onExtract != nil {
				onExtract()
			}
			if err := extractArchive(archivePath, destDir); err != nil {
				return err
			}
			os.Remove(archivePath)
		}
		// Plain files (exe, dll, etc.) stay in destDir as-is.
	}
	return nil
}

// scrapeReleaseAssets fetches the expanded_assets HTML fragment for a GitHub release
// and extracts download links. This does not use the GitHub API and is not rate-limited.
func scrapeReleaseAssets(repo, tag string) ([]assetInfo, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/expanded_assets/%s", repo, tag)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
