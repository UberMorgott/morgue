package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// DownloadProgress reports download status.
type DownloadProgress struct {
	Tool       string
	BytesTotal int64
	BytesDone  int64
	Done       bool
	Err        error
}

// downloadFile downloads a URL to a local file path.
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

// extractArchive extracts an archive file into destDir.
// Supports .zip archives via Go's archive/zip.
func extractArchive(archivePath, destDir string) error {
	// Use platform unzip for simplicity and reliability
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ext := filepath.Ext(archivePath)
	switch ext {
	case ".zip":
		_, err := util.RunCmd(ctx, "powershell", []string{
			"-NoProfile", "-Command",
			fmt.Sprintf("Expand-Archive -Force -Path '%s' -DestinationPath '%s'", archivePath, destDir),
		}, "")
		return err
	default:
		return fmt.Errorf("unsupported archive format: %s", ext)
	}
}

// installFromURL downloads a tool from a direct URL.
func installFromURL(tool ToolDef, destDir string) error {
	archivePath := filepath.Join(destDir, filepath.Base(tool.URL))
	if err := downloadFile(tool.URL, archivePath); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	return extractArchive(archivePath, destDir)
}

// installDotnetTool installs a .NET global tool.
func installDotnetTool(tool ToolDef, destDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := util.RunCmd(ctx, "dotnet", []string{
		"tool", "install", tool.DotnetID, "--tool-path", destDir,
	}, "")
	return err
}
