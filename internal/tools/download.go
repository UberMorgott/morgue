package tools

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"

	"github.com/UberMorgott/morgue/internal/util"
)

// downloadMaxAttempts bounds how many times a download is retried on a
// (likely transient) network failure before giving up.
const downloadMaxAttempts = 3

// isNetworkTimeout reports whether err looks like a transient network/TLS
// timeout worth retrying. On Windows behind a throttling firewall, grab/HTTP
// surfaces WININET errors such as 0x80072EE2 (timeout) wrapped in the chain
// (e.g. "failed to loadSystemRoots: exit status 0x80072ee2").
func isNetworkTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "loadsystemroots") ||
		strings.Contains(msg, "0x80072ee2") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "deadline exceeded")
}

// downloadFile downloads a URL to a local file with progress reporting.
// The grab library handles resume via ETag/partial files automatically.
// Transient network timeouts are retried with bounded exponential backoff, and
// the returned error carries the target URL plus a network-timeout hint so a
// failure is actionable in a headless/redirected log.
func downloadFile(url, destPath string, onProgress func(bytesDown, bytesTotal int64)) error {
	var lastErr error
	for attempt := 1; attempt <= downloadMaxAttempts; attempt++ {
		lastErr = downloadOnce(url, destPath, onProgress)
		if lastErr == nil {
			return nil
		}
		if !isNetworkTimeout(lastErr) {
			break
		}
		if attempt < downloadMaxAttempts {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}

	if isNetworkTimeout(lastErr) {
		return fmt.Errorf("download %s: network timeout after %d attempts (firewall/connectivity?): %w",
			url, downloadMaxAttempts, lastErr)
	}
	return fmt.Errorf("download %s: %w", url, lastErr)
}

// downloadOnce performs a single download attempt.
func downloadOnce(url, destPath string, onProgress func(bytesDown, bytesTotal int64)) error {
	client := grab.NewClient()
	req, err := grab.NewRequest(destPath, url)
	if err != nil {
		return fmt.Errorf("create download request for %s: %w", destPath, err)
	}

	resp := client.Do(req)

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

	if onProgress != nil {
		onProgress(resp.BytesComplete(), resp.Size())
	}

	return resp.Err()
}

// verifyHash checks the SHA-256 hash of a downloaded file against an expected value.
// If expectedHash is empty, verification is skipped.
func verifyHash(path, expectedHash string) error {
	if expectedHash == "" {
		return nil
	}
	actual, err := util.SHA256File(path)
	if err != nil {
		return fmt.Errorf("compute hash of %s: %w", filepath.Base(path), err)
	}
	if actual != expectedHash {
		os.Remove(path)
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s", filepath.Base(path), expectedHash, actual)
	}
	return nil
}

// isArchiveFile returns true if the file is an archive that should be extracted.
func isArchiveFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".zip"
}

// extractArchive extracts an archive file into destDir.
// Standalone executables (.exe) are kept as-is (no extraction needed).
func extractArchive(archivePath, destDir string) error {
	ext := strings.ToLower(filepath.Ext(archivePath))
	switch ext {
	case ".zip":
		return extractZip(archivePath, destDir)
	case ".exe":
		// Standalone executable — no extraction needed.
		// The caller downloaded it into destDir already; nothing to do.
		return nil
	default:
		return fmt.Errorf("unsupported archive format: %s", ext)
	}
}

// extractZip extracts a ZIP archive using Go's archive/zip stdlib.
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", archivePath, err)
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)

		// Prevent zip slip
		rel, err := filepath.Rel(destDir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", target, err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open entry %s: %w", f.Name, err)
		}

		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create %s: %w", target, err)
		}

		// Limit extracted file size to 2GB to prevent zip bombs
		_, copyErr := io.Copy(out, io.LimitReader(rc, 2*1024*1024*1024))
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract %s: %w", f.Name, copyErr)
		}
	}
	return nil
}

// installFromURLs downloads multiple files from direct URLs into destDir.
// Archives (.zip) are extracted; plain files are kept as-is.
func installFromURLs(urls []string, destDir string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) error {
	for _, u := range urls {
		destPath := filepath.Join(destDir, filepath.Base(u))
		if err := downloadFile(u, destPath, onProgress); err != nil {
			return fmt.Errorf("download %s: %w", filepath.Base(u), err)
		}
		if isArchiveFile(destPath) {
			if onExtract != nil {
				onExtract()
			}
			if err := extractArchive(destPath, destDir); err != nil {
				return err
			}
			os.Remove(destPath)
		}
	}
	return nil
}

// installFromURL downloads a tool from a direct URL and extracts it.
func installFromURL(tool ToolDef, destDir string, onProgress func(bytesDown, bytesTotal int64), onExtract func()) error {
	destPath := filepath.Join(destDir, filepath.Base(tool.URL))
	if err := downloadFile(tool.URL, destPath, onProgress); err != nil {
		return err
	}
	if err := verifyHash(destPath, tool.SHA256); err != nil {
		return err
	}
	if onExtract != nil && isArchiveFile(destPath) {
		onExtract()
	}
	if err := extractArchive(destPath, destDir); err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(destPath)) != ".exe" {
		os.Remove(destPath)
	}
	return nil
}

// installDotnetTool installs a .NET global tool.
// It prefers a local portable dotnet SDK, then falls back to the system one.
func installDotnetTool(tool ToolDef, destDir string) error {
	dotnetBin, err := findDotnetBin(destDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{"tool", "install", tool.DotnetID, "--tool-path", destDir}
	if tool.DotnetVersion != "" {
		args = append(args, "--version", tool.DotnetVersion)
	}
	_, err = util.RunCmd(ctx, dotnetBin, args, "")
	return err
}

// installFromGitBuild downloads a GitHub repo as zip, builds it with dotnet, and
// places the output binaries in destDir. This is used for tools that have no
// pre-built releases.
func installFromGitBuild(tool ToolDef, destDir string, onProgress func(string, int64, int64), onExtract func()) (string, error) {
	// 1. Check for a tagged release; fall back to "main" branch.
	version, _ := fetchLatestVersion(tool.Repo)
	if version == "" {
		version = "main"
	}

	// 2. Download repo source as zip (no git clone needed).
	zipURL := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.zip", tool.Repo)
	if version != "main" {
		zipURL = fmt.Sprintf("https://github.com/%s/archive/refs/tags/%s.zip", tool.Repo, version)
	}

	zipPath := filepath.Join(destDir, "source.zip")
	err := downloadFile(zipURL, zipPath, func(down, total int64) {
		if onProgress != nil {
			onProgress(tool.Name, down, total)
		}
	})
	if err != nil {
		return "", fmt.Errorf("download source %s: %w", tool.Repo, err)
	}
	defer os.Remove(zipPath)

	// 3. Extract into a temporary src directory.
	srcDir := filepath.Join(destDir, "src")
	os.MkdirAll(srcDir, 0755)
	if onExtract != nil {
		onExtract()
	}
	if err := extractArchive(zipPath, srcDir); err != nil {
		return "", fmt.Errorf("extract source: %w", err)
	}

	// 4. GitHub zips extract into a {repo}-{branch}/ subdirectory.
	entries, _ := os.ReadDir(srcDir)
	projectDir := srcDir
	for _, e := range entries {
		if e.IsDir() {
			projectDir = filepath.Join(srcDir, e.Name())
			break
		}
	}

	// 5. Build with dotnet.
	dotnetBin, err := findDotnetBin(destDir)
	if err != nil {
		os.RemoveAll(srcDir)
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, dotnetBin, "build", "-c", "Release", "-o", destDir)
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(srcDir)
		return "", fmt.Errorf("dotnet build failed: %w\n%s", err, string(output))
	}

	// 6. Clean up source.
	os.RemoveAll(srcDir)

	// 7. Persist version.
	if version == "" {
		version = "main"
	}
	os.WriteFile(filepath.Join(destDir, ".version"), []byte(version), 0644)

	return version, nil
}

// installFromNuGet downloads a NuGet package (.nupkg) and extracts it as a ZIP.
// This bypasses `dotnet tool install` which requires a matching SDK version.
func installFromNuGet(tool ToolDef, destDir string, onProgress func(int64, int64), onExtract func(string)) (string, error) {
	version := tool.DotnetVersion
	if version == "" {
		var err error
		version, err = fetchNuGetLatestVersion(tool.DotnetID)
		if err != nil {
			return "", fmt.Errorf("fetch latest version: %w", err)
		}
	}

	url := fmt.Sprintf("https://www.nuget.org/api/v2/package/%s/%s", tool.DotnetID, version)
	nupkgPath := filepath.Join(destDir, "package.zip")

	if err := downloadFile(url, nupkgPath, func(down, total int64) {
		if onProgress != nil {
			onProgress(down, total)
		}
	}); err != nil {
		return "", fmt.Errorf("download nupkg: %w", err)
	}

	if onExtract != nil {
		onExtract(tool.Name)
	}

	if err := extractArchive(nupkgPath, destDir); err != nil {
		return "", fmt.Errorf("extract nupkg: %w", err)
	}
	os.Remove(nupkgPath)

	return version, nil
}

// findDotnetBin locates the dotnet binary: local portable first, then system PATH.
func findDotnetBin(toolDestDir string) (string, error) {
	baseDir := filepath.Dir(toolDestDir)
	localBin := filepath.Join(baseDir, "runtimes", "dotnet", "dotnet.exe")
	if _, err := os.Stat(localBin); err == nil {
		return localBin, nil
	}

	if sysPath, err := exec.LookPath("dotnet"); err == nil {
		return sysPath, nil
	}

	return "", fmt.Errorf("dotnet SDK not found — install via Tools page")
}
