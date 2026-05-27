package tools

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"

	"github.com/UberMorgott/morgue/internal/util"
)

// downloadFile downloads a URL to a local file with progress reporting.
// The grab library handles resume via ETag/partial files automatically.
func downloadFile(url, destPath string, onProgress func(bytesDown, bytesTotal int64)) error {
	client := grab.NewClient()
	req, _ := grab.NewRequest(destPath, url)

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

		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract %s: %w", f.Name, copyErr)
		}
	}
	return nil
}

// installFromURL downloads a tool from a direct URL and extracts it.
func installFromURL(tool ToolDef, destDir string, onProgress func(bytesDown, bytesTotal int64)) error {
	destPath := filepath.Join(destDir, filepath.Base(tool.URL))
	if err := downloadFile(tool.URL, destPath, onProgress); err != nil {
		return err
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
func installFromGitBuild(tool ToolDef, destDir string, onProgress func(string, int64, int64)) (string, error) {
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
