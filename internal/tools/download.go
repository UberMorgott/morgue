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
func extractArchive(archivePath, destDir string) error {
	ext := filepath.Ext(archivePath)
	switch ext {
	case ".zip":
		return extractZip(archivePath, destDir)
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
	defer os.Remove(destPath)
	return extractArchive(destPath, destDir)
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

	_, err = util.RunCmd(ctx, dotnetBin, []string{
		"tool", "install", tool.DotnetID, "--tool-path", destDir,
	}, "")
	return err
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
