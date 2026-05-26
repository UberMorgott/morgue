package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// fetchLatestRelease gets the latest release from a GitHub repo.
func fetchLatestRelease(repo string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release for %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API %s: HTTP %d", repo, resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release for %s: %w", repo, err)
	}
	return &release, nil
}

// matchAsset finds the first asset matching a glob pattern.
func matchAsset(assets []githubAsset, glob string) (githubAsset, bool) {
	for _, a := range assets {
		matched, _ := filepath.Match(glob, a.Name)
		if matched {
			return a, true
		}
	}
	// Fallback: case-insensitive contains
	lower := strings.ToLower(glob)
	for _, a := range assets {
		if strings.Contains(strings.ToLower(a.Name), lower) {
			return a, true
		}
	}
	return githubAsset{}, false
}

// installFromGitHub downloads and extracts a GitHub release asset.
func installFromGitHub(opts DownloadOptions, tool ToolDef, destDir string) error {
	release, err := fetchLatestRelease(tool.Repo)
	if err != nil {
		return err
	}

	asset, found := matchAsset(release.Assets, tool.AssetGlob)
	if !found {
		return fmt.Errorf("no matching asset for %s (glob: %s) in release %s",
			tool.Name, tool.AssetGlob, release.TagName)
	}

	opts.URL = asset.BrowserDownloadURL
	opts.DestPath = filepath.Join(destDir, asset.Name)
	if err := downloadFile(opts); err != nil {
		return err
	}
	defer os.Remove(opts.DestPath)

	return extractArchive(opts.DestPath, destDir)
}
