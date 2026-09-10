// Package selfupdate downloads the latest morgue release from GitHub and
// replaces the running executable with it.
//
// It talks to the GitHub API through go-github (the same client the tool
// installer uses) and does the archive extraction and the binary swap itself.
// The previous implementation used github.com/creativeprojects/go-selfupdate,
// which unconditionally imports golang.org/x/crypto/openpgp (GO-2026-5932,
// unmaintained with no fix upstream) for a PGP verification path morgue never
// used: releases are fetched from GitHub over TLS.
package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v74/github"
)

const (
	repoOwner = "UberMorgott"
	repoName  = "morgue"

	// apiTimeout bounds a release lookup; the download uses its own, longer one.
	apiTimeout      = 30 * time.Second
	downloadTimeout = 30 * time.Minute

	// maxExeSize caps what we will extract out of a release archive, so a
	// malformed/hostile zip cannot decompress into memory without bound.
	maxExeSize = 512 << 20
)

// baseVersion extracts clean semver from git describe output.
// "v0.1.0-34-gbaae1cb-dirty" → "0.1.0"
// "v0.1.0" → "0.1.0"
// "0.1.0" → "0.1.0"
func baseVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	return v
}

// isNewer returns true if latestTag is strictly newer than currentVersion.
// Dev builds like "v0.1.0-34-gbaae1cb" are treated as their base version (0.1.0),
// so they won't be "downgraded" to the same release tag.
func isNewer(latestTag, currentVersion string) bool {
	latest, err := semver.NewVersion(latestTag)
	if err != nil {
		return false
	}
	current, err := semver.NewVersion(baseVersion(currentVersion))
	if err != nil {
		// Unparseable current version (e.g. "dev") — always offer update.
		return true
	}
	return latest.GreaterThan(current)
}

// releaseAsset is one downloadable file attached to a GitHub release.
type releaseAsset struct {
	Name string
	URL  string
	Size int64
}

// release is the subset of a GitHub release this package needs.
type release struct {
	Version string // tag without the leading "v"
	Asset   releaseAsset
}

// newClient returns a go-github client, authenticated when GITHUB_TOKEN is set
// (which is what the previous go-selfupdate GitHub source did too).
func newClient() *github.Client {
	client := github.NewClient(nil)
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		client = client.WithAuthToken(tok)
	}
	return client
}

// latestRelease returns the newest release and the asset matching this
// OS/arch. It returns (nil, nil) when the repo has no release or none of its
// assets is for this platform — the "not found" case, not an error.
func latestRelease(ctx context.Context) (*release, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	rel, resp, err := newClient().Repositories.GetLatestRelease(ctx, repoOwner, repoName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("detect latest: %w", err)
	}

	assets := make([]releaseAsset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		assets = append(assets, releaseAsset{
			Name: a.GetName(),
			URL:  a.GetBrowserDownloadURL(),
			Size: int64(a.GetSize()),
		})
	}
	asset := selectAsset(assets, runtime.GOOS, runtime.GOARCH)
	if asset == nil {
		return nil, nil
	}
	return &release{
		Version: strings.TrimPrefix(rel.GetTagName(), "v"),
		Asset:   *asset,
	}, nil
}

// selectAsset picks the release archive built for goos/goarch. Release assets
// are named by goreleaser as morgue_<os>_<arch>.zip; checksums and signatures
// alongside them are never installable.
func selectAsset(assets []releaseAsset, goos, goarch string) *releaseAsset {
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if !strings.HasSuffix(name, ".zip") {
			continue
		}
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			return &assets[i]
		}
	}
	return nil
}

// Check checks if a newer version is available.
func Check(currentVersion string) error {
	rel, err := latestRelease(context.Background())
	if err != nil {
		return err
	}
	if rel == nil {
		fmt.Println("No release found.")
		return nil
	}

	if !isNewer(rel.Version, currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
	} else {
		fmt.Printf("Update available: %s → %s\n", currentVersion, rel.Version)
	}
	return nil
}

// CheckStatus returns update status string for TUI display.
// Returns one of: "up to date", "update: vX.Y.Z", "offline".
func CheckStatus(currentVersion string) string {
	rel, err := latestRelease(context.Background())
	if err != nil {
		return "offline"
	}
	if rel == nil || !isNewer(rel.Version, currentVersion) {
		return "up to date"
	}
	return "update: " + rel.Version
}

// Phase names emitted by Progress during an update.
const (
	PhaseDownloading = "downloading"
	PhaseInstalling  = "installing"
	PhaseDone        = "done"
	PhaseError       = "error"
)

// Progress describes one update progress tick. Callers (the GUI service) map
// this to a Wails event; the CLI ignores it.
type Progress struct {
	Phase      string `json:"phase"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Percent    int    `json:"percent"`
	Version    string `json:"version"`
	Error      string `json:"error,omitempty"`
}

// ProgressFunc receives progress ticks during Update. May be nil.
type ProgressFunc func(Progress)

// countingReader wraps an io.Reader and reports cumulative bytes read via emit.
type countingReader struct {
	r        io.Reader
	read     int64
	total    int64
	version  string
	emit     ProgressFunc
	lastPct  int
	lastEmit time.Time
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.read += int64(n)
		if c.emit != nil {
			pct := 0
			if c.total > 0 {
				pct = min(int(c.read*100/c.total), 100)
			}
			// Throttle: emit on a percent change or at most ~every 120ms, so a
			// fast burst of Reads can't flood the event bridge (and coalesce).
			if pct != c.lastPct || time.Since(c.lastEmit) >= 120*time.Millisecond {
				c.lastPct = pct
				c.lastEmit = time.Now()
				c.emit(Progress{
					Phase:      PhaseDownloading,
					Downloaded: c.read,
					Total:      c.total,
					Percent:    pct,
					Version:    c.version,
				})
			}
		}
	}
	return n, err
}

// Update downloads and applies the latest version, emitting progress via onProgress.
//
// The asset is read through a counting reader so download progress can be
// surfaced to the GUI. onProgress may be nil (CLI path), in which case no
// events are emitted.
func Update(currentVersion string, onProgress ProgressFunc) error {
	emit := func(p Progress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	ctx := context.Background()
	rel, err := latestRelease(ctx)
	if err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return err
	}
	if rel == nil {
		fmt.Println("No release found.")
		return nil
	}

	if !isNewer(rel.Version, currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating %s → %s...\n", currentVersion, rel.Version)

	fail := func(err error) {
		emit(Progress{Phase: PhaseError, Version: rel.Version, Error: err.Error()})
	}

	// Resolve the running executable's path; that is what we replace.
	cmdPath, err := os.Executable()
	if err != nil {
		fail(err)
		return fmt.Errorf("executable path: %w", err)
	}
	if resolved, lerr := filepath.EvalSymlinks(cmdPath); lerr == nil {
		cmdPath = resolved
	}

	emit(Progress{Phase: PhaseDownloading, Total: rel.Asset.Size, Percent: 0, Version: rel.Version})

	data, read, err := downloadAsset(ctx, rel, emit)
	if err != nil {
		fail(err)
		return fmt.Errorf("download asset %q: %w", rel.Asset.Name, err)
	}

	emit(Progress{Phase: PhaseInstalling, Downloaded: read, Total: rel.Asset.Size, Percent: 100, Version: rel.Version})

	exe, err := extractExecutable(data, filepath.Base(cmdPath))
	if err != nil {
		fail(err)
		return fmt.Errorf("extract %q: %w", rel.Asset.Name, err)
	}
	if err := replaceExecutable(cmdPath, exe); err != nil {
		fail(err)
		return fmt.Errorf("apply update: %w", err)
	}

	emit(Progress{Phase: PhaseDone, Downloaded: read, Total: rel.Asset.Size, Percent: 100, Version: rel.Version})
	fmt.Println("Update complete. Restart to use the new version.")
	return nil
}

// downloadAsset fetches the release archive, reporting progress as it reads.
func downloadAsset(ctx context.Context, rel *release, emit ProgressFunc) ([]byte, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	// The URL comes from the GitHub API response for our own release repo, not
	// from user input.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.Asset.URL, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("http %d", resp.StatusCode)
	}

	counting := &countingReader{
		r:       resp.Body,
		total:   rel.Asset.Size,
		version: rel.Version,
		emit:    emit,
	}
	data, err := io.ReadAll(counting)
	if err != nil {
		return nil, 0, err
	}
	return data, counting.read, nil
}

// extractExecutable pulls the single entry named exeName out of a zip archive,
// ignoring any directory prefix goreleaser might add.
func extractExecutable(archive []byte, exeName string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(exeName)
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Zip paths always use forward slashes, so path.Base semantics apply;
		// take the trailing segment by hand to stay OS-independent.
		name := f.Name
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		if strings.ToLower(name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxExeSize))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("%s in archive is empty", exeName)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s not found in archive", exeName)
}

// oldSuffix marks the displaced previous binary. Windows refuses to delete or
// overwrite a running .exe but does allow renaming it, so the swap is:
// rename running exe aside → move the new one into its place → drop the old
// copy on the next start (see CleanupOld).
const oldSuffix = ".old"

// renameFile is os.Rename, indirected so the rollback path (a rename that fails
// only after the running binary has been moved aside) is testable.
var renameFile = os.Rename

// replaceExecutable installs newExe at exePath. The new bytes are written to a
// temp file in the SAME directory first (so the rename into place is atomic and
// a truncated download can never become the installed binary), then the running
// binary is renamed aside. Any failure after that rename is rolled back, so the
// user is never left without an executable.
func replaceExecutable(exePath string, newExe []byte) error {
	dir := filepath.Dir(exePath)
	old := exePath + oldSuffix

	tmp, err := os.CreateTemp(dir, ".morgue-update-*")
	if err != nil {
		return fmt.Errorf("create temp binary: %w", err)
	}
	tmpPath := tmp.Name()
	_, werr := tmp.Write(newExe)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp binary: %w", err)
	}

	// A leftover .old from a previous update would block the rename below.
	_ = os.Remove(old)

	if err := renameFile(exePath, old); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move current binary aside: %w", err)
	}
	if err := renameFile(tmpPath, exePath); err != nil {
		// Roll back: put the original binary back where it was.
		_ = renameFile(old, exePath)
		_ = os.Remove(tmpPath)
		return fmt.Errorf("install new binary: %w", err)
	}

	// Best effort: on Windows the displaced binary is still running and cannot
	// be deleted until the process exits — CleanupOld finishes the job.
	_ = os.Remove(old)
	return nil
}

// CleanupOld removes the binary displaced by a previous update, if it is still
// around. Called once at startup; failures are irrelevant (the file is inert)
// and are ignored.
func CleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + oldSuffix)
}
