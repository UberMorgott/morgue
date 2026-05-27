package selfupdate

import (
	"context"
	"fmt"
	"strings"

	"github.com/creativeprojects/go-selfupdate"
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

const repo = "UberMorgott/morgue"

// Check checks if a newer version is available.
func Check(currentVersion string) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("github source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("updater: %w", err)
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

	if latest.Version() == baseVersion(currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
	} else {
		fmt.Printf("Update available: %s → %s\n", currentVersion, latest.Version())
	}
	return nil
}

// CheckStatus returns update status string for TUI display.
// Returns one of: "up to date", "update: vX.Y.Z", "offline".
func CheckStatus(currentVersion string) string {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return "offline"
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
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

	if latest.Version() == baseVersion(currentVersion) {
		return "up to date"
	}
	return "update: " + latest.Version()
}

// Update downloads and applies the latest version.
func Update(currentVersion string) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("github source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("updater: %w", err)
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

	if latest.Version() == baseVersion(currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating %s → %s...\n", currentVersion, latest.Version())

	_, err = updater.UpdateSelf(ctx, baseVersion(currentVersion), selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	fmt.Println("Update complete. Restart to use the new version.")
	return nil
}
