package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/google/go-github/v60/github"
)

type assetInfo struct {
	Name string
	URL  string
}

// fetchLatestRelease gets the latest release info from GitHub.
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
// Returns the version tag on success.
func installFromGitHub(tool ToolDef, destDir string, onProgress func(bytesDown, bytesTotal int64)) (string, error) {
	tagName, assets, err := fetchLatestRelease(tool.Repo)
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
