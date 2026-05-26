package tools

import (
	"archive/zip"
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
		if !filepath.IsAbs(target) {
			target = filepath.Clean(target)
		}
		rel, err := filepath.Rel(destDir, target)
		if err != nil || rel == ".." || (len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator)) {
			continue // skip entries that escape destDir
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
