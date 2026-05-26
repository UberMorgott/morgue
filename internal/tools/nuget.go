package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// fetchNuGetLatestVersion returns the latest version of a NuGet package.
func fetchNuGetLatestVersion(packageID string) (string, error) {
	url := fmt.Sprintf("https://api.nuget.org/v3-flatcontainer/%s/index.json", strings.ToLower(packageID))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nuget API returned %d for %s", resp.StatusCode, packageID)
	}

	var result struct {
		Versions []string `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Versions) == 0 {
		return "", fmt.Errorf("no versions found for %s", packageID)
	}
	return result.Versions[len(result.Versions)-1], nil
}
