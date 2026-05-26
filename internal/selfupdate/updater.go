package selfupdate

import (
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
)

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

	latest, found, err := updater.DetectLatest(nil, selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("detect latest: %w", err)
	}
	if !found {
		fmt.Println("No release found.")
		return nil
	}

	if latest.Version() == currentVersion {
		fmt.Printf("Already up to date: %s\n", currentVersion)
	} else {
		fmt.Printf("Update available: %s → %s\n", currentVersion, latest.Version())
	}
	return nil
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

	latest, found, err := updater.DetectLatest(nil, selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("detect latest: %w", err)
	}
	if !found {
		fmt.Println("No release found.")
		return nil
	}

	if latest.Version() == currentVersion {
		fmt.Printf("Already up to date: %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating %s → %s...\n", currentVersion, latest.Version())

	_, err = updater.UpdateSelf(nil, currentVersion, selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	fmt.Println("Update complete. Restart to use the new version.")
	return nil
}
