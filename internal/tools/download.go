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

// DownloadOptions configures a robust file download.
type DownloadOptions struct {
	URL        string
	DestPath   string
	Retries    int
	TimeoutMin int
	OnProgress func(bytesDown, bytesTotal int64)
}

// progressReader wraps an io.Reader to report progress.
type progressReader struct {
	reader     io.Reader
	onProgress func(bytesDown, bytesTotal int64)
	done       int64
	total      int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.done += int64(n)
	if pr.onProgress != nil {
		pr.onProgress(pr.done, pr.total)
	}
	return n, err
}

// downloadFile downloads a URL to a local file with resume, retries, and progress.
func downloadFile(opts DownloadOptions) error {
	if opts.Retries <= 0 {
		opts.Retries = 3
	}
	if opts.TimeoutMin <= 0 {
		opts.TimeoutMin = 30
	}

	partPath := opts.DestPath + ".part"
	var lastErr error

	for attempt := 0; attempt < opts.Retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s, 8s...
			time.Sleep(backoff)
		}

		err := downloadAttempt(opts, partPath)
		if err == nil {
			return os.Rename(partPath, opts.DestPath)
		}
		lastErr = err
	}

	// Clean up .part on final failure
	os.Remove(partPath)
	return fmt.Errorf("download %s failed after %d attempts: %w", opts.URL, opts.Retries, lastErr)
}

// downloadAttempt performs a single download attempt with resume support.
func downloadAttempt(opts DownloadOptions, partPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.TimeoutMin)*time.Minute)
	defer cancel()

	var partSize int64
	if info, err := os.Stat(partPath); err == nil {
		partSize = info.Size()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", opts.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if partSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", partSize))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", opts.URL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// Full response — server doesn't support range or fresh download.
		partSize = 0
	case http.StatusPartialContent:
		// Resume accepted.
	case http.StatusRequestedRangeNotSatisfiable:
		// .part is corrupted or complete; start fresh.
		os.Remove(partPath)
		partSize = 0
		// Re-request without Range header.
		resp.Body.Close()
		req.Header.Del("Range")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("download %s: %w", opts.URL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download %s: HTTP %d", opts.URL, resp.StatusCode)
		}
	default:
		return fmt.Errorf("download %s: HTTP %d", opts.URL, resp.StatusCode)
	}

	// Determine expected total size.
	var expectedTotal int64
	if resp.ContentLength > 0 {
		expectedTotal = partSize + resp.ContentLength
	}

	// Open file: append if resuming, create if fresh.
	var f *os.File
	if partSize > 0 {
		f, err = os.OpenFile(partPath, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		f, err = os.Create(partPath)
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", partPath, err)
	}
	defer f.Close()

	// Wrap reader with progress reporting.
	reader := io.Reader(resp.Body)
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     resp.Body,
			onProgress: opts.OnProgress,
			done:       partSize,
			total:      expectedTotal,
		}
	}

	written, err := io.Copy(f, reader)
	if err != nil {
		return fmt.Errorf("write %s: %w", partPath, err)
	}

	// Verify size if Content-Length was provided.
	if resp.ContentLength > 0 && written != resp.ContentLength {
		return fmt.Errorf("size mismatch for %s: expected %d bytes from server, got %d",
			opts.URL, resp.ContentLength, written)
	}

	return nil
}

// downloadFileSimple is a convenience wrapper with defaults (no resume/retries).
func downloadFileSimple(url, destPath string) error {
	return downloadFile(DownloadOptions{
		URL:      url,
		DestPath: destPath,
		Retries:  1,
	})
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
func installFromURL(opts DownloadOptions, tool ToolDef, destDir string) error {
	opts.DestPath = filepath.Join(destDir, filepath.Base(tool.URL))
	opts.URL = tool.URL
	if err := downloadFile(opts); err != nil {
		return err
	}
	defer os.Remove(opts.DestPath)
	return extractArchive(opts.DestPath, destDir)
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
