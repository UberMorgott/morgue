package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/creativeprojects/go-selfupdate/update"
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

// semverOrZero returns the clean base semver of v, or "0.0.0" when v isn't
// valid semver (e.g. a "dev" build). go-selfupdate's UpdateSelf parses the
// supplied current version as semver, so an unparseable "dev" must be mapped
// to a real low version instead of crashing the update.
func semverOrZero(v string) string {
	b := baseVersion(v)
	if _, err := semver.NewVersion(b); err != nil {
		return "0.0.0"
	}
	return b
}

const repo = "UberMorgott/morgue"

// newSource creates the GitHub release source. It is shared by the updater and
// by the manual progress-aware download path (which needs DownloadReleaseAsset).
func newSource() (*selfupdate.GitHubSource, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("github source: %w", err)
	}
	return source, nil
}

// newUpdater creates a configured selfupdate.Updater with GitHub source.
func newUpdater() (*selfupdate.Updater, error) {
	source, err := newSource()
	if err != nil {
		return nil, err
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
	if err != nil {
		return nil, fmt.Errorf("updater: %w", err)
	}
	return updater, nil
}

// Check checks if a newer version is available.
func Check(currentVersion string) error {
	updater, err := newUpdater()
	if err != nil {
		return err
	}

	ctx := context.Background()
	latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("detect latest: %w", err)
	}
	if !found {
		fmt.Println("No release found.")
		return nil
	}

	if !isNewer(latest.Version(), currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
	} else {
		fmt.Printf("Update available: %s → %s\n", currentVersion, latest.Version())
	}
	return nil
}

// CheckStatus returns update status string for TUI display.
// Returns one of: "up to date", "update: vX.Y.Z", "offline".
func CheckStatus(currentVersion string) string {
	updater, err := newUpdater()
	if err != nil {
		return "offline"
	}

	ctx := context.Background()
	latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(repo))
	if err != nil {
		return "offline"
	}
	if !found {
		return "up to date"
	}

	if !isNewer(latest.Version(), currentVersion) {
		return "up to date"
	}
	return "update: " + latest.Version()
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
				pct = int(c.read * 100 / c.total)
				if pct > 100 {
					pct = 100
				}
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
// It mirrors go-selfupdate's Updater.UpdateTo, but downloads the release asset
// through a counting reader so download progress can be surfaced to the GUI.
// go-selfupdate v1.5.2 exposes no progress hook on UpdateSelf/UpdateTo, so the
// counting-reader approach is the supported way to observe download bytes:
// DetectLatest → source.DownloadReleaseAsset → DecompressCommand → update.Apply.
// onProgress may be nil (CLI path), in which case no events are emitted.
func Update(currentVersion string, onProgress ProgressFunc) error {
	source, err := newSource()
	if err != nil {
		return err
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
	if err != nil {
		return fmt.Errorf("updater: %w", err)
	}

	emit := func(p Progress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	ctx := context.Background()
	rel, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(repo))
	if err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return fmt.Errorf("detect latest: %w", err)
	}
	if !found {
		fmt.Println("No release found.")
		return nil
	}

	if !isNewer(rel.Version(), currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating %s → %s...\n", currentVersion, rel.Version())

	// Resolve the running executable's path; that is what we replace.
	cmdPath, err := selfupdate.ExecutablePath()
	if err != nil {
		emit(Progress{Phase: PhaseError, Version: rel.Version(), Error: err.Error()})
		return fmt.Errorf("executable path: %w", err)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(cmdPath), ".exe") {
		cmdPath += ".exe"
	}

	emit(Progress{Phase: PhaseDownloading, Total: int64(rel.AssetByteSize), Percent: 0, Version: rel.Version()})

	// Download the asset through a counting reader so we observe progress.
	reader, err := source.DownloadReleaseAsset(ctx, rel, rel.AssetID)
	if err != nil {
		emit(Progress{Phase: PhaseError, Version: rel.Version(), Error: err.Error()})
		return fmt.Errorf("download asset %q: %w", rel.AssetName, err)
	}
	counting := &countingReader{
		r:       reader,
		total:   int64(rel.AssetByteSize),
		version: rel.Version(),
		emit:    emit,
	}
	data, err := io.ReadAll(counting)
	_ = reader.Close()
	if err != nil {
		emit(Progress{Phase: PhaseError, Version: rel.Version(), Error: err.Error()})
		return fmt.Errorf("read asset %q: %w", rel.AssetName, err)
	}

	emit(Progress{Phase: PhaseInstalling, Downloaded: counting.read, Total: counting.total, Percent: 100, Version: rel.Version()})

	// Decompress the archive to the embedded executable, then atomically replace
	// the current binary. This mirrors Updater.decompressAndUpdate.
	_, cmdName := splitCmd(cmdPath)
	asset, err := selfupdate.DecompressCommand(bytes.NewReader(data), rel.AssetName, cmdName, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		emit(Progress{Phase: PhaseError, Version: rel.Version(), Error: err.Error()})
		return fmt.Errorf("decompress %q: %w", rel.AssetName, err)
	}
	if err := update.Apply(asset, update.Options{TargetPath: cmdPath}); err != nil {
		emit(Progress{Phase: PhaseError, Version: rel.Version(), Error: err.Error()})
		return fmt.Errorf("apply update: %w", err)
	}

	emit(Progress{Phase: PhaseDone, Downloaded: counting.read, Total: counting.total, Percent: 100, Version: rel.Version()})
	fmt.Println("Update complete. Restart to use the new version.")
	return nil
}

// splitCmd splits a path into its directory and trailing filename. Mirrors
// filepath.Split but kept local to avoid importing path/filepath just for this.
func splitCmd(p string) (dir, file string) {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i+1], p[i+1:]
		}
	}
	return "", p
}
